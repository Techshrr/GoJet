package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
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

type cnameAuthority struct {
	conn        *net.UDPConn
	expected    string
	mu          sync.RWMutex
	missing     bool
	target      string
	queries     int
	queryNames  []string
	queryTypes  []string
	done        chan struct{}
}

func main() {
	caseFlag := flag.String("case", "P06-T011", "P06 ingress DNS case ID")
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
	case "P06-T011":
		if err := caseT011(ctx, db, &result); err != nil {
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

func caseT011(ctx context.Context, db *sql.DB, out *caseResult) error {
	workspace := "p06-t011-ingress"
	store := domains.NewMySQLStore(db)
	now := fixedNow()
	if _, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace,
		SourceKey: "subscription-business-t011",
		Status: domains.EntitlementActive,
		DomainLimit: 1,
		StartsAt: now.Add(-24 * time.Hour),
		DecisionReason: "T011 active entitlement fixture",
	}, "corr-p06-t011-plan"); err != nil {
		return err
	}

	created, err := store.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspace,
		ActorID: "actor-t011",
		CorrelationID: "corr-p06-t011-create",
		Reason: "create domain before ingress DNS verification",
		Hostname: "ingress-t011.example.com",
		Now: now,
	})
	if err != nil {
		return err
	}
	verifiedAt := now.Add(time.Minute)
	if _, err := db.ExecContext(ctx, `
		UPDATE custom_domains
		SET ownership_status = 'verified', ownership_verified_at = ?
		WHERE workspace_id = ? AND id = ?`, verifiedAt, workspace, created.Domain.ID); err != nil {
		return fmt.Errorf("seed T011 ownership precondition: %w", err)
	}

	expectedTarget := "edge.t011.gojet-ingress.example.net"
	wrongTarget := "wrong.t011.gojet-ingress.example.net"
	authority, err := startCNAMEAuthority(created.Domain.HostnameASCII)
	if err != nil {
		return err
	}
	defer authority.Close()
	verifier, err := domains.NewIngressDNSVerifier(store, authority.Resolver(), expectedTarget)
	if err != nil {
		return fmt.Errorf("construct ingress verifier: %w", err)
	}

	authority.SetMissing()
	missing, err := verifier.Verify(ctx, domains.VerifyIngressDNSInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t011",
		CorrelationID: "corr-p06-t011-missing",
		Reason: "verify missing ingress CNAME",
		Now: now.Add(2 * time.Minute),
	})
	if err != nil {
		return fmt.Errorf("missing ingress verification: %w", err)
	}
	if missing.Outcome != domains.IngressDNSMissing || missing.Domain.IngressDNSStatus != domains.IngressInvalid {
		return fmt.Errorf("missing ingress state outcome=%s status=%s", missing.Outcome, missing.Domain.IngressDNSStatus)
	}

	authority.SetTarget(wrongTarget)
	mismatch, err := verifier.Verify(ctx, domains.VerifyIngressDNSInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t011",
		CorrelationID: "corr-p06-t011-mismatch",
		Reason: "verify wrong ingress CNAME",
		Now: now.Add(3 * time.Minute),
	})
	if err != nil {
		return fmt.Errorf("wrong ingress verification: %w", err)
	}
	if mismatch.Outcome != domains.IngressDNSMismatch || mismatch.Domain.IngressDNSStatus != domains.IngressInvalid {
		return fmt.Errorf("wrong ingress state outcome=%s status=%s", mismatch.Outcome, mismatch.Domain.IngressDNSStatus)
	}

	authority.SetTarget(expectedTarget)
	valid, err := verifier.Verify(ctx, domains.VerifyIngressDNSInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t011",
		CorrelationID: "corr-p06-t011-valid",
		Reason: "verify expected ingress CNAME",
		Now: now.Add(4 * time.Minute),
	})
	if err != nil {
		return fmt.Errorf("valid ingress verification: %w", err)
	}
	if valid.Outcome != domains.IngressDNSValid || valid.Domain.IngressDNSStatus != domains.IngressValid {
		return fmt.Errorf("expected ingress did not become valid: outcome=%s status=%s", valid.Outcome, valid.Domain.IngressDNSStatus)
	}

	authority.SetTarget(wrongTarget)
	drift, err := verifier.Verify(ctx, domains.VerifyIngressDNSInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t011",
		CorrelationID: "corr-p06-t011-drift",
		Reason: "detect ingress DNS drift",
		Now: now.Add(5 * time.Minute),
	})
	if err != nil {
		return fmt.Errorf("drift ingress verification: %w", err)
	}
	if drift.Outcome != domains.IngressDNSDrift || drift.Domain.IngressDNSStatus != domains.IngressInvalid {
		return fmt.Errorf("drift did not fail ingress axis: outcome=%s status=%s", drift.Outcome, drift.Domain.IngressDNSStatus)
	}
	if drift.Domain.OwnershipStatus != domains.OwnershipVerified || drift.Domain.HTTPSStatus != domains.HTTPSPending || drift.Domain.RiskStatus != domains.RiskMissing {
		return fmt.Errorf("ingress drift collapsed independent axes: ownership=%s https=%s risk=%s", drift.Domain.OwnershipStatus, drift.Domain.HTTPSStatus, drift.Domain.RiskStatus)
	}
	entitlement, err := store.ResolveEntitlement(ctx, workspace, now.Add(5*time.Minute))
	if err != nil {
		return err
	}
	readiness := drift.Domain.Readiness(entitlement)
	if readiness.IngressDNSReady || readiness.ReadyForNewLinks || readiness.ReadyForRouting {
		return fmt.Errorf("drifted ingress remained ready: %+v", readiness)
	}

	authority.SetTarget(expectedTarget)
	recovered, err := verifier.Verify(ctx, domains.VerifyIngressDNSInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t011",
		CorrelationID: "corr-p06-t011-recovered",
		Reason: "revalidate restored ingress CNAME",
		Now: now.Add(6 * time.Minute),
	})
	if err != nil {
		return fmt.Errorf("recovered ingress verification: %w", err)
	}
	if recovered.Outcome != domains.IngressDNSValid || recovered.Domain.IngressDNSStatus != domains.IngressValid {
		return fmt.Errorf("restored ingress did not recover: outcome=%s status=%s", recovered.Outcome, recovered.Domain.IngressDNSStatus)
	}

	revalidationResults, err := queryStrings(ctx, db, `
		SELECT result
		FROM custom_domain_revalidations
		WHERE workspace_id = ? AND domain_id = ? AND axis = 'ingress_dns'
		ORDER BY id`, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	wantResults := []string{"fail", "fail", "pass", "fail", "pass"}
	if strings.Join(revalidationResults, ",") != strings.Join(wantResults, ",") {
		return fmt.Errorf("ingress revalidation results=%v want=%v", revalidationResults, wantResults)
	}
	outcomes, err := queryStrings(ctx, db, `
		SELECT JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.outcome'))
		FROM custom_domain_revalidations
		WHERE workspace_id = ? AND domain_id = ? AND axis = 'ingress_dns'
		ORDER BY id`, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	wantOutcomes := []string{"missing", "mismatch", "valid", "drift", "valid"}
	if strings.Join(outcomes, ",") != strings.Join(wantOutcomes, ",") {
		return fmt.Errorf("ingress revalidation outcomes=%v want=%v", outcomes, wantOutcomes)
	}

	auditMetadata, err := queryStrings(ctx, db, `
		SELECT CAST(metadata_json AS CHAR)
		FROM custom_domain_audit_events
		WHERE workspace_id = ? AND domain_id = ? AND action = 'domain.ingress.verify'
		ORDER BY id`, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	if len(auditMetadata) != 5 {
		return fmt.Errorf("ingress verify audit count=%d want=5", len(auditMetadata))
	}
	joinedAudit := strings.Join(auditMetadata, "\n")
	if strings.Contains(joinedAudit, expectedTarget) || strings.Contains(joinedAudit, wrongTarget) {
		return errors.New("ingress verification audit leaked server ingress target detail")
	}

	queryCount, queryNames, queryTypes := authority.QueryEvidence()
	if queryCount < 5 {
		return fmt.Errorf("authoritative DNS received only %d queries, want at least 5", queryCount)
	}
	expectedFQDN := strings.ToLower(created.Domain.HostnameASCII) + "."
	expectedQueries := 0
	for index, name := range queryNames {
		if name == expectedFQDN && queryTypes[index] == dnsmessage.TypeCNAME.String() {
			expectedQueries++
		}
		if strings.Contains(name, "internal.cloudapp.net") {
			return fmt.Errorf("absolute ingress query was redirected through host search suffix: %q", name)
		}
	}
	if expectedQueries < 5 {
		return fmt.Errorf("authoritative DNS received only %d exact CNAME queries for %s", expectedQueries, expectedFQDN)
	}

	out.Details = map[string]any{
		"domain_id": created.Domain.ID,
		"hostname_ascii": created.Domain.HostnameASCII,
		"dns_transport": "udp",
		"authoritative_dns_queries": queryCount,
		"exact_hostname_cname_queries": expectedQueries,
		"missing_outcome": missing.Outcome,
		"mismatch_outcome": mismatch.Outcome,
		"valid_outcome": valid.Outcome,
		"drift_outcome": drift.Outcome,
		"recovered_outcome": recovered.Outcome,
		"final_ingress_status": recovered.Domain.IngressDNSStatus,
		"ownership_status_after_drift": drift.Domain.OwnershipStatus,
		"https_status_after_drift": drift.Domain.HTTPSStatus,
		"risk_status_after_drift": drift.Domain.RiskStatus,
		"drift_ready_for_new_links": readiness.ReadyForNewLinks,
		"drift_ready_for_routing": readiness.ReadyForRouting,
		"revalidation_results": revalidationResults,
		"revalidation_outcomes": outcomes,
		"verify_audit_events": len(auditMetadata),
		"audit_target_detail_leak": false,
	}
	return nil
}

func startCNAMEAuthority(expectedName string) (*cnameAuthority, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		return nil, err
	}
	authority := &cnameAuthority{
		conn: conn,
		expected: strings.ToLower(strings.TrimSuffix(expectedName, ".")) + ".",
		missing: true,
		done: make(chan struct{}),
	}
	go authority.serve()
	return authority, nil
}

func (a *cnameAuthority) SetMissing() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.missing = true
	a.target = ""
}

