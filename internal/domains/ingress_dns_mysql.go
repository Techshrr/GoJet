package domains

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

const ingressDNSPolicyVersion = "ingress-dns-v1"

type CNAMEResolver interface {
	LookupCNAME(ctx context.Context, name string) (string, error)
}

type NetCNAMEResolver struct {
	Resolver *net.Resolver
}

func (r NetCNAMEResolver) LookupCNAME(ctx context.Context, name string) (string, error) {
	resolver := r.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrInvalidDomainMutation
	}
	if !strings.HasSuffix(name, ".") {
		name += "."
	}
	return resolver.LookupCNAME(ctx, name)
}

type IngressDNSOutcome string

const (
	IngressDNSValid    IngressDNSOutcome = "valid"
	IngressDNSMissing  IngressDNSOutcome = "missing"
	IngressDNSMismatch IngressDNSOutcome = "mismatch"
	IngressDNSDrift    IngressDNSOutcome = "drift"
)

type VerifyIngressDNSInput struct {
	WorkspaceID   string
	DomainID      uint64
	ActorID       string
	CorrelationID string
	Reason        string
	Now           time.Time
}

type IngressDNSVerificationResult struct {
	Domain  Domain            `json:"domain"`
	Outcome IngressDNSOutcome `json:"outcome"`
}

type IngressDNSVerifier struct {
	store          *MySQLStore
	resolver       CNAMEResolver
	expectedTarget string
}

// NewIngressDNSVerifier establishes the server-owned ingress target authority.
// The expected target is supplied by service configuration, never by the
// customer verification request.
func NewIngressDNSVerifier(store *MySQLStore, resolver CNAMEResolver, expectedTarget string) (*IngressDNSVerifier, error) {
	if store == nil {
		return nil, ErrInvalidDomainMutation
	}
	if resolver == nil {
		resolver = NetCNAMEResolver{Resolver: net.DefaultResolver}
	}
	canonicalTarget, err := canonicalDNSTarget(expectedTarget)
	if err != nil {
		return nil, err
	}
	return &IngressDNSVerifier{store: store, resolver: resolver, expectedTarget: canonicalTarget}, nil
}

