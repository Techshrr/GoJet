package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/Techshrr/GoJet/internal/links"
	"github.com/Techshrr/GoJet/internal/trust"
)

type output struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	Fixture      string          `json:"fixture"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

type deterministicProcessor struct {
	store        *trust.Store
	policy       trust.DestinationPolicy
	transientID  uint64
	alwaysFailID uint64
	calls        map[uint64]int
}

func (p *deterministicProcessor) Process(ctx context.Context, scan trust.DestinationScan, _ []trust.ScanTarget) error {
	p.calls[scan.ID]++
	if scan.ID == p.alwaysFailID {
		return errors.New("deterministic permanent worker failure")
	}
	if scan.ID == p.transientID && p.calls[scan.ID] == 1 {
		return errors.New("deterministic transient worker failure")
	}
	observation := trust.ProviderObservation{
		Provider:   "semantic-fixture",
		Outcome:    trust.ProviderAllow,
		SignalCode: "worker-allow",
		Evidence:   map[string]any{"fixture": "t007"},
		ObservedAt: time.Now().UTC(),
	}
	if _, err := p.store.RecordProviderObservation(ctx, trust.RecordProviderObservationInput{
		WorkspaceID:   scan.WorkspaceID,
		ScanID:        scan.ID,
		Observation:   observation,
		ActorID:       "p16-t007-processor",
		CorrelationID: scan.CorrelationID,
	}); err != nil {
		return err
	}
	_, err := p.store.FinalizeDestinationDecision(ctx, trust.FinalizeDestinationDecisionInput{
		WorkspaceID:       scan.WorkspaceID,
		ScanID:            scan.ID,
		Policy:            p.policy,
		LocalSafetyPassed: true,
		ActorID:           "p16-t007-processor",
		CorrelationID:     scan.CorrelationID,
	})
	return err
}

func main() {
	result, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		result.Case = "P16-T007"
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
		Case:         "P16-T007",
		Status:       "FAIL",
		Fixture:      "real MySQL lease/retry/recovery plus native SVC-OPS-MONITOR operationsmonitor executable",
		RecordCounts: map[string]int{},
		Checks:       map[string]bool{},
	}
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	if dsn == "" || redisAddr == "" {
		return out, fmt.Errorf("GOJET_MYSQL_DSN and GOJET_REDIS_ADDR are required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return out, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return out, err
	}
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&out.MySQLVersion); err != nil {
		return out, err
	}

	workspace := "p16-t007-workspace"
	linkID, fingerprint, err := createLink(ctx, db, workspace, "p16-t007")
	if err != nil {
		return out, err
	}
	store := trust.NewStore(db)
	policy := trust.DestinationPolicy{Version: "p16-worker-policy-v1", RequiredProviders: []string{"semantic-fixture"}, AllowTTL: 10 * time.Minute}

	transient, err := enqueue(ctx, store, workspace, linkID, fingerprint, policy.Version, "transient", 3)
	if err != nil {
		return out, err
	}
	processor := &deterministicProcessor{store: store, policy: policy, transientID: transient.Scan.ID, calls: map[uint64]int{}}
	worker, err := trust.NewRiskWorker(store, processor, "operationsmonitor-active")
	if err != nil {
		return out, err
	}
	worker.RetryBase = 0
	worker.LeaseTTL = 30 * time.Second

	worked1, firstErr := worker.RunOnce(ctx)
	afterFirst, err := store.GetDestinationScanState(ctx, workspace, transient.Scan.ID)
	if err != nil {
		return out, err
	}
	worked2, secondErr := worker.RunOnce(ctx)
	afterSecond, err := store.GetDestinationScanState(ctx, workspace, transient.Scan.ID)
	if err != nil {
		return out, err
	}

	recovery, err := enqueue(ctx, store, workspace, linkID, fingerprint, policy.Version, "recovery", 3)
	if err != nil {
		return out, err
	}
	leaseStarted := time.Now().UTC()
	deadLease, err := store.LeaseDestinationScan(ctx, "operationsmonitor-dead", leaseStarted, 30*time.Second)
	if err != nil || deadLease.Scan.ID != recovery.Scan.ID {
		return out, fmt.Errorf("establish abandoned lease: %w", err)
	}
	worker.Now = func() time.Time { return leaseStarted.Add(31 * time.Second) }
	worked3, recoveryErr := worker.RunOnce(ctx)
	worker.Now = time.Now
	afterRecovery, err := store.GetDestinationScanState(ctx, workspace, recovery.Scan.ID)
	if err != nil {
		return out, err
	}

	exhausted, err := enqueue(ctx, store, workspace, linkID, fingerprint, policy.Version, "exhausted", 1)
	if err != nil {
		return out, err
	}
	processor.alwaysFailID = exhausted.Scan.ID
	worked4, exhaustedErr := worker.RunOnce(ctx)
	afterExhausted, err := store.GetDestinationScanState(ctx, workspace, exhausted.Scan.ID)
	if err != nil {
		return out, err
	}

	worked5, idleErr := worker.RunOnce(ctx)
	if idleErr != nil {
		return out, idleErr
	}
	decisionCount, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_decisions WHERE workspace_id=?`, workspace)
	if err != nil {
		return out, err
	}
	transientDecisionCount, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_decisions WHERE scan_id=?`, transient.Scan.ID)
	if err != nil {
		return out, err
	}
	recoveryDecisionCount, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_decisions WHERE scan_id=?`, recovery.Scan.ID)
	if err != nil {
		return out, err
	}
	exhaustedDecisionCount, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_decisions WHERE scan_id=?`, exhausted.Scan.ID)
	if err != nil {
		return out, err
	}
	retryAudits, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_audit_events WHERE workspace_id=? AND action='destination-risk.scan-retry'`, workspace)
	if err != nil {
		return out, err
	}
	recoveryAudits, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_audit_events WHERE workspace_id=? AND action='destination-risk.scan-recover'`, workspace)
	if err != nil {
		return out, err
	}
	failedAudits, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_audit_events WHERE workspace_id=? AND action='destination-risk.scan-failed'`, workspace)
	if err != nil {
		return out, err
	}

	serviceExecuted, err := runOperationsMonitorOnce(ctx, dsn, redisAddr, policy.Version)
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{
		"durable_decisions":         decisionCount,
		"retry_audits":              retryAudits,
		"recovery_audits":           recoveryAudits,
		"failed_audits":             failedAudits,
		"transient_processor_calls": processor.calls[transient.Scan.ID],
		"recovery_processor_calls":  processor.calls[recovery.Scan.ID],
		"exhausted_processor_calls": processor.calls[exhausted.Scan.ID],
	}
	out.Checks = map[string]bool{
		"transient_failure_enters_retry":              worked1 && firstErr != nil && afterFirst.Status == trust.ScanStatusRetry && afterFirst.Attempts == 1,
		"retry_reuses_same_scan_and_completes":        worked2 && secondErr == nil && afterSecond.Status == trust.ScanStatusCompleted && afterSecond.Attempts == 2,
		"retry_produces_single_final_authority":       transientDecisionCount == 1 && processor.calls[transient.Scan.ID] == 2,
		"expired_lease_is_recovered_by_new_worker":    worked3 && recoveryErr == nil && afterRecovery.Status == trust.ScanStatusCompleted && afterRecovery.Attempts == 2 && recoveryAudits == 1,
		"recovery_produces_single_final_authority":    recoveryDecisionCount == 1 && processor.calls[recovery.Scan.ID] == 1,
		"max_attempt_exhaustion_fails_closed":         worked4 && exhaustedErr != nil && afterExhausted.Status == trust.ScanStatusFailed && afterExhausted.Attempts == 1 && exhaustedDecisionCount == 0,
		"idle_rerun_cannot_duplicate_final_authority": !worked5 && decisionCount == 2,
		"retry_and_failure_lifecycle_is_audited":      retryAudits == 1 && failedAudits == 1,
		"fixed_operationsmonitor_executable_runs":     serviceExecuted,
		"service_identity_is_fixed_ops_monitor":       trust.OperationsMonitorServiceID == "SVC-OPS-MONITOR",
	}
	if allTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func enqueue(ctx context.Context, store *trust.Store, workspace string, linkID uint64, fingerprint, policyVersion, suffix string, maxAttempts uint32) (trust.EnqueueDestinationScanResult, error) {
	key := "p16-t007-" + suffix
	return store.EnqueueDestinationScan(ctx, trust.EnqueueDestinationScanInput{
		WorkspaceID:     workspace,
		LinkID:          linkID,
		RiskFingerprint: fingerprint,
		PolicyVersion:   policyVersion,
		RequestKind:     trust.ScanRequestInitial,
		IdempotencyKey:  key,
		CorrelationID:   key,
		ActorID:         "p16-ci-worker",
		MaxAttempts:     maxAttempts,
	})
}

