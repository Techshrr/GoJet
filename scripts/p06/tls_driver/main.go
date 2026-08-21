package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	_ "github.com/go-sql-driver/mysql"
)

type caseResult struct {
	CaseID  string         `json:"case_id"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details"`
	Errors  []string       `json:"errors"`
}

type tlsAuthority struct {
	listener     net.Listener
	activeConfig *tls.Config
	errorConfig  *tls.Config
	mu           sync.RWMutex
	mode         string
	accepts      int
	modes        []string
	done         chan struct{}
	wg           sync.WaitGroup
}

func main() {
	caseFlag := flag.String("case", "P06-T012", "P06 HTTPS readiness case ID")
	flag.Parse()

	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	if dsn == "" {
		failFatal("GOJET_MYSQL_DSN is required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		failFatal(err.Error())
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		failFatal(fmt.Sprintf("ping MySQL: %v", err))
	}

	result := caseResult{CaseID: *caseFlag, Status: "PASS", Details: map[string]any{}, Errors: []string{}}
	switch *caseFlag {
	case "P06-T012":
		if err := caseT012(ctx, db, &result); err != nil {
			result.Status = "FAIL"
			result.Errors = append(result.Errors, err.Error())
		}
	default:
		result.Status = "FAIL"
		result.Errors = append(result.Errors, fmt.Sprintf("unsupported case %s", *caseFlag))
	}
	writeJSON(map[string]caseResult{*caseFlag: result})
	if result.Status != "PASS" {
		os.Exit(1)
	}
}

func caseT012(ctx context.Context, db *sql.DB, out *caseResult) error {
	workspace := "p06-t012-https"
	hostname := "https-t012.example.com"
	store := domains.NewMySQLStore(db)
	now := fixedNow()
	if _, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace,
		SourceKey: "subscription-business-t012",
		Status: domains.EntitlementActive,
		DomainLimit: 1,
		StartsAt: now.Add(-24 * time.Hour),
		DecisionReason: "T012 active entitlement fixture",
	}, "corr-p06-t012-plan"); err != nil {
		return err
	}
	created, err := store.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspace,
		ActorID: "actor-t012",
		CorrelationID: "corr-p06-t012-create",
		Reason: "create domain before HTTPS readiness verification",
		Hostname: hostname,
		Now: now,
	})
	if err != nil {
		return err
	}
	fixtureAt := now.Add(time.Minute)
	if _, err := db.ExecContext(ctx, `
		UPDATE custom_domains
		SET ownership_status = 'verified', ownership_verified_at = ?,
		    ingress_dns_status = 'valid', ingress_dns_checked_at = ?,
		    risk_status = 'allow', risk_checked_at = ?, risk_policy_version = 't012-readiness-fixture'
		WHERE workspace_id = ? AND id = ?`, fixtureAt, fixtureAt, fixtureAt, workspace, created.Domain.ID); err != nil {
		return fmt.Errorf("seed T012 domain prerequisites: %w", err)
	}

	roots, activeCertificate, wrongCertificate, err := createTestPKI(hostname)
	if err != nil {
		return err
	}
	authority, err := startTLSAuthority(activeCertificate, wrongCertificate)
	if err != nil {
		return err
	}
	defer authority.Close()
	probe := domains.NetTLSReadinessProbe{
		RootCAs: roots,
		HandshakeTimeout: 200 * time.Millisecond,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "tcp", authority.Address())
		},
	}
	verifier := domains.NewHTTPSVerifier(store, probe)

	authority.SetMode("pending")
	pending, err := verifier.Verify(ctx, domains.VerifyHTTPSInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t012",
		CorrelationID: "corr-p06-t012-pending",
		Reason: "probe pending TLS provisioning",
		Now: now.Add(2 * time.Minute),
	})
	if err != nil {
		return fmt.Errorf("pending HTTPS verification: %w", err)
	}
	if pending.Observation.Outcome != domains.TLSProbePending || pending.Domain.HTTPSStatus != domains.HTTPSPending {
		return fmt.Errorf("pending TLS state outcome=%s status=%s", pending.Observation.Outcome, pending.Domain.HTTPSStatus)
	}

	authority.SetMode("error")
	failed, err := verifier.Verify(ctx, domains.VerifyHTTPSInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t012",
		CorrelationID: "corr-p06-t012-error",
		Reason: "probe hostname-invalid TLS certificate",
		Now: now.Add(3 * time.Minute),
	})
	if err != nil {
		return fmt.Errorf("error HTTPS verification: %w", err)
	}
	if failed.Observation.Outcome != domains.TLSProbeError || failed.Domain.HTTPSStatus != domains.HTTPSError {
		return fmt.Errorf("invalid TLS state outcome=%s status=%s", failed.Observation.Outcome, failed.Domain.HTTPSStatus)
	}

	authority.SetMode("active")
	active, err := verifier.Verify(ctx, domains.VerifyHTTPSInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t012",
		CorrelationID: "corr-p06-t012-active",
		Reason: "probe valid current TLS certificate",
		Now: now.Add(4 * time.Minute),
	})
	if err != nil {
		return fmt.Errorf("active HTTPS verification: %w", err)
	}
	if active.Observation.Outcome != domains.TLSProbeActive || active.Domain.HTTPSStatus != domains.HTTPSActive || !active.Observation.HandshakeComplete || active.Observation.TLSVersion == "" {
		return fmt.Errorf("valid TLS did not become active: observation=%+v status=%s", active.Observation, active.Domain.HTTPSStatus)
	}
	entitlement, err := store.ResolveEntitlement(ctx, workspace, now.Add(4*time.Minute))
	if err != nil {
		return err
	}
	activeReadiness := active.Domain.Readiness(entitlement)
	if !activeReadiness.HTTPSReady || !activeReadiness.ReadyForNewLinks || !activeReadiness.ReadyForRouting {
		return fmt.Errorf("active HTTPS did not satisfy ready authority with other fixture axes ready: %+v", activeReadiness)
	}

	authority.SetMode("error")
	regressed, err := verifier.Verify(ctx, domains.VerifyHTTPSInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t012",
		CorrelationID: "corr-p06-t012-regressed",
		Reason: "revalidate TLS after certificate regression",
		Now: now.Add(5 * time.Minute),
	})
	if err != nil {
		return fmt.Errorf("regressed HTTPS verification: %w", err)
	}
	regressedReadiness := regressed.Domain.Readiness(entitlement)
	if regressed.Domain.HTTPSStatus != domains.HTTPSError || regressedReadiness.HTTPSReady || regressedReadiness.ReadyForNewLinks || regressedReadiness.ReadyForRouting {
		return fmt.Errorf("current TLS error did not fail closed: status=%s readiness=%+v", regressed.Domain.HTTPSStatus, regressedReadiness)
	}
	if regressed.Domain.OwnershipStatus != domains.OwnershipVerified || regressed.Domain.IngressDNSStatus != domains.IngressValid || regressed.Domain.RiskStatus != domains.RiskAllow {
		return fmt.Errorf("HTTPS regression collapsed independent axes: ownership=%s ingress=%s risk=%s", regressed.Domain.OwnershipStatus, regressed.Domain.IngressDNSStatus, regressed.Domain.RiskStatus)
	}

	authority.SetMode("active")
	recovered, err := verifier.Verify(ctx, domains.VerifyHTTPSInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t012",
		CorrelationID: "corr-p06-t012-recovered",
		Reason: "revalidate restored TLS certificate",
		Now: now.Add(6 * time.Minute),
	})
	if err != nil {
		return fmt.Errorf("recovered HTTPS verification: %w", err)
	}
	if recovered.Observation.Outcome != domains.TLSProbeActive || recovered.Domain.HTTPSStatus != domains.HTTPSActive {
		return fmt.Errorf("restored TLS did not recover: outcome=%s status=%s", recovered.Observation.Outcome, recovered.Domain.HTTPSStatus)
	}

	revalidationResults, err := queryStrings(ctx, db, `
		SELECT result
		FROM custom_domain_revalidations
		WHERE workspace_id = ? AND domain_id = ? AND axis = 'https'
		ORDER BY id`, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	wantResults := []string{"pending", "fail", "pass", "fail", "pass"}
	if strings.Join(revalidationResults, ",") != strings.Join(wantResults, ",") {
		return fmt.Errorf("HTTPS revalidation results=%v want=%v", revalidationResults, wantResults)
	}
	outcomes, err := queryStrings(ctx, db, `
		SELECT JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.outcome'))
		FROM custom_domain_revalidations
		WHERE workspace_id = ? AND domain_id = ? AND axis = 'https'
		ORDER BY id`, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	wantOutcomes := []string{"pending", "error", "active", "error", "active"}
	if strings.Join(outcomes, ",") != strings.Join(wantOutcomes, ",") {
		return fmt.Errorf("HTTPS revalidation outcomes=%v want=%v", outcomes, wantOutcomes)
	}

	auditMetadata, err := queryStrings(ctx, db, `
		SELECT CAST(metadata_json AS CHAR)
		FROM custom_domain_audit_events
		WHERE workspace_id = ? AND domain_id = ? AND action = 'domain.https.verify'
		ORDER BY id`, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	if len(auditMetadata) != 5 {
		return fmt.Errorf("HTTPS verify audit count=%d want=5", len(auditMetadata))
	}
	joinedAudit := strings.Join(auditMetadata, "\n")
	if strings.Contains(joinedAudit, authority.Address()) || strings.Contains(joinedAudit, "wrong-t012.example.net") {
		return errors.New("HTTPS verification audit leaked endpoint or certificate detail")
	}

	accepts, modes := authority.Evidence()
	if accepts < 5 {
		return fmt.Errorf("TLS authority accepted only %d connections, want at least 5", accepts)
	}
	wantModes := []string{"pending", "error", "active", "error", "active"}
	if len(modes) < len(wantModes) || strings.Join(modes[:len(wantModes)], ",") != strings.Join(wantModes, ",") {
		return fmt.Errorf("TLS authority mode evidence=%v want prefix=%v", modes, wantModes)
	}

	out.Details = map[string]any{
		"domain_id": created.Domain.ID,
		"hostname_ascii": created.Domain.HostnameASCII,
		"transport": "tcp+tls",
		"tls_authority_connections": accepts,
		"tls_authority_modes": modes,
		"pending_outcome": pending.Observation.Outcome,
		"error_outcome": failed.Observation.Outcome,
		"active_outcome": active.Observation.Outcome,
		"active_tls_version": active.Observation.TLSVersion,
		"regressed_outcome": regressed.Observation.Outcome,
		"recovered_outcome": recovered.Observation.Outcome,
		"final_https_status": recovered.Domain.HTTPSStatus,
		"active_ready_for_new_links": activeReadiness.ReadyForNewLinks,
		"regressed_ready_for_new_links": regressedReadiness.ReadyForNewLinks,
		"regressed_ready_for_routing": regressedReadiness.ReadyForRouting,
		"ownership_status_after_https_error": regressed.Domain.OwnershipStatus,
		"ingress_status_after_https_error": regressed.Domain.IngressDNSStatus,
		"risk_status_after_https_error": regressed.Domain.RiskStatus,
		"revalidation_results": revalidationResults,
		"revalidation_outcomes": outcomes,
		"verify_audit_events": len(auditMetadata),
		"audit_tls_detail_leak": false,
	}
	return nil
}

func createTestPKI(hostname string) (*x509.CertPool, tls.Certificate, tls.Certificate, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, tls.Certificate{}, tls.Certificate{}, err
	}
	now := time.Now().UTC()
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(12001),
		Subject: pkix.Name{CommonName: "GoJet P06 T012 Test CA"},
		NotBefore: now.Add(-time.Hour),
		NotAfter: now.Add(24 * time.Hour),
		IsCA: true,
		BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, tls.Certificate{}, tls.Certificate{}, err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, tls.Certificate{}, tls.Certificate{}, err
	}
	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	active, err := createLeaf(caCert, caKey, caDER, hostname, 12002, now)
	if err != nil {
		return nil, tls.Certificate{}, tls.Certificate{}, err
	}
	wrong, err := createLeaf(caCert, caKey, caDER, "wrong-t012.example.net", 12003, now)
	if err != nil {
		return nil, tls.Certificate{}, tls.Certificate{}, err
	}
	return roots, active, wrong, nil
}

