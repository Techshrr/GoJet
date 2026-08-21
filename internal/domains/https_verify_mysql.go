package domains

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"syscall"
	"time"
)

const httpsReadinessPolicyVersion = "https-readiness-v1"

const defaultTLSHandshakeTimeout = 5 * time.Second

type TLSProbeOutcome string

const (
	TLSProbePending TLSProbeOutcome = "pending"
	TLSProbeError   TLSProbeOutcome = "error"
	TLSProbeActive  TLSProbeOutcome = "active"
)

type TLSReadinessObservation struct {
	Outcome           TLSProbeOutcome `json:"outcome"`
	HandshakeComplete bool            `json:"handshake_complete"`
	TLSVersion        string          `json:"tls_version,omitempty"`
}

type TLSReadinessProbe interface {
	Probe(ctx context.Context, hostnameASCII string) (TLSReadinessObservation, error)
}

type TLSDialContext func(ctx context.Context, network, address string) (net.Conn, error)

type NetTLSReadinessProbe struct {
	DialContext      TLSDialContext
	RootCAs          *x509.CertPool
	HandshakeTimeout time.Duration
}

// Probe performs a real TCP/TLS handshake with hostname verification enabled.
// A handshake that cannot complete before the readiness timeout is pending;
// a completed but invalid TLS negotiation/certificate is error; only a
// successful verified handshake is active.
func (p NetTLSReadinessProbe) Probe(ctx context.Context, hostnameASCII string) (TLSReadinessObservation, error) {
	hostnameASCII = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(hostnameASCII, ".")))
	if hostnameASCII == "" || strings.Contains(hostnameASCII, ":") || net.ParseIP(hostnameASCII) != nil {
		return TLSReadinessObservation{}, ErrInvalidDomainMutation
	}
	timeout := p.HandshakeTimeout
	if timeout <= 0 {
		timeout = defaultTLSHandshakeTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dial := p.DialContext
	if dial == nil {
		var dialer net.Dialer
		dial = dialer.DialContext
	}
	conn, err := dial(probeCtx, "tcp", net.JoinHostPort(hostnameASCII, "443"))
	if err != nil {
		if isTLSPendingError(err) {
			return TLSReadinessObservation{Outcome: TLSProbePending}, nil
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			return TLSReadinessObservation{Outcome: TLSProbeError}, nil
		}
		return TLSReadinessObservation{}, ErrHTTPSProbe
	}
	defer conn.Close()
	if deadline, ok := probeCtx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: hostnameASCII,
		RootCAs:    p.RootCAs,
		MinVersion: tls.VersionTLS12,
	})
	if err := tlsConn.HandshakeContext(probeCtx); err != nil {
		if isTLSPendingError(err) {
			return TLSReadinessObservation{Outcome: TLSProbePending}, nil
		}
		return TLSReadinessObservation{Outcome: TLSProbeError}, nil
	}
	state := tlsConn.ConnectionState()
	if !state.HandshakeComplete || len(state.VerifiedChains) == 0 {
		return TLSReadinessObservation{Outcome: TLSProbeError}, nil
	}
	return TLSReadinessObservation{
		Outcome:           TLSProbeActive,
		HandshakeComplete: true,
		TLSVersion:        tlsVersionName(state.Version),
	}, nil
}

func isTLSPendingError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func tlsVersionName(version uint16) string {
	switch version {
	case tls.VersionTLS13:
		return "TLS1.3"
	case tls.VersionTLS12:
		return "TLS1.2"
	default:
		return "unknown"
	}
}

type VerifyHTTPSInput struct {
	WorkspaceID   string
	DomainID      uint64
	ActorID       string
	CorrelationID string
	Reason        string
	Now           time.Time
}

type HTTPSVerificationResult struct {
	Domain      Domain                  `json:"domain"`
	Observation TLSReadinessObservation `json:"observation"`
}

type HTTPSVerifier struct {
	store *MySQLStore
	probe TLSReadinessProbe
}

func NewHTTPSVerifier(store *MySQLStore, probe TLSReadinessProbe) *HTTPSVerifier {
	if probe == nil {
		probe = NetTLSReadinessProbe{}
	}
	return &HTTPSVerifier{store: store, probe: probe}
}

