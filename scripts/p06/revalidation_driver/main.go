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
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/net/dns/dnsmessage"
)

type caseResult struct {
	CaseID  string         `json:"case_id"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details"`
	Errors  []string       `json:"errors"`
}

type periodicDNSAuthority struct {
	conn          *net.UDPConn
	hostname      string
	ownershipName string
	mu            sync.RWMutex
	txtRecords    []string
	cnameTarget   string
	queries       int
	queryNames    []string
	queryTypes    []string
	done          chan struct{}
}

type periodicTLSAuthority struct {
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

type periodicRiskEvaluator struct {
	mu          sync.RWMutex
	observation domains.DomainRiskObservation
	calls       int
}

func (e *periodicRiskEvaluator) Set(status domains.DomainRiskStatus, cycle int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.observation = domains.DomainRiskObservation{
		Status:        status,
		PolicyVersion: "domain-risk-t014-v1",
		EvidenceRef:   fmt.Sprintf("risk:t014:%02d:%s", cycle, status),
	}
}

func (e *periodicRiskEvaluator) Evaluate(_ context.Context, _ string) (domains.DomainRiskObservation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	return e.observation, nil
}

func (e *periodicRiskEvaluator) Calls() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.calls
}

type historyRow struct {
	ID                 uint64
	Axis               string
	Result             string
	CheckedAt          time.Time
	NextDueAt          time.Time
	CorrelationID      string
	SchedulePolicy     string
	PreviousCheckedRaw sql.NullString
}

func main() {
	caseFlag := flag.String("case", "P06-T014", "P06 periodic revalidation case ID")
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
	if *caseFlag != "P06-T014" {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, fmt.Sprintf("unsupported case %s", *caseFlag))
	} else if err := caseT014(ctx, db, &result); err != nil {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, err.Error())
	}
	writeJSON(map[string]caseResult{*caseFlag: result})
	if result.Status != "PASS" {
		os.Exit(1)
	}
}