func createLeaf(caCert *x509.Certificate, caKey *ecdsa.PrivateKey, caDER []byte, hostname string, serial int64, now time.Time) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject: pkix.Name{CommonName: hostname},
		DNSNames: []string{hostname},
		NotBefore: now.Add(-time.Hour),
		NotAfter: now.Add(12 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{
		Certificate: [][]byte{leafDER, caDER},
		PrivateKey: key,
		Leaf: leaf,
	}, nil
}

func startTLSAuthority(activeCertificate, wrongCertificate tls.Certificate) (*tlsAuthority, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	authority := &tlsAuthority{
		listener: listener,
		activeConfig: &tls.Config{Certificates: []tls.Certificate{activeCertificate}, MinVersion: tls.VersionTLS12},
		errorConfig: &tls.Config{Certificates: []tls.Certificate{wrongCertificate}, MinVersion: tls.VersionTLS12},
		mode: "pending",
		done: make(chan struct{}),
	}
	go authority.serve()
	return authority, nil
}

func (a *tlsAuthority) Address() string {
	return a.listener.Addr().String()
}

func (a *tlsAuthority) SetMode(mode string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mode = mode
}

func (a *tlsAuthority) Evidence() (int, []string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.accepts, append([]string(nil), a.modes...)
}

func (a *tlsAuthority) Close() {
	_ = a.listener.Close()
	<-a.done
	a.wg.Wait()
}