// Verify probes HTTPS only after current entitlement, ownership and ingress
// prerequisites are ready. The mutation transaction row-locks and re-checks
// those authorities after the network handshake so stale probe evidence cannot
// advance HTTPS after a concurrent trust change.
func (v *HTTPSVerifier) Verify(ctx context.Context, input VerifyHTTPSInput) (HTTPSVerificationResult, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	input.Reason = strings.TrimSpace(input.Reason)
	if v == nil || v.store == nil || v.probe == nil || input.WorkspaceID == "" || input.DomainID == 0 || input.ActorID == "" || input.CorrelationID == "" || input.Reason == "" || input.Now.IsZero() {
		return HTTPSVerificationResult{}, ErrInvalidDomainMutation
	}
	now := input.Now.UTC()

	snapshot, err := v.preflight(ctx, input, now)
	if err != nil {
		return HTTPSVerificationResult{}, err
	}
	observation, probeErr := v.probe.Probe(ctx, snapshot.HostnameASCII)
	if probeErr != nil {
		if err := v.recordProbeFailure(ctx, input, snapshot, now); err != nil {
			return HTTPSVerificationResult{}, err
		}
		return HTTPSVerificationResult{}, probeErr
	}
	if observation.Outcome != TLSProbePending && observation.Outcome != TLSProbeError && observation.Outcome != TLSProbeActive {
		return HTTPSVerificationResult{}, ErrHTTPSProbe
	}

	tx, err := v.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return HTTPSVerificationResult{}, err
	}
	defer tx.Rollback()
	domain, err := loadDomainByIDForUpdate(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return HTTPSVerificationResult{}, err
	}
	if domain.HostnameASCII != snapshot.HostnameASCII {
		return HTTPSVerificationResult{}, ErrInvalidDomainMutation
	}
	entitlement, err := v.store.resolveEntitlementTx(ctx, tx, input.WorkspaceID, now)
	if err != nil {
		return HTTPSVerificationResult{}, err
	}
	if !entitlement.MutationAllowed {
		if err := appendHTTPSDenialAuditTx(ctx, tx, domain, input, "entitlement_required", entitlement.DecisionReason); err != nil {
			return HTTPSVerificationResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return HTTPSVerificationResult{}, err
		}
		return HTTPSVerificationResult{}, ErrEntitlementRequired
	}
	if domain.OwnershipStatus != OwnershipVerified {
		if err := appendHTTPSDenialAuditTx(ctx, tx, domain, input, "ownership_required", "ownership verification required"); err != nil {
			return HTTPSVerificationResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return HTTPSVerificationResult{}, err
		}
		return HTTPSVerificationResult{}, ErrOwnershipRequired
	}
	if domain.IngressDNSStatus != IngressValid {
		if err := appendHTTPSDenialAuditTx(ctx, tx, domain, input, "ingress_dns_required", "valid ingress DNS required"); err != nil {
			return HTTPSVerificationResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return HTTPSVerificationResult{}, err
		}
		return HTTPSVerificationResult{}, ErrIngressDNSRequired
	}

	status := HTTPSPending
	revalidationResult := "pending"
	auditResult := "denied"
	auditCode := "https_pending"
	switch observation.Outcome {
	case TLSProbeError:
		status = HTTPSError
		revalidationResult = "fail"
		auditCode = "https_error"
	case TLSProbeActive:
		status = HTTPSActive
		revalidationResult = "pass"
		auditResult = "success"
		auditCode = "https_active"
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE custom_domains
		SET https_status = ?, https_checked_at = ?
		WHERE workspace_id = ? AND id = ?`, status, now, input.WorkspaceID, input.DomainID); err != nil {
		return HTTPSVerificationResult{}, err
	}
	updated, err := loadDomainByID(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return HTTPSVerificationResult{}, err
	}
	metadata := map[string]any{
		"outcome":            observation.Outcome,
		"handshake_complete": observation.HandshakeComplete,
	}
	if observation.TLSVersion != "" {
		metadata["tls_version"] = observation.TLSVersion
	}
	if err := appendHTTPSRevalidationTx(ctx, tx, updated, revalidationResult, now, input.CorrelationID, metadata); err != nil {
		return HTTPSVerificationResult{}, err
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &updated.ID, nil, input.ActorID, "domain.https.verify", auditResult, input.Reason, input.CorrelationID, map[string]any{
		"code":               auditCode,
		"outcome":            observation.Outcome,
		"handshake_complete": observation.HandshakeComplete,
	}); err != nil {
		return HTTPSVerificationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return HTTPSVerificationResult{}, err
	}
	return HTTPSVerificationResult{Domain: updated, Observation: observation}, nil
}

func (v *HTTPSVerifier) preflight(ctx context.Context, input VerifyHTTPSInput, now time.Time) (Domain, error) {
	tx, err := v.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Domain{}, err
	}
	defer tx.Rollback()
	domain, err := loadDomainByIDForUpdate(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return Domain{}, err
	}
	entitlement, err := v.store.resolveEntitlementTx(ctx, tx, input.WorkspaceID, now)
	if err != nil {
		return Domain{}, err
	}
	if !entitlement.MutationAllowed {
		if err := appendHTTPSDenialAuditTx(ctx, tx, domain, input, "entitlement_required", entitlement.DecisionReason); err != nil {
			return Domain{}, err
		}
		if err := tx.Commit(); err != nil {
			return Domain{}, err
		}
		return Domain{}, ErrEntitlementRequired
	}
	if domain.OwnershipStatus != OwnershipVerified {
		if err := appendHTTPSDenialAuditTx(ctx, tx, domain, input, "ownership_required", "ownership verification required"); err != nil {
			return Domain{}, err
		}
		if err := tx.Commit(); err != nil {
			return Domain{}, err
		}
		return Domain{}, ErrOwnershipRequired
	}
	if domain.IngressDNSStatus != IngressValid {
		if err := appendHTTPSDenialAuditTx(ctx, tx, domain, input, "ingress_dns_required", "valid ingress DNS required"); err != nil {
			return Domain{}, err
		}
		if err := tx.Commit(); err != nil {
			return Domain{}, err
		}
		return Domain{}, ErrIngressDNSRequired
	}
	if err := tx.Commit(); err != nil {
		return Domain{}, err
	}
	return domain, nil
}

func appendHTTPSDenialAuditTx(ctx context.Context, tx *sql.Tx, domain Domain, input VerifyHTTPSInput, code, reason string) error {
	return appendDomainAuditTx(ctx, tx, input.WorkspaceID, &domain.ID, nil, input.ActorID, "domain.https.verify", "denied", reason, input.CorrelationID, map[string]any{
		"code": code,
	})
}

func appendHTTPSRevalidationTx(ctx context.Context, tx *sql.Tx, domain Domain, result string, checkedAt time.Time, correlationID string, metadata map[string]any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO custom_domain_revalidations (
			domain_id, workspace_id, axis, result, policy_version, checked_at,
			next_due_at, evidence_ref, correlation_id, metadata_json
		) VALUES (?, ?, 'https', ?, ?, ?, NULL, ?, ?, ?)`,
		domain.ID, domain.WorkspaceID, result, httpsReadinessPolicyVersion, checkedAt,
		"tls:handshake:"+domain.HostnameASCII, correlationID, string(raw),
	)
	return err
}

func (v *HTTPSVerifier) recordProbeFailure(ctx context.Context, input VerifyHTTPSInput, snapshot Domain, checkedAt time.Time) error {
	tx, err := v.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	domain, err := loadDomainByIDForUpdate(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return err
	}
	if domain.HostnameASCII != snapshot.HostnameASCII {
		return ErrInvalidDomainMutation
	}
	metadata := map[string]any{"outcome": "probe_error", "handshake_complete": false}
	if err := appendHTTPSRevalidationTx(ctx, tx, domain, "error", checkedAt, input.CorrelationID, metadata); err != nil {
		return err
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &domain.ID, nil, input.ActorID, "domain.https.verify", "failed", "HTTPS readiness probe failed", input.CorrelationID, map[string]any{
		"code": "https_probe_failed",
	}); err != nil {
		return err
	}
	return tx.Commit()
}
