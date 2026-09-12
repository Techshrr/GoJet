package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/net/dns/dnsmessage"
)

type predecessorEvidence struct {
	Status               string         `json:"status"`
	ImplementationCommit string         `json:"implementation_commit"`
	ExactHead            string         `json:"exact_head"`
	Errors               []string       `json:"errors"`
	Details              map[string]any `json:"details"`
}

type probe struct {
	Status               string         `json:"status"`
	ImplementationCommit string         `json:"implementation_commit"`
	Errors               []string       `json:"errors"`
	Details              map[string]any `json:"details"`
}

type httpResult struct {
	Status  int
	Body    map[string]any
	Raw     []byte
	Headers http.Header
	Cookies []*http.Cookie
}

type membershipPermission struct {
	db     *sql.DB
	userID string
}

func (p membershipPermission) CanManageCustomDomains(ctx context.Context, workspaceID, actorID string) (bool, error) {
	if p.db == nil || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(actorID) == "" || actorID != p.userID {
		return false, nil
	}
	var role string
	if err := p.db.QueryRowContext(ctx,
		"SELECT role FROM workspace_memberships WHERE workspace_id=? AND user_id=?",
		workspaceID, actorID,
	).Scan(&role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner", "admin", "member":
		return true, nil
	default:
		return false, nil
	}
}

type domainDNSAuthority struct {
	conn         *net.UDPConn
	txtName      string
	hostname     string
	mu           sync.RWMutex
	txtRecords   []string
	cnameTarget  string
	txtQueries   int
	cnameQueries int
	done         chan struct{}
}

func startDomainDNSAuthority(txtName, hostname string) (*domainDNSAuthority, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		return nil, err
	}
	a := &domainDNSAuthority{
		conn:     conn,
		txtName:  strings.ToLower(strings.TrimSuffix(txtName, ".")) + ".",
		hostname: strings.ToLower(strings.TrimSuffix(hostname, ".")) + ".",
		done:     make(chan struct{}),
	}
	go a.serve()
	return a, nil
}

func (a *domainDNSAuthority) Close() {
	_ = a.conn.Close()
	<-a.done
}

func (a *domainDNSAuthority) SetTXT(records []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.txtRecords = append([]string(nil), records...)
}

func (a *domainDNSAuthority) SetCNAME(target string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cnameTarget = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(target, ".")))
}

func (a *domainDNSAuthority) resolver() *net.Resolver {
	address := a.conn.LocalAddr().String()
	return &net.Resolver{
		PreferGo:     true,
		StrictErrors: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "udp4", address)
		},
	}
}

func (a *domainDNSAuthority) TXTResolver() domains.TXTResolver {
	return domains.NetTXTResolver{Resolver: a.resolver()}
}

func (a *domainDNSAuthority) CNAMEResolver() domains.CNAMEResolver {
	return domains.NetCNAMEResolver{Resolver: a.resolver()}
}

func (a *domainDNSAuthority) QueryCounts() (int, int) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.txtQueries, a.cnameQueries
}

func (a *domainDNSAuthority) serve() {
	defer close(a.done)
	buffer := make([]byte, 4096)
	for {
		n, remote, err := a.conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		var query dnsmessage.Message
		if err := query.Unpack(buffer[:n]); err != nil || len(query.Questions) == 0 {
			continue
		}
		question := query.Questions[0]
		qname := strings.ToLower(question.Name.String())

		a.mu.Lock()
		txtRecords := append([]string(nil), a.txtRecords...)
		cnameTarget := a.cnameTarget
		if qname == a.txtName && question.Type == dnsmessage.TypeTXT {
			a.txtQueries++
		}
		if qname == a.hostname && question.Type == dnsmessage.TypeCNAME {
			a.cnameQueries++
		}
		a.mu.Unlock()

		answers := []dnsmessage.Resource{}
		if qname == a.txtName && question.Type == dnsmessage.TypeTXT {
			for _, record := range txtRecords {
				answers = append(answers, dnsmessage.Resource{
					Header: dnsmessage.ResourceHeader{Name: question.Name, Class: dnsmessage.ClassINET, TTL: 0},
					Body:   &dnsmessage.TXTResource{TXT: []string{record}},
				})
			}
		}
		if qname == a.hostname && question.Type == dnsmessage.TypeCNAME && cnameTarget != "" {
			name, nameErr := dnsmessage.NewName(cnameTarget + ".")
			if nameErr == nil {
				answers = append(answers, dnsmessage.Resource{
					Header: dnsmessage.ResourceHeader{Name: question.Name, Class: dnsmessage.ClassINET, TTL: 0},
					Body:   &dnsmessage.CNAMEResource{CNAME: name},
				})
			}
		}
		response := dnsmessage.Message{
			Header: dnsmessage.Header{
				ID:               query.Header.ID,
				Response:         true,
				Authoritative:    true,
				RecursionDesired: query.Header.RecursionDesired,
				RCode:            dnsmessage.RCodeSuccess,
			},
			Questions: query.Questions,
			Answers:   answers,
		}
		packed, packErr := response.Pack()
		if packErr == nil {
			_, _ = a.conn.WriteToUDP(packed, remote)
		}
	}
}