func (a *tlsAuthority) serve() {
	defer close(a.done)
	for {
		conn, err := a.listener.Accept()
		if err != nil {
			return
		}
		a.mu.Lock()
		mode := a.mode
		a.accepts++
		a.modes = append(a.modes, mode)
		a.mu.Unlock()
		a.wg.Add(1)
		go func(conn net.Conn, mode string) {
			defer a.wg.Done()
			defer conn.Close()
			switch mode {
			case "pending":
				_ = conn.SetDeadline(time.Now().Add(time.Second))
				buffer := make([]byte, 1)
				_, _ = conn.Read(buffer)
				time.Sleep(500 * time.Millisecond)
			case "error":
				tlsConn := tls.Server(conn, a.errorConfig)
				_ = tlsConn.Handshake()
			case "active":
				tlsConn := tls.Server(conn, a.activeConfig)
				if err := tlsConn.Handshake(); err == nil {
					_, _ = io.WriteString(tlsConn, "ok")
				}
			}
		}(conn, mode)
	}
}

func queryStrings(ctx context.Context, db *sql.DB, query string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func fixedNow() time.Time {
	return time.Date(2026, 8, 21, 16, 0, 0, 0, time.UTC)
}

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		failFatal(err.Error())
	}
}

func failFatal(message string) {
	_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"status": "FAIL", "error": message})
	os.Exit(2)
}