func (a *cnameAuthority) SetTarget(target string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.missing = false
	a.target = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(target), ".")) + "."
}

func (a *cnameAuthority) Resolver() domains.CNAMEResolver {
	address := a.conn.LocalAddr().String()
	return domains.NetCNAMEResolver{Resolver: &net.Resolver{
		PreferGo: true,
		StrictErrors: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "udp4", address)
		},
	}}
}

func (a *cnameAuthority) QueryEvidence() (int, []string, []string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.queries, append([]string(nil), a.queryNames...), append([]string(nil), a.queryTypes...)
}

func (a *cnameAuthority) Close() {
	_ = a.conn.Close()
	<-a.done
}

func (a *cnameAuthority) serve() {
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

		a.mu.RLock()
		missing := a.missing
		target := a.target
		a.mu.RUnlock()
		a.mu.Lock()
		a.queries++
		a.queryNames = append(a.queryNames, qname)
		a.queryTypes = append(a.queryTypes, question.Type.String())
		a.mu.Unlock()

		response := dnsmessage.Message{
			Header: dnsmessage.Header{
				ID: query.Header.ID,
				Response: true,
				Authoritative: true,
				RecursionDesired: query.Header.RecursionDesired,
				RCode: dnsmessage.RCodeSuccess,
			},
			Questions: query.Questions,
		}
		if qname == a.expected && question.Type == dnsmessage.TypeCNAME {
			if missing {
				response.Header.RCode = dnsmessage.RCodeNameError
			} else {
				targetName, err := dnsmessage.NewName(target)
				if err != nil {
					continue
				}
				response.Answers = []dnsmessage.Resource{{
					Header: dnsmessage.ResourceHeader{Name: question.Name, Class: dnsmessage.ClassINET, TTL: 0},
					Body: &dnsmessage.CNAMEResource{CNAME: targetName},
				}}
			}
		}
		packed, err := response.Pack()
		if err != nil {
			continue
		}
		_, _ = a.conn.WriteToUDP(packed, remote)
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