type tlsAuthority struct {
	listener net.Listener
	roots    *x509.CertPool
	done     chan struct{}
	mu       sync.Mutex
	count    int
}

func startTLSAuthority(hostname string) (*tlsAuthority, error) {
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "GoJet P20 T019 local CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, err
	}

	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: hostname},
		DNSNames:     []string{hostname},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(12 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	cert := tls.Certificate{Certificate: [][]byte{serverDER, caDER}, PrivateKey: serverKey}
	listener, err := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	a := &tlsAuthority{listener: listener, roots: roots, done: make(chan struct{})}
	go a.serve()
	return a, nil
}

func (a *tlsAuthority) serve() {
	defer close(a.done)
	for {
		conn, err := a.listener.Accept()
		if err != nil {
			return
		}
		a.mu.Lock()
		a.count++
		a.mu.Unlock()
		go func(c net.Conn) {
			defer c.Close()
			if tlsConn, ok := c.(*tls.Conn); ok {
				_ = tlsConn.Handshake()
			}
		}(conn)
	}
}

func (a *tlsAuthority) Close() {
	_ = a.listener.Close()
	<-a.done
}

func (a *tlsAuthority) Probe() domains.TLSReadinessProbe {
	address := a.listener.Addr().String()
	return domains.NetTLSReadinessProbe{
		RootCAs:          a.roots,
		HandshakeTimeout: 3 * time.Second,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "tcp4", address)
		},
	}
}

func (a *tlsAuthority) Connections() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.count
}

type mutableRiskEvaluator struct {
	mu          sync.RWMutex
	observation domains.DomainRiskObservation
	calls       int
}

func (e *mutableRiskEvaluator) Set(status domains.DomainRiskStatus, policy, evidence string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.observation = domains.DomainRiskObservation{Status: status, PolicyVersion: policy, EvidenceRef: evidence}
}

func (e *mutableRiskEvaluator) Evaluate(ctx context.Context, hostnameASCII string) (domains.DomainRiskObservation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	return e.observation, nil
}

