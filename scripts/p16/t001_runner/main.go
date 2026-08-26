package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/Techshrr/GoJet/internal/links"
	"github.com/Techshrr/GoJet/internal/trust"
)

type output struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

func main() {
	result, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		result.Case = "P16-T001"
		result.Status = "FAIL"
		if result.Checks == nil {
			result.Checks = map[string]bool{"runner_completed": false}
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
	if err != nil || result.Status != "PASS" {
		os.Exit(1)
	}
}

func run() (output, error) {
	out := output{
		Case:         "P16-T001",
		Status:       "FAIL",
		RecordCounts: map[string]int{},
		Checks:       map[string]bool{},
	}
	dsn := os.Getenv("GOJET_MYSQL_DSN")
	if dsn == "" {
		return out, fmt.Errorf("GOJET_MYSQL_DSN is required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return out, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return out, err
	}
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&out.MySQLVersion); err != nil {
		return out, err
	}

	workspace := "p16-t001-workspace"
	correlation := "p16-t001-correlation"
	policy := "p16-policy-v1"
	linkID, fingerprint, expectedTargets, err := createLink(ctx, db, workspace, "p16-t001")
	if err != nil {
		return out, err
	}

	store := trust.NewStore(db)
	enqueued, err := store.EnqueueDestinationScan(ctx, trust.EnqueueDestinationScanInput{
		WorkspaceID:     workspace,
		LinkID:          linkID,
		RiskFingerprint: fingerprint,
		PolicyVersion:   policy,
		RequestKind:     trust.ScanRequestInitial,
		IdempotencyKey:  "p16-t001-initial",
		CorrelationID:   correlation,
		ActorID:         "p16-ci-security",
		MaxAttempts:     5,
	})
	if err != nil {
		return out, err
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	safeProviderEvidence := `{"category":"fixture","detail":"redacted"}`
	if _, err := db.ExecContext(ctx, `
INSERT INTO destination_risk_provider_observations
(scan_id,provider,outcome,signal_code,evidence_json,observed_at,created_at)
VALUES (?,?,'unknown',?,?,?,?)`,
		enqueued.Scan.ID, "semantic-fixture", "fixture-redacted", safeProviderEvidence, now, now); err != nil {
		return out, err
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO destination_risk_decisions
(workspace_id,link_id,scan_id,risk_fingerprint,policy_version,state,reason_category,decision_metadata_json,valid_until,decided_at,created_at)
VALUES (?,?,?,?,?,'pending','awaiting-provider','{}',NULL,?,?)`,
		workspace, linkID, enqueued.Scan.ID, fingerprint, policy, now, now); err != nil {
		return out, err
	}

	persistedScan, persistedTargets, err := store.GetDestinationScan(ctx, workspace, enqueued.Scan.ID)
	if err != nil {
		return out, err
	}

	tableCount, err := scalarInt(ctx, db, `
SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema=DATABASE() AND table_name IN (
'destination_risk_scans','destination_risk_scan_targets','destination_risk_provider_observations','destination_risk_decisions','destination_risk_audit_events')`)
	if err != nil {
		return out, err
	}
	scanCount, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM destination_risk_scans WHERE workspace_id=? AND correlation_id=?", workspace, correlation)
	if err != nil {
		return out, err
	}
	targetCount, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM destination_risk_scan_targets WHERE scan_id=?", enqueued.Scan.ID)
	if err != nil {
		return out, err
	}
	observationCount, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM destination_risk_provider_observations WHERE scan_id=?", enqueued.Scan.ID)
	if err != nil {
		return out, err
	}
	decisionCount, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM destination_risk_decisions WHERE scan_id=? AND risk_fingerprint=? AND policy_version=?", enqueued.Scan.ID, fingerprint, policy)
	if err != nil {
		return out, err
	}
	auditCount, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM destination_risk_audit_events WHERE scan_id=? AND correlation_id=? AND action='destination-risk.scan-enqueue' AND result='success'", enqueued.Scan.ID, correlation)
	if err != nil {
		return out, err
	}
	correlatedDecisionCount, err := scalarInt(ctx, db, `
SELECT COUNT(*) FROM destination_risk_decisions d
JOIN destination_risk_scans s ON s.id=d.scan_id
WHERE d.scan_id=? AND s.correlation_id=? AND d.risk_fingerprint=s.risk_fingerprint AND d.policy_version=s.policy_version`, enqueued.Scan.ID, correlation)
	if err != nil {
		return out, err
	}
	secretCount, err := scalarInt(ctx, db, `
SELECT COUNT(*) FROM destination_risk_provider_observations
WHERE scan_id=? AND LOWER(CAST(evidence_json AS CHAR)) LIKE '%p16-provider-secret-fixture%'`, enqueued.Scan.ID)
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{
		"schema_tables":         tableCount,
		"scans":                 scanCount,
		"scan_targets":          targetCount,
		"provider_observations": observationCount,
		"policy_decisions":      decisionCount,
		"audit_events":          auditCount,
	}
	out.Checks = map[string]bool{
		"five_durable_tables_present":        tableCount == 5,
		"scan_created_and_correlated":        enqueued.Created && scanCount == 1 && persistedScan.CorrelationID == correlation,
		"exact_fingerprint_is_durable":       persistedScan.RiskFingerprint == fingerprint && persistedScan.PolicyVersion == policy,
		"reachable_targets_are_durable":      targetCount == len(expectedTargets) && len(persistedTargets) == len(expectedTargets),
		"provider_observation_is_durable":    observationCount == 1,
		"policy_decision_is_durable":         decisionCount == 1,
		"decision_inherits_scan_correlation": correlatedDecisionCount == 1,
		"enqueue_audit_is_correlated":        auditCount == 1,
		"raw_provider_secret_not_persisted":  secretCount == 0,
	}
	if allTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func createLink(ctx context.Context, db *sql.DB, workspace, suffix string) (uint64, string, []string, error) {
	primary := "https://primary.example/" + suffix
	routing := []links.RoutingRule{{ID: "us", MatchType: "country", MatchValue: "US", Destination: "https://route.example/" + suffix, Enabled: true}}
	variants := []links.ABVariant{
		{ID: "a", Destination: "https://a.example/" + suffix, Weight: 50, Enabled: true},
		{ID: "b", Destination: "https://b.example/" + suffix, Weight: 50, Enabled: true},
	}
	fingerprint, targets, err := links.RiskFingerprint(primary, routing, variants)
	if err != nil {
		return 0, "", nil, err
	}
	routingJSON, _ := json.Marshal(routing)
	abJSON, _ := json.Marshal(variants)
	res, err := db.ExecContext(ctx, `
INSERT INTO links
(workspace_id,hostname,domain_kind,code,title,primary_destination,redirect_status,status,version,risk_fingerprint,routing_json,ab_json,utm_json,access_json)
VALUES (?,?,'official',?, ?, ?,302,'active',1,?,?,?,'{}','{}')`,
		workspace, "go.example.test", suffix, "P16 fixture", primary, fingerprint, string(routingJSON), string(abJSON))
	if err != nil {
		return 0, "", nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, "", nil, err
	}
	return uint64(id), fingerprint, targets, nil
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, query, args...).Scan(&n)
	return n, err
}

func allTrue(checks map[string]bool) bool {
	for _, ok := range checks {
		if !ok {
			return false
		}
	}
	return len(checks) > 0
}