func createLink(ctx context.Context, db *sql.DB, workspace, suffix string) (uint64, string, error) {
	primary := "https://customer.example/" + suffix
	fingerprint, _, err := links.RiskFingerprint(primary, nil, nil)
	if err != nil {
		return 0, "", err
	}
	res, err := db.ExecContext(ctx, `
INSERT INTO links
(workspace_id,hostname,domain_kind,code,title,primary_destination,redirect_status,status,version,risk_fingerprint,routing_json,ab_json,utm_json,access_json)
VALUES (?,?,'official',?,?,?,302,'active',1,?,'[]','[]','{}','{}')`,
		workspace, "go.example.test", suffix, "P16 T007 fixture", primary, fingerprint)
	if err != nil {
		return 0, "", err
	}
	id, err := res.LastInsertId()
	return uint64(id), fingerprint, err
}

func runOperationsMonitorOnce(ctx context.Context, dsn, redisAddr, policyVersion string) (bool, error) {
	tmp := filepath.Join(os.TempDir(), "gojet-p16-operationsmonitor-t007")
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", tmp, "./services/platformapi/cmd/operationsmonitor")
	if raw, err := build.CombinedOutput(); err != nil {
		return false, fmt.Errorf("build operationsmonitor: %w: %s", err, strings.TrimSpace(string(raw)))
	}
	defer os.Remove(tmp)
	cmd := exec.CommandContext(ctx, tmp, "--once")
	cmd.Env = append(os.Environ(),
		"GOJET_MYSQL_DSN="+dsn,
		"GOJET_REDIS_ADDR="+redisAddr,
		"GOJET_RISK_PROVIDER_NAME=semantic-fixture",
		"GOJET_RISK_PROVIDER_ENDPOINT=https://example.com/fixture-not-called",
		"GOJET_RISK_POLICY_VERSION="+policyVersion,
		"GOJET_OPSMONITOR_WORKER_ID=p16-t007-native",
		"GOJET_RISK_PROJECTION_TTL=5s",
	)
	if raw, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("run operationsmonitor --once: %w: %s", err, strings.TrimSpace(string(raw)))
	}
	return true, nil
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func allTrue(checks map[string]bool) bool {
	for _, ok := range checks {
		if !ok {
			return false
		}
	}
	return len(checks) > 0
}