// Verify performs a real CNAME lookup only after entitlement and ownership
// preflight. It then row-locks and re-checks both authorities before updating
// the independent ingress_dns axis, preventing stale DNS results from advancing
// a domain after ownership or entitlement changes.
func (v *IngressDNSVerifier) Verify(ctx context.Context, input VerifyIngressDNSInput) (IngressDNSVerificationResult, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	input.Reason = strings.TrimSpace(input.Reason)
	if v == nil || v.store == nil || v.resolver == nil || v.expectedTarget == "" || input.WorkspaceID == "" || input.DomainID == 0 || input.ActorID == "" || input.CorrelationID == "" || input.Reason == "" || input.Now.IsZero() {
		return IngressDNSVerificationResult{}, ErrInvalidDomainMutation
	}
	now := input.Now.UTC()

	snapshot, err := v.preflight(ctx, input, now)
	if err != nil {
		return IngressDNSVerificationResult{}, err
	}

	observedTarget, lookupErr := v.resolver.LookupCNAME(ctx, snapshot.HostnameASCII)
	missing := isDNSNotFound(lookupErr)
	if lookupErr != nil && !missing {
		if err := v.recordLookupFailure(ctx, input, snapshot, now); err != nil {
			return IngressDNSVerificationResult{}, err
		}
		return IngressDNSVerificationResult{}, ErrIngressDNSLookup
	}
	canonicalObserved := ""
	if !missing {
		canonicalObserved, err = canonicalDNSTarget(observedTarget)
		if err != nil {
			canonicalObserved = ""
		}
	}

	tx, err := v.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return IngressDNSVerificationResult{}, err
	}
	defer tx.Rollback()

	domain, err := loadDomainByIDForUpdate(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return IngressDNSVerificationResult{}, err
	}
	if domain.HostnameASCII != snapshot.HostnameASCII {
		return IngressDNSVerificationResult{}, ErrInvalidDomainMutation
	}
	entitlement, err := v.store.resolveEntitlementTx(ctx, tx, input.WorkspaceID, now)
	if err != nil {
		return IngressDNSVerificationResult{}, err
	}
	if !entitlement.MutationAllowed {
		if err := appendIngressDenialAuditTx(ctx, tx, domain, input, "entitlement_required", entitlement.DecisionReason); err != nil {
			return IngressDNSVerificationResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return IngressDNSVerificationResult{}, err
		}
		return IngressDNSVerificationResult{}, ErrEntitlementRequired
	}
	if domain.OwnershipStatus != OwnershipVerified {
		if err := appendIngressDenialAuditTx(ctx, tx, domain, input, "ownership_required", "ownership verification required"); err != nil {
			return IngressDNSVerificationResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return IngressDNSVerificationResult{}, err
		}
		return IngressDNSVerificationResult{}, ErrOwnershipRequired
	}

	outcome := IngressDNSMismatch
	status := IngressInvalid
	if missing {
		outcome = IngressDNSMissing
	} else if canonicalObserved == v.expectedTarget {
		outcome = IngressDNSValid
		status = IngressValid
	}
	if status != IngressValid && domain.IngressDNSStatus == IngressValid {
		outcome = IngressDNSDrift
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE custom_domains
		SET ingress_dns_status = ?, ingress_dns_checked_at = ?
		WHERE workspace_id = ? AND id = ?`, status, now, input.WorkspaceID, input.DomainID); err != nil {
		return IngressDNSVerificationResult{}, err
	}
	updated, err := loadDomainByID(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return IngressDNSVerificationResult{}, err
	}

	revalidationResult := "fail"
	auditResult := "denied"
	auditCode := "ingress_dns_invalid"
	if outcome == IngressDNSValid {
		revalidationResult = "pass"
		auditResult = "success"
		auditCode = "ingress_dns_valid"
	} else if outcome == IngressDNSDrift {
		auditCode = "ingress_dns_drift"
	} else if outcome == IngressDNSMissing {
		auditCode = "ingress_dns_missing"
	}
	metadata := map[string]any{
		"outcome":        outcome,
		"target_matched": outcome == IngressDNSValid,
	}
	if err := appendIngressRevalidationTx(ctx, tx, updated, revalidationResult, now, input.CorrelationID, metadata); err != nil {
		return IngressDNSVerificationResult{}, err
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &updated.ID, nil, input.ActorID, "domain.ingress.verify", auditResult, input.Reason, input.CorrelationID, map[string]any{
		"code":           auditCode,
		"outcome":        outcome,
		"target_matched": outcome == IngressDNSValid,
	}); err != nil {
		return IngressDNSVerificationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return IngressDNSVerificationResult{}, err
	}
	return IngressDNSVerificationResult{Domain: updated, Outcome: outcome}, nil
}

func (v *IngressDNSVerifier) preflight(ctx context.Context, input VerifyIngressDNSInput, now time.Time) (Domain, error) {
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
		if err := appendIngressDenialAuditTx(ctx, tx, domain, input, "entitlement_required", entitlement.DecisionReason); err != nil {
			return Domain{}, err
		}
		if err := tx.Commit(); err != nil {
			return Domain{}, err
		}
		return Domain{}, ErrEntitlementRequired
	}
	if domain.OwnershipStatus != OwnershipVerified {
		if err := appendIngressDenialAuditTx(ctx, tx, domain, input, "ownership_required", "ownership verification required"); err != nil {
			return Domain{}, err
		}
		if err := tx.Commit(); err != nil {
			return Domain{}, err
		}
		return Domain{}, ErrOwnershipRequired
	}
	if err := tx.Commit(); err != nil {
		return Domain{}, err
	}
	return domain, nil
}

func appendIngressDenialAuditTx(ctx context.Context, tx *sql.Tx, domain Domain, input VerifyIngressDNSInput, code, reason string) error {
	return appendDomainAuditTx(ctx, tx, input.WorkspaceID, &domain.ID, nil, input.ActorID, "domain.ingress.verify", "denied", reason, input.CorrelationID, map[string]any{
		"code": code,
	})
}

func appendIngressRevalidationTx(ctx context.Context, tx *sql.Tx, domain Domain, result string, checkedAt time.Time, correlationID string, metadata map[string]any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO custom_domain_revalidations (
			domain_id, workspace_id, axis, result, policy_version, checked_at,
			next_due_at, evidence_ref, correlation_id, metadata_json
		) VALUES (?, ?, 'ingress_dns', ?, ?, ?, NULL, ?, ?, ?)`,
		domain.ID, domain.WorkspaceID, result, ingressDNSPolicyVersion, checkedAt,
		"dns:cname:"+domain.HostnameASCII, correlationID, string(raw),
	)
	return err
}

func (v *IngressDNSVerifier) recordLookupFailure(ctx context.Context, input VerifyIngressDNSInput, snapshot Domain, checkedAt time.Time) error {
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
	metadata := map[string]any{"outcome": "lookup_error", "target_matched": false}
	if err := appendIngressRevalidationTx(ctx, tx, domain, "error", checkedAt, input.CorrelationID, metadata); err != nil {
		return err
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &domain.ID, nil, input.ActorID, "domain.ingress.verify", "failed", "ingress DNS lookup failed", input.CorrelationID, map[string]any{
		"code": "ingress_dns_lookup_failed",
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func canonicalDNSTarget(raw string) (string, error) {
	target := strings.TrimSpace(strings.TrimSuffix(raw, "."))
	if target == "" || strings.HasPrefix(target, "*.") || strings.ContainsAny(target, "/?#@ ") || net.ParseIP(target) != nil {
		return "", ErrInvalidDomainMutation
	}
	ascii, err := idna.Lookup.ToASCII(target)
	if err != nil {
		return "", ErrInvalidDomainMutation
	}
	ascii = strings.ToLower(strings.TrimSuffix(ascii, "."))
	if ascii == "" || len(ascii) > 253 || !strings.Contains(ascii, ".") {
		return "", ErrInvalidDomainMutation
	}
	return ascii, nil
}

func isDNSNotFound(err error) bool {
	if err == nil {
		return false
	}
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && dnsErr.IsNotFound
}