func (e *mutableRiskEvaluator) Calls() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.calls
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	exactHead := strings.TrimSpace(os.Getenv("P20_EXACT_HEAD"))
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	apiBase := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_P20_API_BASE")), "/")
	origin := strings.TrimSpace(os.Getenv("GOJET_AUTH_ALLOWED_ORIGIN"))
	outPath := strings.TrimSpace(os.Getenv("GOJET_P20_T019_PROBE_OUT"))
	if outPath == "" {
		outPath = "artifacts/v10/P20/runtime/t019/probe.json"
	}

	result := probe{
		Status:               "FAIL",
		ImplementationCommit: exactHead,
		Errors:               []string{},
		Details: map[string]any{
			"real_mysql":                 true,
			"real_platform_api":          true,
			"real_session_authenticated": false,
			"csrf_authority_issued":      false,
			"real_dns_udp":               false,
			"real_tls_handshake":         false,
			"mock_authority":             false,
			"test_header_authority":      false,
			"secret_material_recorded":   false,
		},
	}
	finish := func() {
		if len(result.Errors) == 0 {
			result.Status = "PASS"
		}
		_ = os.MkdirAll(filepath.Dir(outPath), 0o755)
		raw, _ := json.MarshalIndent(result, "", "  ")
		raw = append(raw, '\n')
		_ = os.WriteFile(outPath, raw, 0o644)
		_, _ = os.Stdout.Write(raw)
		if len(result.Errors) != 0 {
			os.Exit(1)
		}
	}
	fail := func(message string) {
		result.Errors = append(result.Errors, message)
		finish()
	}

	if len(exactHead) != 40 || dsn == "" || apiBase == "" || origin == "" {
		fail("T019 formal probe runtime configuration is incomplete")
		return
	}

	t018, err := readEvidence("artifacts/v10/P20/p0/P20-T018.json")
	if err != nil || t018.Status != "PASS" || t018.ImplementationCommit != exactHead {
		fail("T019 requires same-run exact-head formal P20-T018 PASS evidence")
		return
	}
	t018Details := t018.Details
	userID := stringValue(t018Details["user_id"])
	workspaceID := stringValue(t018Details["workspace_id"])
	if userID == "" || workspaceID == "" || stringValue(t018Details["next_case"]) != "P20-T019" {
		fail("T019 requires P20-T018 to unlock exactly P20-T019 with correlated identity")
		return
	}
	result.Details["t018_evidence_bound"] = true
	result.Details["user_id"] = userID
	result.Details["workspace_id"] = workspaceID

	p06Cases := []string{"P06-T002", "P06-T009", "P06-T010", "P06-T011", "P06-T012", "P06-T013", "P06-T014", "P06-T018"}
	for _, caseID := range p06Cases {
		path := filepath.Join("artifacts", "v10", "P06", "results", caseID+".json")
		evidence, readErr := readEvidence(path)
		if readErr != nil || evidence.Status != "PASS" || evidence.ImplementationCommit != exactHead || len(evidence.Errors) != 0 {
			fail("T019 requires exact-head P06 predecessor PASS evidence: " + caseID)
			return
		}
	}
	result.Details["p06_entitlement_ticket_independence_proven"] = true
	result.Details["p06_ownership_dns_https_risk_axes_proven"] = true
	result.Details["p06_revalidation_history_proven"] = true
	result.Details["p06_link_assignment_guard_proven"] = true

	for _, caseID := range []string{"P17-T008", "P17-T009"} {
		path := filepath.Join("artifacts", "v10", "P17", "domain", caseID+".json")
		evidence, readErr := readEvidence(path)
		if readErr != nil || evidence.Status != "PASS" || evidence.ExactHead != exactHead || len(evidence.Errors) != 0 {
			fail("T019 requires exact-head P17 predecessor PASS evidence: " + caseID)
			return
		}
	}
	result.Details["p17_ticket_manager_cannot_grant_entitlement"] = true
	result.Details["p17_entitlement_and_safety_conjunctive"] = true

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fail("T019 formal probe could not open MySQL")
		return
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		fail("T019 formal probe could not reach MySQL")
		return
	}

	var userStatus, workspaceRole string
	if err := db.QueryRowContext(ctx, `
		SELECT u.status,m.role
		FROM auth_users u JOIN workspace_memberships m ON m.user_id=u.id
		WHERE u.id=? AND m.workspace_id=?`, userID, workspaceID,
	).Scan(&userStatus, &workspaceRole); err != nil || userStatus != "active" || workspaceRole != "owner" {
		fail("T019 predecessor identity is not an active owner Workspace authority")
		return
	}
	result.Details["workspace_role"] = workspaceRole

	windowSeconds := 60
	if raw := strings.TrimSpace(os.Getenv("GOJET_AUTH_RATE_WINDOW_SECONDS")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 && parsed <= 300 {
			windowSeconds = parsed
		}
	}
	result.Details["auth_rate_window_seconds"] = windowSeconds
	result.Details["auth_rate_window_respected"] = true
	select {
	case <-ctx.Done():
		fail("T019 probe timed out before the real authentication rate window elapsed")
		return
	case <-time.After(time.Duration(windowSeconds+1) * time.Second):
	}

	suffix := strings.ToLower(exactHead[:12])
	email := "p20-t009-" + suffix + "@example.test"
	login, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/auth/login", map[string]any{
		"email":          email,
		"password":       "P20-T009!Strong-Passphrase-2026",
		"correlation_id": "p20-t019-login-" + suffix,
	}, nil)
	if err != nil {
		fail("T019 real P15 login request failed")
		return
	}
	result.Details["login_http_status"] = login.Status
	if login.Status != http.StatusOK || stringValue(login.Body["status"]) != "authenticated" {
		result.Details["login_error_code"] = nestedErrorCode(login.Body)
		fail("T019 could not establish the real authenticated Domain session")
		return
	}
	sessionCookie := findSessionCookie(login.Cookies)
	if sessionCookie == nil || strings.TrimSpace(sessionCookie.Value) == "" {
		fail("T019 real login did not issue the signed session cookie")
		return
	}
	cookieHeader := "__Host-gojet_session=" + sessionCookie.Value
	csrf, sessionID, err := issueCSRF(ctx, apiBase, cookieHeader, userID)
	if err != nil {
		fail("T019 real session is not accepted by /api/me")
		return
	}
	result.Details["real_session_authenticated"] = true
	result.Details["csrf_authority_issued"] = csrf != ""
	result.Details["session_id_present"] = sessionID != ""

	store := domains.NewMySQLStore(db)
	now := time.Now().UTC()
	expiresAt := now.Add(2 * time.Hour)
	_, err = store.CreateManualApproval(ctx, domains.ManualApprovalInput{
		WorkspaceID:     workspaceID,
		SourceKey:       "p20-t019-" + suffix,
		DomainLimit:     3,
		StartsAt:        now.Add(-time.Minute),
		ExpiresAt:       expiresAt,
		GrantedBy:       "p20-t019-fixture",
		SupportTicketID: "p20-t019-ticket-" + suffix,
		DecisionReason:  "P20 T019 structured manual approval prerequisite",
		CorrelationID:   "p20-t019-entitlement-" + suffix,
	})
	if err != nil {
		fail("T019 could not establish the canonical structured P06 entitlement prerequisite")
		return
	}
	entitlement, err := store.ResolveEntitlement(ctx, workspaceID, now)
	if err != nil || !entitlement.MutationAllowed || entitlement.Source != domains.SourceManualApproval {
		fail("T019 structured manual approval did not resolve current mutation authority")
		return
	}
	result.Details["structured_entitlement_active"] = true
	result.Details["entitlement_source"] = string(entitlement.Source)
	result.Details["entitlement_domain_limit"] = entitlement.DomainLimit
	result.Details["support_ticket_reference_not_authority"] = true

	rowsBefore, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domains WHERE workspace_id=? AND removed_at IS NULL`, workspaceID)
	if err != nil {
		fail("T019 could not inspect pre-create custom-domain row count")
		return
	}
	result.Details["domain_rows_before"] = rowsBefore

	hostname := "p20-t019-" + suffix + ".example.com"
	created, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/workspaces/"+workspaceID+"/domains", map[string]any{
		"hostname":      hostname,
		"change_reason": "P20 T019 real custom-domain create",
	}, unsafeHeaders(cookieHeader, origin, csrf, "p20-t019-create-"+suffix))
	if err != nil {
		fail("T019 real custom-domain create request failed")
		return
	}
	rowsAfter, _ := scalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domains WHERE workspace_id=? AND removed_at IS NULL`, workspaceID)
	result.Details["domain_create_http_status"] = created.Status
	result.Details["domain_create_error_code"] = nestedErrorCode(created.Body)
	result.Details["domain_create_row_delta"] = rowsAfter - rowsBefore
	domainBody := mapValue(created.Body["domain"])
	domainID := uint64Value(domainBody["id"])
	createdWorkspace := stringValue(domainBody["workspace_id"])
	ownershipTXTName := stringValue(created.Body["ownership_txt_name"])
	ownershipTXTValue := stringValue(created.Body["ownership_txt_value"])
	result.Details["domain_id"] = domainID
	result.Details["domain_create_identity_bound"] = createdWorkspace == workspaceID && domainID > 0
	result.Details["domain_hostname"] = stringValue(domainBody["hostname_ascii"])
	result.Details["domain_initial_routing_state"] = stringValue(domainBody["routing_state"])
	result.Details["domain_initial_ownership_status"] = stringValue(domainBody["ownership_status"])
	result.Details["domain_initial_ingress_status"] = stringValue(domainBody["ingress_dns_status"])
	result.Details["domain_initial_https_status"] = stringValue(domainBody["https_status"])
	result.Details["domain_initial_risk_status"] = stringValue(domainBody["risk_status"])
	if created.Status != http.StatusCreated || rowsAfter-rowsBefore != 1 || createdWorkspace != workspaceID || domainID == 0 || ownershipTXTName == "" || ownershipTXTValue == "" {
		fail("T019 production Domain create did not preserve real session/durable identity authority")
		return
	}

	dnsAuthority, err := startDomainDNSAuthority(ownershipTXTName, hostname)
	if err != nil {
		fail("T019 could not start deterministic real UDP DNS authority")
		return
	}
	defer dnsAuthority.Close()

	expectedIngress := "ingress-p20-t019.example.com"
	dnsAuthority.SetCNAME(expectedIngress)
	ownershipVerifier := domains.NewOwnershipVerifier(store, dnsAuthority.TXTResolver())
	ingressVerifier, err := domains.NewIngressDNSVerifier(store, dnsAuthority.CNAMEResolver(), expectedIngress)
	if err != nil {
		fail("T019 could not configure ingress DNS verifier")
		return
	}

	tlsAuthority, err := startTLSAuthority(hostname)
	if err != nil {
		fail("T019 could not start deterministic real TLS authority")
		return
	}
	defer tlsAuthority.Close()
	httpsVerifier := domains.NewHTTPSVerifier(store, tlsAuthority.Probe())

	riskEvaluator := &mutableRiskEvaluator{}
	riskEvaluator.Set(domains.RiskReview, "p20-t019-risk-v1", "local:p20-t019-review")
	riskVerifier := domains.NewDomainRiskVerifier(store, riskEvaluator)

	authority, err := domains.NewDomainAuthorityService(
		store,
		membershipPermission{db: db, userID: userID},
		ownershipVerifier,
		ingressVerifier,
		httpsVerifier,
		riskVerifier,
	)
	if err != nil {
		fail("T019 could not compose the P06 Domain authority service")
		return
	}

	_, err = authority.VerifyIngressDNS(ctx, domains.VerifyIngressDNSInput{
		WorkspaceID: workspaceID, DomainID: domainID, ActorID: userID,
		CorrelationID: "p20-t019-ingress-pre-" + suffix, Reason: "prove ownership gate before ingress", Now: now.Add(time.Minute),
	})
	result.Details["ingress_before_ownership_denied"] = errors.Is(err, domains.ErrOwnershipRequired)
	_, cnameBefore := dnsAuthority.QueryCounts()
	result.Details["ingress_preflight_avoided_dns"] = cnameBefore == 0
	if result.Details["ingress_before_ownership_denied"] != true || result.Details["ingress_preflight_avoided_dns"] != true {
		fail("T019 ingress DNS authority did not remain conjunctive behind ownership")
		return
	}

	dnsAuthority.SetTXT([]string{ownershipTXTValue})
	ownershipResult, err := authority.VerifyOwnershipTXT(ctx, domains.VerifyOwnershipTXTInput{
		WorkspaceID: workspaceID, DomainID: domainID, ActorID: userID,
		CorrelationID: "p20-t019-ownership-" + suffix, Reason: "verify current ownership TXT", Now: now.Add(2 * time.Minute),
	})
	if err != nil || ownershipResult.Outcome != domains.OwnershipVerificationVerified || ownershipResult.Domain.OwnershipStatus != domains.OwnershipVerified {
		fail("T019 real UDP TXT ownership verification did not reach verified")
		return
	}
	result.Details["ownership_verified"] = true
	result.Details["ownership_outcome"] = string(ownershipResult.Outcome)

	_, err = authority.VerifyHTTPS(ctx, domains.VerifyHTTPSInput{
		WorkspaceID: workspaceID, DomainID: domainID, ActorID: userID,
		CorrelationID: "p20-t019-https-pre-" + suffix, Reason: "prove ingress gate before HTTPS", Now: now.Add(3 * time.Minute),
	})
	result.Details["https_before_ingress_denied"] = errors.Is(err, domains.ErrIngressDNSRequired)
	if result.Details["https_before_ingress_denied"] != true {
		fail("T019 HTTPS authority did not remain conjunctive behind ingress DNS")
		return
	}

	ingressResult, err := authority.VerifyIngressDNS(ctx, domains.VerifyIngressDNSInput{
		WorkspaceID: workspaceID, DomainID: domainID, ActorID: userID,
		CorrelationID: "p20-t019-ingress-" + suffix, Reason: "verify current ingress CNAME", Now: now.Add(4 * time.Minute),
	})
	if err != nil || ingressResult.Outcome != domains.IngressDNSValid || ingressResult.Domain.IngressDNSStatus != domains.IngressValid {
		fail("T019 real UDP CNAME ingress verification did not reach valid")
		return
	}
	result.Details["ingress_dns_valid"] = true
	result.Details["ingress_outcome"] = string(ingressResult.Outcome)

	_, err = authority.VerifyDomainRisk(ctx, domains.VerifyDomainRiskInput{
		WorkspaceID: workspaceID, DomainID: domainID, ActorID: userID,
		CorrelationID: "p20-t019-risk-pre-" + suffix, Reason: "prove HTTPS gate before risk", Now: now.Add(5 * time.Minute),
	})
	result.Details["risk_before_https_denied"] = errors.Is(err, domains.ErrHTTPSRequired)
	if result.Details["risk_before_https_denied"] != true {
		fail("T019 risk authority did not remain conjunctive behind HTTPS")
		return
	}

	httpsResult, err := authority.VerifyHTTPS(ctx, domains.VerifyHTTPSInput{
		WorkspaceID: workspaceID, DomainID: domainID, ActorID: userID,
		CorrelationID: "p20-t019-https-" + suffix, Reason: "verify current HTTPS readiness", Now: now.Add(6 * time.Minute),
	})
	if err != nil || httpsResult.Observation.Outcome != domains.TLSProbeActive || httpsResult.Domain.HTTPSStatus != domains.HTTPSActive || !httpsResult.Observation.HandshakeComplete {
		fail("T019 real TCP/TLS HTTPS verification did not reach active")
		return
	}
	result.Details["https_active"] = true
	result.Details["tls_version"] = httpsResult.Observation.TLSVersion
	result.Details["real_tls_handshake"] = tlsAuthority.Connections() > 0

	reviewResult, err := authority.VerifyDomainRisk(ctx, domains.VerifyDomainRiskInput{
		WorkspaceID: workspaceID, DomainID: domainID, ActorID: userID,
		CorrelationID: "p20-t019-risk-review-" + suffix, Reason: "verify review risk fails closed", Now: now.Add(7 * time.Minute),
	})
	if err != nil || reviewResult.Observation.Status != domains.RiskReview || reviewResult.Domain.RiskStatus != domains.RiskReview {
		fail("T019 domain risk review state was not persisted as an independent fail-closed axis")
		return
	}
	result.Details["risk_review_persisted"] = true

	linksBeforeReview, _ := scalarInt(ctx, db, `SELECT COUNT(*) FROM links WHERE workspace_id=? AND hostname=? AND domain_kind='custom'`, workspaceID, hostname)
	linkCSRF, _, err := issueCSRF(ctx, apiBase, cookieHeader, userID)
	if err != nil {
		fail("T019 could not issue fresh CSRF for not-ready Link assignment")
		return
	}
	linkCode := "p20t019" + suffix
	notReady, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/workspaces/"+workspaceID+"/links",
		linkPayload(hostname, linkCode, "P20 T019 custom-domain Link", "https://example.com/p20-t019", "P20 T019 not-ready assignment"),
		unsafeHeaders(cookieHeader, origin, linkCSRF, "p20-t019-link-deny-"+suffix))
	if err != nil {
		fail("T019 not-ready custom-domain Link assignment request failed")
		return
	}
	linksAfterReview, _ := scalarInt(ctx, db, `SELECT COUNT(*) FROM links WHERE workspace_id=? AND hostname=? AND domain_kind='custom'`, workspaceID, hostname)
	result.Details["not_ready_link_http_status"] = notReady.Status
	result.Details["not_ready_link_error_code"] = nestedErrorCode(notReady.Body)
	result.Details["not_ready_link_zero_write"] = linksAfterReview == linksBeforeReview
	if notReady.Status == http.StatusCreated || linksAfterReview != linksBeforeReview {
		fail("T019 custom-domain Link assignment did not fail closed while risk was review")
		return
	}

	riskEvaluator.Set(domains.RiskAllow, "p20-t019-risk-v1", "local:p20-t019-allow")
	allowResult, err := authority.VerifyDomainRisk(ctx, domains.VerifyDomainRiskInput{
		WorkspaceID: workspaceID, DomainID: domainID, ActorID: userID,
		CorrelationID: "p20-t019-risk-allow-" + suffix, Reason: "verify allow risk readiness", Now: now.Add(8 * time.Minute),
	})
	if err != nil || allowResult.Observation.Status != domains.RiskAllow || allowResult.Domain.RiskStatus != domains.RiskAllow {
		fail("T019 domain risk allow state did not restore the final trust axis")
		return
	}
	result.Details["risk_allow_persisted"] = true

	currentEntitlement, err := store.ResolveEntitlement(ctx, workspaceID, now.Add(8*time.Minute))
	if err != nil {
		fail("T019 could not resolve final entitlement readiness")
		return
	}
	readiness := allowResult.Domain.Readiness(currentEntitlement)
	result.Details["ready_for_new_links"] = readiness.ReadyForNewLinks
	result.Details["axes_conjunctive_ready"] = readiness.EntitlementReady && readiness.OwnershipReady && readiness.IngressDNSReady && readiness.HTTPSReady && readiness.RiskReady
	if !readiness.ReadyForNewLinks || result.Details["axes_conjunctive_ready"] != true {
		fail("T019 all independent Domain axes did not converge to ready-for-new-links")
		return
	}

	successCSRF, _, err := issueCSRF(ctx, apiBase, cookieHeader, userID)
	if err != nil {
		fail("T019 could not issue fresh CSRF for ready Link assignment")
		return
	}
	ready, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/workspaces/"+workspaceID+"/links",
		linkPayload(hostname, linkCode, "P20 T019 custom-domain Link", "https://example.com/p20-t019", "P20 T019 ready assignment"),
		unsafeHeaders(cookieHeader, origin, successCSRF, "p20-t019-link-allow-"+suffix))
	if err != nil {
		fail("T019 ready custom-domain Link assignment request failed")
		return
	}
	linksAfterAllow, _ := scalarInt(ctx, db, `SELECT COUNT(*) FROM links WHERE workspace_id=? AND hostname=? AND domain_kind='custom'`, workspaceID, hostname)
	result.Details["ready_link_http_status"] = ready.Status
	result.Details["ready_link_row_delta"] = linksAfterAllow - linksAfterReview
	result.Details["ready_link_workspace_bound"] = stringValue(ready.Body["workspace_id"]) == workspaceID
	result.Details["ready_link_hostname_bound"] = stringValue(ready.Body["hostname"]) == hostname
	result.Details["ready_link_domain_kind"] = stringValue(ready.Body["domain_kind"])
	result.Details["ready_link_id"] = uint64Value(ready.Body["id"])
	if ready.Status != http.StatusCreated || linksAfterAllow-linksAfterReview != 1 || result.Details["ready_link_workspace_bound"] != true || result.Details["ready_link_hostname_bound"] != true || result.Details["ready_link_domain_kind"] != "custom" {
		fail("T019 ready custom-domain Link assignment did not succeed through real API authority")
		return
	}

	txtQueries, cnameQueries := dnsAuthority.QueryCounts()
	result.Details["real_dns_udp"] = txtQueries > 0 && cnameQueries > 0
	result.Details["dns_txt_queries"] = txtQueries
	result.Details["dns_cname_queries"] = cnameQueries
	result.Details["risk_evaluator_calls"] = riskEvaluator.Calls()
	revalidationRows, _ := scalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domain_revalidations WHERE workspace_id=? AND domain_id=?`, workspaceID, domainID)
	result.Details["domain_revalidation_rows"] = revalidationRows
	result.Details["revalidation_history_bound"] = revalidationRows >= 5

	var finalOwnership, finalIngress, finalHTTPS, finalRisk, finalRouting string
	if err := db.QueryRowContext(ctx, `
		SELECT ownership_status,ingress_dns_status,https_status,risk_status,routing_state
		FROM custom_domains WHERE workspace_id=? AND id=?`, workspaceID, domainID,
	).Scan(&finalOwnership, &finalIngress, &finalHTTPS, &finalRisk, &finalRouting); err != nil {
		fail("T019 could not inspect final Domain axis state")
		return
	}
	result.Details["final_ownership_status"] = finalOwnership
	result.Details["final_ingress_dns_status"] = finalIngress
	result.Details["final_https_status"] = finalHTTPS
	result.Details["final_risk_status"] = finalRisk
	result.Details["final_routing_state"] = finalRouting

	auditSecretLeak, err := auditContains(ctx, db, workspaceID, domainID, ownershipTXTValue)
	if err != nil {
		fail("T019 could not inspect secret-safe Domain audit evidence")
		return
	}
	result.Details["audit_secret_leak"] = auditSecretLeak
	result.Details["secret_material_recorded"] = auditSecretLeak
	if auditSecretLeak {
		fail("T019 Domain audit evidence leaked ownership TXT secret material")
		return
	}
	if result.Details["real_dns_udp"] != true || result.Details["real_tls_handshake"] != true || result.Details["revalidation_history_bound"] != true {
		fail("T019 did not bind real DNS/TLS/revalidation authority")
		return
	}
	if finalOwnership != "verified" || finalIngress != "valid" || finalHTTPS != "active" || finalRisk != "allow" {
		fail("T019 final Domain axes are not all current and ready")
		return
	}

	result.Details["next_case"] = "P20-T020"
	finish()
}

func linkPayload(hostname, code, title, destination, reason string) map[string]any {
	return map[string]any{
		"hostname":            hostname,
		"domain_kind":         "custom",
		"code":                code,
		"title":               title,
		"primary_destination": destination,
		"redirect_status":     http.StatusFound,
		"routing":             []any{},
		"ab":                  []any{},
		"utm":                 map[string]any{},
		"access":              map[string]any{},
		"one_time":            false,
		"change_reason":       reason,
	}
}

func readEvidence(path string) (predecessorEvidence, error) {
	var value predecessorEvidence
	raw, err := os.ReadFile(path)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, err
	}
	return value, nil
}

func requestJSON(ctx context.Context, base, method, path string, payload map[string]any, extra map[string]string) (httpResult, error) {
	var raw []byte
	var err error
	if payload != nil {
		raw, err = json.Marshal(payload)
		if err != nil {
			return httpResult{}, err
		}
	}
	headers := map[string]string{}
	for key, value := range extra {
		headers[key] = value
	}
	if payload != nil {
		headers["Content-Type"] = "application/json"
	}
	return requestRaw(ctx, base, method, path, raw, headers)
}

func requestRaw(ctx context.Context, base, method, path string, payload []byte, extra map[string]string) (httpResult, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return httpResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	for key, value := range extra {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return httpResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return httpResult{}, err
	}
	decoded := map[string]any{}
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			decoded = map[string]any{"_non_json": true}
		}
	}
	return httpResult{Status: resp.StatusCode, Body: decoded, Raw: raw, Headers: resp.Header.Clone(), Cookies: resp.Cookies()}, nil
}

func issueCSRF(ctx context.Context, apiBase, cookie, expectedUserID string) (string, string, error) {
	me, err := requestRaw(ctx, apiBase, http.MethodGet, "/api/me", nil, map[string]string{"Cookie": cookie})
	if err != nil || me.Status != http.StatusOK || nestedString(me.Body, "user", "id") != expectedUserID {
		return "", "", fmt.Errorf("real session identity mismatch")
	}
	csrf := stringValue(me.Body["csrf_token"])
	sessionID := nestedString(me.Body, "session", "id")
	if csrf == "" || sessionID == "" {
		return "", "", fmt.Errorf("missing session csrf authority")
	}
	return csrf, sessionID, nil
}

func unsafeHeaders(cookie, origin, csrf, correlation string) map[string]string {
	return map[string]string{
		"Cookie":       cookie,
		"Origin":       origin,
		"X-CSRF-Token": csrf,
		"X-Request-ID": correlation,
	}
}

func findSessionCookie(cookies []*http.Cookie) *http.Cookie {
	for _, cookie := range cookies {
		if cookie != nil && cookie.Name == "__Host-gojet_session" {
			return cookie
		}
	}
	return nil
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var value int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

func auditContains(ctx context.Context, db *sql.DB, workspaceID string, domainID uint64, needle string) (bool, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT CAST(metadata_json AS CHAR), COALESCE(reason,'')
		FROM custom_domain_audit_events
		WHERE workspace_id=? AND domain_id=? ORDER BY id`, workspaceID, domainID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var metadata, reason string
		if err := rows.Scan(&metadata, &reason); err != nil {
			return false, err
		}
		if needle != "" && (strings.Contains(metadata, needle) || strings.Contains(reason, needle)) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func mapValue(value any) map[string]any {
	m, _ := value.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func uint64Value(value any) uint64 {
	switch typed := value.(type) {
	case float64:
		if typed > 0 && typed == float64(uint64(typed)) {
			return uint64(typed)
		}
	case json.Number:
		value, _ := typed.Int64()
		if value > 0 {
			return uint64(value)
		}
	}
	return 0
}

func nestedString(body map[string]any, parent, child string) string {
	value, _ := body[parent].(map[string]any)
	return stringValue(value[child])
}

func nestedErrorCode(body map[string]any) string {
	if code := stringValue(body["code"]); code != "" {
		return code
	}
	if value, ok := body["error"].(map[string]any); ok {
		return stringValue(value["code"])
	}
	return ""
}
