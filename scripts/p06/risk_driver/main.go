package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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

type deterministicRiskEvaluator struct {
	mu          sync.RWMutex
	observation domains.DomainRiskObservation
	calls       int
	hostnames   []string
}

func (e *deterministicRiskEvaluator) Set(observation domains.DomainRiskObservation) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.observation = observation
}

func (e *deterministicRiskEvaluator) Evaluate(_ context.Context, hostnameASCII string) (domains.DomainRiskObservation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	e.hostnames = append(e.hostnames, hostnameASCII)
	return e.observation, nil
}

func (e *deterministicRiskEvaluator) Evidence() (int, []string) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.calls, append([]string(nil), e.hostnames...)
}

func main() {
	caseFlag := flag.String("case", "P06-T013", "P06 domain-risk case ID")
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
	if *caseFlag != "P06-T013" {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, fmt.Sprintf("unsupported case %s", *caseFlag))
	} else if err := caseT013(ctx, db, &result); err != nil {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, err.Error())
	}
	writeJSON(map[string]caseResult{*caseFlag: result})
	if result.Status != "PASS" {
		os.Exit(1)
	}
}

func caseT013(ctx context.Context, db *sql.DB, out *caseResult) error {
	workspace := "p06-t013-risk"
	store := domains.NewMySQLStore(db)
	now := fixedNow()
	if _, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace,
		SourceKey: "subscription-business-t013",
		Status: domains.EntitlementActive,
		DomainLimit: 1,
		StartsAt: now.Add(-24 * time.Hour),
		DecisionReason: "T013 active entitlement fixture",
	}, "corr-p06-t013-plan"); err != nil {
		return err
	}
	created, err := store.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspace,
		ActorID: "actor-t013",
		CorrelationID: "corr-p06-t013-create",
		Reason: "create domain before domain-risk verification",
		Hostname: "risk-t013.example.com",
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
		    https_status = 'active', https_checked_at = ?
		WHERE workspace_id = ? AND id = ?`, fixtureAt, fixtureAt, fixtureAt, workspace, created.Domain.ID); err != nil {
		return fmt.Errorf("seed T013 ready prerequisites: %w", err)
	}

	evaluator := &deterministicRiskEvaluator{}
	verifier := domains.NewDomainRiskVerifier(store, evaluator)
	entitlement, err := store.ResolveEntitlement(ctx, workspace, now.Add(2*time.Minute))
	if err != nil {
		return err
	}

	statuses := []domains.DomainRiskStatus{
		domains.RiskMissing,
		domains.RiskReview,
		domains.RiskBlock,
		domains.RiskMalformed,
		domains.RiskStale,
		domains.RiskAllow,
		domains.RiskBlock,
		domains.RiskAllow,
	}
	results := make([]string, 0, len(statuses))
	readyStates := make([]bool, 0, len(statuses))
	evidenceRefs := make([]string, 0, len(statuses))
	var final domains.DomainRiskVerificationResult
	for index, status := range statuses {
		evidenceRef := fmt.Sprintf("risk:t013:%02d:%s", index+1, status)
		evidenceRefs = append(evidenceRefs, evidenceRef)
		evaluator.Set(domains.DomainRiskObservation{
			Status: status,
			PolicyVersion: "domain-risk-t013-v1",
			EvidenceRef: evidenceRef,
		})
		verified, err := verifier.Verify(ctx, domains.VerifyDomainRiskInput{
			WorkspaceID: workspace,
			DomainID: created.Domain.ID,
			ActorID: "actor-t013",
			CorrelationID: fmt.Sprintf("corr-p06-t013-%02d", index+1),
			Reason: "evaluate current domain risk authority",
			Now: now.Add(time.Duration(index+2) * time.Minute),
		})
		if err != nil {
			return fmt.Errorf("risk state %s verification: %w", status, err)
		}
		if verified.Domain.RiskStatus != status || verified.Observation.Status != status {
			return fmt.Errorf("risk state mismatch observation=%s domain=%s want=%s", verified.Observation.Status, verified.Domain.RiskStatus, status)
		}
		if verified.Domain.OwnershipStatus != domains.OwnershipVerified || verified.Domain.IngressDNSStatus != domains.IngressValid || verified.Domain.HTTPSStatus != domains.HTTPSActive {
			return fmt.Errorf("risk state %s collapsed another axis: ownership=%s ingress=%s https=%s", status, verified.Domain.OwnershipStatus, verified.Domain.IngressDNSStatus, verified.Domain.HTTPSStatus)
		}
		readiness := verified.Domain.Readiness(entitlement)
		shouldReady := status == domains.RiskAllow
		if readiness.RiskReady != shouldReady || readiness.ReadyForNewLinks != shouldReady || readiness.ReadyForRouting != shouldReady {
			return fmt.Errorf("risk state %s readiness=%+v want ready=%v", status, readiness, shouldReady)
		}
		serialized, err := json.Marshal(verified.Domain)
		if err != nil {
			return err
		}
		if strings.Contains(string(serialized), evidenceRef) || strings.Contains(string(serialized), "risk_evidence_ref") {
			return errors.New("base Domain JSON leaked internal risk evidence reference")
		}
		results = append(results, string(status))
		readyStates = append(readyStates, shouldReady)
		final = verified
	}

	revalidationResults, err := queryStrings(ctx, db, `
		SELECT result FROM custom_domain_revalidations
		WHERE workspace_id = ? AND domain_id = ? AND axis = 'risk'
		ORDER BY id`, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	wantRevalidations := []string{"pending", "fail", "fail", "fail", "stale", "pass", "fail", "pass"}
	if strings.Join(revalidationResults, ",") != strings.Join(wantRevalidations, ",") {
		return fmt.Errorf("risk revalidation sequence=%v want=%v", revalidationResults, wantRevalidations)
	}
	persistedRefs, err := queryStrings(ctx, db, `
		SELECT evidence_ref FROM custom_domain_revalidations
		WHERE workspace_id = ? AND domain_id = ? AND axis = 'risk'
		ORDER BY id`, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	if strings.Join(persistedRefs, ",") != strings.Join(evidenceRefs, ",") {
		return fmt.Errorf("risk evidence refs=%v want=%v", persistedRefs, evidenceRefs)
	}

	auditMetadata, err := queryStrings(ctx, db, `
		SELECT CAST(metadata_json AS CHAR) FROM custom_domain_audit_events
		WHERE workspace_id = ? AND domain_id = ? AND action = 'domain.risk.verify'
		ORDER BY id`, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	if len(auditMetadata) != len(statuses) {
		return fmt.Errorf("risk verify audit count=%d want=%d", len(auditMetadata), len(statuses))
	}
	joinedAudit := strings.Join(auditMetadata, "\n")
	for _, ref := range evidenceRefs {
		if strings.Contains(joinedAudit, ref) {
			return errors.New("risk audit leaked internal provider evidence reference")
		}
	}

	calls, hostnames := evaluator.Evidence()
	if calls != len(statuses) {
		return fmt.Errorf("server risk evaluator calls=%d want=%d", calls, len(statuses))
	}
	for _, hostname := range hostnames {
		if hostname != created.Domain.HostnameASCII {
			return fmt.Errorf("risk evaluator received unexpected hostname %q", hostname)
		}
	}
	if final.Domain.RiskStatus != domains.RiskAllow {
		return fmt.Errorf("final risk state=%s want allow", final.Domain.RiskStatus)
	}

	out.Details = map[string]any{
		"domain_id": created.Domain.ID,
		"hostname_ascii": created.Domain.HostnameASCII,
		"risk_sequence": results,
		"ready_sequence": readyStates,
		"revalidation_results": revalidationResults,
		"risk_evaluator_calls": calls,
		"final_risk_status": final.Domain.RiskStatus,
		"ownership_status": final.Domain.OwnershipStatus,
		"ingress_status": final.Domain.IngressDNSStatus,
		"https_status": final.Domain.HTTPSStatus,
		"provider_evidence_persisted_internal": true,
		"domain_json_evidence_leak": false,
		"audit_evidence_leak": false,
	}
	return nil
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