func caseT014(ctx context.Context, db *sql.DB, out *caseResult) error {
	workspace := "p06-t014-periodic"
	hostname := "periodic-t014.example.com"
	expectedIngress := "edge.t014.gojet-ingress.example.net"
	wrongIngress := "wrong.t014.gojet-ingress.example.net"
	store := domains.NewMySQLStore(db)
	now := fixedNow()
	if _, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace,
		SourceKey: "subscription-business-t014",
		Status: domains.EntitlementActive,
		DomainLimit: 1,
		StartsAt: now.Add(-24 * time.Hour),
		DecisionReason: "T014 active entitlement fixture",
	}, "corr-p06-t014-plan"); err != nil {
		return err
	}
	created, err := store.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspace,
		ActorID: "actor-t014",
		CorrelationID: "corr-p06-t014-create",
		Reason: "create domain before periodic revalidation",
		Hostname: hostname,
		Now: now,
	})
	if err != nil {
		return err
	}

	dnsAuthority, err := startPeriodicDNSAuthority(created.Domain.HostnameASCII, created.OwnershipTXTName)
	if err != nil {
		return err
	}
	defer dnsAuthority.Close()
	dnsAuthority.SetTXT([]string{created.OwnershipTXTValue})
	dnsAuthority.SetCNAME(expectedIngress)

	roots, activeCert, wrongCert, err := createTestPKI(hostname)
	if err != nil {
		return err
	}
	tlsAuthority, err := startPeriodicTLSAuthority(activeCert, wrongCert)
	if err != nil {
		return err
	}
	defer tlsAuthority.Close()
	tlsAuthority.SetMode("active")

	resolver := dnsAuthority.Resolver()
	ownership := domains.NewOwnershipVerifier(store, domains.NetTXTResolver{Resolver: resolver})
	ingress, err := domains.NewIngressDNSVerifier(store, domains.NetCNAMEResolver{Resolver: resolver}, expectedIngress)
	if err != nil {
		return err
	}
	https := domains.NewHTTPSVerifier(store, domains.NetTLSReadinessProbe{
		RootCAs: roots,
		HandshakeTimeout: time.Second,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "tcp", tlsAuthority.Address())
		},
	})
	riskEvaluator := &periodicRiskEvaluator{}
	riskEvaluator.Set(domains.RiskAllow, 1)
	risk := domains.NewDomainRiskVerifier(store, riskEvaluator)
	policy := domains.RevalidationSchedulePolicy{
		Version: "periodic-t014-schedule-v1",
		EntitlementInterval: 5 * time.Minute,
		OwnershipInterval:   10 * time.Minute,
		IngressDNSInterval:  15 * time.Minute,
		HTTPSInterval:       20 * time.Minute,
		RiskInterval:        25 * time.Minute,
	}
	revalidator, err := domains.NewPeriodicRevalidator(store, ownership, ingress, https, risk, policy)
	if err != nil {
		return err
	}

	readySequence := []bool{}
	statusSequence := []map[string]string{}
	runCycle := func(cycle int, at time.Time) (domains.PeriodicRevalidationResult, error) {
		result, err := revalidator.Run(ctx, domains.PeriodicRevalidationInput{
			WorkspaceID: workspace,
			DomainID: created.Domain.ID,
			CorrelationID: fmt.Sprintf("corr-p06-t014-cycle-%d", cycle),
			Now: at,
		})
		if err != nil {
			return domains.PeriodicRevalidationResult{}, err
		}
		readiness := result.Domain.Readiness(result.Entitlement)
		readySequence = append(readySequence, readiness.ReadyForRouting && readiness.ReadyForNewLinks)
		statusSequence = append(statusSequence, map[string]string{
			"ownership": string(result.Domain.OwnershipStatus),
			"ingress_dns": string(result.Domain.IngressDNSStatus),
			"https": string(result.Domain.HTTPSStatus),
			"risk": string(result.Domain.RiskStatus),
		})
		if len(result.Axes) != 5 {
			return domains.PeriodicRevalidationResult{}, fmt.Errorf("cycle %d axis count=%d want=5", cycle, len(result.Axes))
		}
		return result, nil
	}

	cycle1, err := runCycle(1, now.Add(5*time.Minute))
	if err != nil {
		return fmt.Errorf("cycle 1 ready baseline: %w", err)
	}
	if !readySequence[0] || cycle1.Domain.RiskStatus != domains.RiskAllow || cycle1.Domain.HTTPSStatus != domains.HTTPSActive || cycle1.Domain.IngressDNSStatus != domains.IngressValid || cycle1.Domain.OwnershipStatus != domains.OwnershipVerified {
		return fmt.Errorf("cycle 1 did not reach ready: domain=%+v", cycle1.Domain)
	}

	riskEvaluator.Set(domains.RiskStale, 2)
	cycle2, err := runCycle(2, now.Add(10*time.Minute))
	if err != nil {
		return fmt.Errorf("cycle 2 risk stale: %w", err)
	}
	if readySequence[1] || cycle2.Domain.RiskStatus != domains.RiskStale {
		return fmt.Errorf("cycle 2 stale risk did not fail closed: risk=%s ready=%v", cycle2.Domain.RiskStatus, readySequence[1])
	}

	riskEvaluator.Set(domains.RiskAllow, 3)
	tlsAuthority.SetMode("error")
	cycle3, err := runCycle(3, now.Add(15*time.Minute))
	if err != nil {
		return fmt.Errorf("cycle 3 TLS regression: %w", err)
	}
	if readySequence[2] || cycle3.Domain.HTTPSStatus != domains.HTTPSError || cycle3.Domain.RiskStatus != domains.RiskAllow {
		return fmt.Errorf("cycle 3 TLS regression did not override prior pass: https=%s risk=%s ready=%v", cycle3.Domain.HTTPSStatus, cycle3.Domain.RiskStatus, readySequence[2])
	}

	tlsAuthority.SetMode("active")
	dnsAuthority.SetCNAME(wrongIngress)
	riskEvaluator.Set(domains.RiskAllow, 4)
	cycle4, err := runCycle(4, now.Add(20*time.Minute))
	if err != nil {
		return fmt.Errorf("cycle 4 ingress drift: %w", err)
	}
	if readySequence[3] || cycle4.Domain.IngressDNSStatus != domains.IngressInvalid || cycle4.Domain.HTTPSStatus != domains.HTTPSActive {
		return fmt.Errorf("cycle 4 ingress drift did not fail closed independently: ingress=%s https=%s ready=%v", cycle4.Domain.IngressDNSStatus, cycle4.Domain.HTTPSStatus, readySequence[3])
	}

	dnsAuthority.SetCNAME(expectedIngress)
	riskEvaluator.Set(domains.RiskAllow, 5)
	cycle5, err := runCycle(5, now.Add(25*time.Minute))
	if err != nil {
		return fmt.Errorf("cycle 5 recovery: %w", err)
	}
	if !readySequence[4] || cycle5.Domain.OwnershipStatus != domains.OwnershipVerified || cycle5.Domain.IngressDNSStatus != domains.IngressValid || cycle5.Domain.HTTPSStatus != domains.HTTPSActive || cycle5.Domain.RiskStatus != domains.RiskAllow {
		return fmt.Errorf("cycle 5 did not recover ready state: domain=%+v ready=%v", cycle5.Domain, readySequence[4])
	}

	rows, err := loadHistory(ctx, db, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	if len(rows) != 25 {
		return fmt.Errorf("periodic revalidation rows=%d want=25", len(rows))
	}
	byAxis := map[string][]historyRow{}
	for _, row := range rows {
		byAxis[row.Axis] = append(byAxis[row.Axis], row)
		if !row.NextDueAt.After(row.CheckedAt) {
			return fmt.Errorf("axis %s row %d next_due_at=%s is not after checked_at=%s", row.Axis, row.ID, row.NextDueAt, row.CheckedAt)
		}
		if row.SchedulePolicy != policy.Version {
			return fmt.Errorf("axis %s row %d schedule policy=%q want=%q", row.Axis, row.ID, row.SchedulePolicy, policy.Version)
		}
	}
	wantAxisResults := map[string][]string{
		"entitlement": {"pass", "pass", "pass", "pass", "pass"},
		"ownership":   {"pass", "pass", "pass", "pass", "pass"},
		"ingress_dns": {"pass", "pass", "pass", "fail", "pass"},
		"https":       {"pass", "pass", "fail", "pass", "pass"},
		"risk":        {"pass", "stale", "pass", "pass", "pass"},
	}
	for axis, want := range wantAxisResults {
		axisRows := byAxis[axis]
		if len(axisRows) != 5 {
			return fmt.Errorf("axis %s row count=%d want=5", axis, len(axisRows))
		}
		got := make([]string, 0, 5)
		for index, row := range axisRows {
			got = append(got, row.Result)
			if index == 0 {
				if row.PreviousCheckedRaw.Valid {
					return fmt.Errorf("axis %s first row unexpectedly has previous_checked_at=%s", axis, row.PreviousCheckedRaw.String)
				}
			} else {
				if !row.PreviousCheckedRaw.Valid {
					return fmt.Errorf("axis %s row %d missing previous_checked_at", axis, row.ID)
				}
				previous, err := time.Parse(time.RFC3339Nano, row.PreviousCheckedRaw.String)
				if err != nil {
					return fmt.Errorf("axis %s row %d parse previous_checked_at: %w", axis, row.ID, err)
				}
				if !previous.Equal(axisRows[index-1].CheckedAt.UTC()) {
					return fmt.Errorf("axis %s row %d previous_checked_at=%s want=%s", axis, row.ID, previous, axisRows[index-1].CheckedAt.UTC())
				}
			}
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			return fmt.Errorf("axis %s results=%v want=%v", axis, got, want)
		}
	}

	auditCount, err := scalarInt(ctx, db, `
		SELECT COUNT(*) FROM custom_domain_audit_events
		WHERE workspace_id = ? AND domain_id = ? AND action = 'domain.revalidation.periodic'`, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	if auditCount != 5 {
		return fmt.Errorf("periodic audit count=%d want=5", auditCount)
	}
	correlations, err := queryStrings(ctx, db, `
		SELECT correlation_id FROM custom_domain_audit_events
		WHERE workspace_id = ? AND domain_id = ? AND action = 'domain.revalidation.periodic'
		ORDER BY id`, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	for cycle, correlation := range correlations {
		want := fmt.Sprintf("corr-p06-t014-cycle-%d", cycle+1)
		if correlation != want {
			return fmt.Errorf("periodic audit correlation[%d]=%q want=%q", cycle, correlation, want)
		}
	}

	dnsQueries, dnsNames, dnsTypes := dnsAuthority.Evidence()
	if dnsQueries < 10 {
		return fmt.Errorf("periodic DNS authority queries=%d want at least 10", dnsQueries)
	}
	exactTXT := 0
	exactCNAME := 0
	for index, name := range dnsNames {
		if name == strings.ToLower(created.OwnershipTXTName)+"." && dnsTypes[index] == dnsmessage.TypeTXT.String() {
			exactTXT++
		}
		if name == strings.ToLower(created.Domain.HostnameASCII)+"." && dnsTypes[index] == dnsmessage.TypeCNAME.String() {
			exactCNAME++
		}
	}
	if exactTXT < 5 || exactCNAME < 5 {
		return fmt.Errorf("periodic exact DNS evidence TXT=%d CNAME=%d want >=5 each", exactTXT, exactCNAME)
	}
	tlsAccepts, tlsModes := tlsAuthority.Evidence()
	if tlsAccepts < 5 {
		return fmt.Errorf("periodic TLS connections=%d want at least 5", tlsAccepts)
	}
	if riskEvaluator.Calls() != 5 {
		return fmt.Errorf("periodic risk evaluator calls=%d want=5", riskEvaluator.Calls())
	}

	axisSummary := map[string][]string{}
	for axis, axisRows := range byAxis {
		for _, row := range axisRows {
			axisSummary[axis] = append(axisSummary[axis], row.Result)
		}
	}
	out.Details = map[string]any{
		"domain_id": created.Domain.ID,
		"hostname_ascii": created.Domain.HostnameASCII,
		"periodic_cycles": 5,
		"history_rows": len(rows),
		"axis_results": axisSummary,
		"ready_sequence": readySequence,
		"status_sequence": statusSequence,
		"schedule_policy_version": policy.Version,
		"all_next_due_after_checked_at": true,
		"previous_checked_chain_verified": true,
		"periodic_audit_events": auditCount,
		"dns_transport": "udp",
		"authoritative_dns_queries": dnsQueries,
		"exact_txt_queries": exactTXT,
		"exact_cname_queries": exactCNAME,
		"tls_transport": "tcp+tls",
		"tls_connections": tlsAccepts,
		"tls_modes": tlsModes,
		"risk_evaluator_calls": riskEvaluator.Calls(),
		"stale_prior_evidence_kept_ready": false,
	}
	return nil
}

func loadHistory(ctx context.Context, db *sql.DB, workspace string, domainID uint64) ([]historyRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, axis, result, checked_at, next_due_at, correlation_id,
		       JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.schedule_policy_version')),
		       JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.previous_checked_at'))
		FROM custom_domain_revalidations
		WHERE workspace_id = ? AND domain_id = ?
		  AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.trigger')) = 'periodic'
		ORDER BY id`, workspace, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []historyRow{}
	for rows.Next() {
		var row historyRow
		if err := rows.Scan(&row.ID, &row.Axis, &row.Result, &row.CheckedAt, &row.NextDueAt, &row.CorrelationID, &row.SchedulePolicy, &row.PreviousCheckedRaw); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func startPeriodicDNSAuthority(hostname, ownershipName string) (*periodicDNSAuthority, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		return nil, err
	}
	a := &periodicDNSAuthority{
		conn: conn,
		hostname: strings.ToLower(strings.TrimSuffix(hostname, ".")) + ".",
		ownershipName: strings.ToLower(strings.TrimSuffix(ownershipName, ".")) + ".",
		done: make(chan struct{}),
	}
	go a.serve()
	return a, nil
}

func (a *periodicDNSAuthority) SetTXT(records []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.txtRecords = append([]string(nil), records...)
}

func (a *periodicDNSAuthority) SetCNAME(target string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cnameTarget = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(target), ".")) + "."
}

func (a *periodicDNSAuthority) Resolver() *net.Resolver {
	address := a.conn.LocalAddr().String()
	return &net.Resolver{
		PreferGo: true,
		StrictErrors: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "udp4", address)
		},
	}
}

func (a *periodicDNSAuthority) Evidence() (int, []string, []string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.queries, append([]string(nil), a.queryNames...), append([]string(nil), a.queryTypes...)
}

func (a *periodicDNSAuthority) Close() {
	_ = a.conn.Close()
	<-a.done
}

func (a *periodicDNSAuthority) serve() {
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
		q := query.Questions[0]
		qname := strings.ToLower(q.Name.String())
		a.mu.RLock()
		txt := append([]string(nil), a.txtRecords...)
		cname := a.cnameTarget
		a.mu.RUnlock()
		a.mu.Lock()
		a.queries++
		a.queryNames = append(a.queryNames, qname)
		a.queryTypes = append(a.queryTypes, q.Type.String())
		a.mu.Unlock()

		response := dnsmessage.Message{
			Header: dnsmessage.Header{ID: query.Header.ID, Response: true, Authoritative: true, RecursionDesired: query.Header.RecursionDesired, RCode: dnsmessage.RCodeSuccess},
			Questions: query.Questions,
		}
		if qname == a.ownershipName && q.Type == dnsmessage.TypeTXT {
			for _, record := range txt {
				response.Answers = append(response.Answers, dnsmessage.Resource{
					Header: dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 0},
					Body: &dnsmessage.TXTResource{TXT: []string{record}},
				})
			}
		} else if qname == a.hostname && q.Type == dnsmessage.TypeCNAME && cname != "" {
			targetName, err := dnsmessage.NewName(cname)
			if err != nil {
				continue
			}
			response.Answers = []dnsmessage.Resource{{
				Header: dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 0},
				Body: &dnsmessage.CNAMEResource{CNAME: targetName},
			}}
		}
		packed, err := response.Pack()
		if err == nil {
			_, _ = a.conn.WriteToUDP(packed, remote)
		}
	}
}

func createTestPKI(hostname string) (*x509.CertPool, tls.Certificate, tls.Certificate, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, tls.Certificate{}, tls.Certificate{}, err
	}
	now := time.Now().UTC()
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(14001), Subject: pkix.Name{CommonName: "GoJet P06 T014 Test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
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
	active, err := createLeaf(caCert, caKey, caDER, hostname, 14002, now)
	if err != nil {
		return nil, tls.Certificate{}, tls.Certificate{}, err
	}
	wrong, err := createLeaf(caCert, caKey, caDER, "wrong-t014.example.net", 14003, now)
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
		SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: hostname}, DNSNames: []string{hostname},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(12 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{leafDER, caDER}, PrivateKey: key, Leaf: leaf}, nil
}

func startPeriodicTLSAuthority(active, wrong tls.Certificate) (*periodicTLSAuthority, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	a := &periodicTLSAuthority{
		listener: listener,
		activeConfig: &tls.Config{Certificates: []tls.Certificate{active}, MinVersion: tls.VersionTLS12},
		errorConfig: &tls.Config{Certificates: []tls.Certificate{wrong}, MinVersion: tls.VersionTLS12},
		mode: "active", done: make(chan struct{}),
	}
	go a.serve()
	return a, nil
}

func (a *periodicTLSAuthority) Address() string { return a.listener.Addr().String() }
func (a *periodicTLSAuthority) SetMode(mode string) { a.mu.Lock(); a.mode = mode; a.mu.Unlock() }
func (a *periodicTLSAuthority) Evidence() (int, []string) {
	a.mu.RLock(); defer a.mu.RUnlock(); return a.accepts, append([]string(nil), a.modes...)
}
func (a *periodicTLSAuthority) Close() { _ = a.listener.Close(); <-a.done; a.wg.Wait() }

func (a *periodicTLSAuthority) serve() {
	defer close(a.done)
	for {
		conn, err := a.listener.Accept()
		if err != nil { return }
		a.mu.Lock(); mode := a.mode; a.accepts++; a.modes = append(a.modes, mode); a.mu.Unlock()
		a.wg.Add(1)
		go func(conn net.Conn, mode string) {
			defer a.wg.Done(); defer conn.Close()
			config := a.activeConfig
			if mode == "error" { config = a.errorConfig }
			tlsConn := tls.Server(conn, config)
			_ = tlsConn.Handshake()
		}(conn, mode)
	}
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var value int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil { return 0, err }
	return value, nil
}

func queryStrings(ctx context.Context, db *sql.DB, query string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil { return nil, err }
	defer rows.Close()
	values := []string{}
	for rows.Next() { var value string; if err := rows.Scan(&value); err != nil { return nil, err }; values = append(values, value) }
	return values, rows.Err()
}

func fixedNow() time.Time { return time.Date(2026, 8, 21, 16, 0, 0, 0, time.UTC) }

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout); encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil { failFatal(err.Error()) }
}
func failFatal(message string) { _ = json.NewEncoder(os.Stderr).Encode(map[string]any{"status": "FAIL", "error": message}); os.Exit(2) }

func init() { sort.Strings([]string{}) }
