package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

type linkFixture struct {
	ID          uint64
	WorkspaceID string
	Primary     string
	Routing     []links.RoutingRule
	Variants    []links.ABVariant
	Fingerprint string
	Targets     []string
}

func main() {
	result, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		result.Case = "P16-T002"
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
		Case:         "P16-T002",
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

	fixture, err := createLink(ctx, db)
	if err != nil {
		return out, err
	}
	store := trust.NewStore(db)
	policy := "p16-policy-v1"
	actor := "p16-ci-security"

	initialInput := trust.EnqueueDestinationScanInput{
		WorkspaceID:     fixture.WorkspaceID,
		LinkID:          fixture.ID,
		RiskFingerprint: fixture.Fingerprint,
		PolicyVersion:   policy,
		RequestKind:     trust.ScanRequestInitial,
		IdempotencyKey:  "p16-t002-k1",
		CorrelationID:   "p16-t002-initial",
		ActorID:         actor,
		MaxAttempts:     5,
	}
	initial, err := store.EnqueueDestinationScan(ctx, initialInput)
	if err != nil {
		return out, fmt.Errorf("initial enqueue: %w", err)
	}
	replay, err := store.EnqueueDestinationScan(ctx, initialInput)
	if err != nil {
		return out, fmt.Errorf("idempotent replay: %w", err)
	}

	rescanInput := initialInput
	rescanInput.RequestKind = trust.ScanRequestRescan
	rescanInput.IdempotencyKey = "p16-t002-k2"
	rescanInput.CorrelationID = "p16-t002-rescan"
	rescan, err := store.EnqueueDestinationScan(ctx, rescanInput)
	if err != nil {
		return out, fmt.Errorf("same-fingerprint rescan: %w", err)
	}

	policyConflict := initialInput
	policyConflict.PolicyVersion = "p16-policy-v2"
	_, policyConflictErr := store.EnqueueDestinationScan(ctx, policyConflict)

	kindConflict := initialInput
	kindConflict.RequestKind = trust.ScanRequestRescan
	_, kindConflictErr := store.EnqueueDestinationScan(ctx, kindConflict)

	oldFingerprint := fixture.Fingerprint
	oldTargets := append([]string(nil), fixture.Targets...)
	newFingerprint, newTargets, err := mutateReachableTarget(ctx, db, fixture)
	if err != nil {
		return out, err
	}

	staleInput := initialInput
	staleInput.IdempotencyKey = "p16-t002-k3"
	staleInput.CorrelationID = "p16-t002-stale"
	_, staleErr := store.EnqueueDestinationScan(ctx, staleInput)

	currentInput := initialInput
	currentInput.RiskFingerprint = newFingerprint
	currentInput.IdempotencyKey = "p16-t002-k4"
	currentInput.CorrelationID = "p16-t002-current"
	current, err := store.EnqueueDestinationScan(ctx, currentInput)
	if err != nil {
		return out, fmt.Errorf("new-current fingerprint enqueue: %w", err)
	}

	crossAuthority := initialInput
	crossAuthority.RiskFingerprint = newFingerprint
	crossAuthority.CorrelationID = "p16-t002-cross-authority"
	_, crossAuthorityErr := store.EnqueueDestinationScan(ctx, crossAuthority)

	scanCount, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM destination_risk_scans WHERE workspace_id=?", fixture.WorkspaceID)
	if err != nil {
		return out, err
	}
	auditCount, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM destination_risk_audit_events WHERE workspace_id=? AND action='destination-risk.scan-enqueue' AND result='success'", fixture.WorkspaceID)
	if err != nil {
		return out, err
	}
	oldAuthorityCount, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM destination_risk_scans WHERE workspace_id=? AND risk_fingerprint=?", fixture.WorkspaceID, oldFingerprint)
	if err != nil {
		return out, err
	}
	newAuthorityCount, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM destination_risk_scans WHERE workspace_id=? AND risk_fingerprint=?", fixture.WorkspaceID, newFingerprint)
	if err != nil {
		return out, err
	}
	currentPersisted, currentTargets, err := store.GetDestinationScan(ctx, fixture.WorkspaceID, current.Scan.ID)
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{
		"scans":                 scanCount,
		"successful_enqueue_audits": auditCount,
		"old_fingerprint_scans": oldAuthorityCount,
		"new_fingerprint_scans": newAuthorityCount,
		"old_target_count":      len(oldTargets),
		"new_target_count":      len(newTargets),
	}
	out.Checks = map[string]bool{
		"initial_enqueue_created":                   initial.Created && initial.Scan.ID > 0,
		"duplicate_request_reuses_same_scan":        !replay.Created && replay.Scan.ID == initial.Scan.ID,
		"duplicate_request_reuses_exact_targets":    sameTargets(replay.Targets, initial.Targets),
		"same_fingerprint_rescan_creates_new_scan":  rescan.Created && rescan.Scan.ID != initial.Scan.ID && rescan.Scan.RiskFingerprint == oldFingerprint,
		"idempotency_key_rejects_policy_reuse":      errors.Is(policyConflictErr, trust.ErrConflict),
		"idempotency_key_rejects_request_kind_reuse": errors.Is(kindConflictErr, trust.ErrConflict),
		"reachable_target_mutation_changes_fingerprint": newFingerprint != oldFingerprint && !sameStrings(newTargets, oldTargets),
		"old_fingerprint_fails_closed_after_mutation": errors.Is(staleErr, trust.ErrStaleFingerprint),
		"new_current_fingerprint_enqueues":          current.Created && current.Scan.RiskFingerprint == newFingerprint,
		"old_idempotency_key_cannot_cross_fingerprint": errors.Is(crossAuthorityErr, trust.ErrConflict),
		"failed_or_conflicting_requests_create_no_scan": scanCount == 3,
		"successful_new_scans_are_audited":         auditCount == 3,
		"old_and_new_authority_remain_distinct":     oldAuthorityCount == 2 && newAuthorityCount == 1,
		"current_scan_persists_exact_current_targets": currentPersisted.RiskFingerprint == newFingerprint && targetURLs(currentTargets, newTargets),
	}
	if allTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func createLink(ctx context.Context, db *sql.DB) (linkFixture, error) {
	fixture := linkFixture{
		WorkspaceID: "p16-t002-workspace",
		Primary:     "https://primary.example/t002",
		Routing: []links.RoutingRule{{
			ID: "us", MatchType: "country", MatchValue: "US", Destination: "https://route.example/t002", Enabled: true,
		}},
		Variants: []links.ABVariant{
			{ID: "a", Destination: "https://a.example/t002", Weight: 50, Enabled: true},
			{ID: "b", Destination: "https://b.example/t002", Weight: 50, Enabled: true},
		},
	}
	fingerprint, targets, err := links.RiskFingerprint(fixture.Primary, fixture.Routing, fixture.Variants)
	if err != nil {
		return fixture, err
	}
	fixture.Fingerprint = fingerprint
	fixture.Targets = targets
	routingJSON, err := json.Marshal(fixture.Routing)
	if err != nil {
		return fixture, err
	}
	abJSON, err := json.Marshal(fixture.Variants)
	if err != nil {
		return fixture, err
	}
	res, err := db.ExecContext(ctx, `
INSERT INTO links
(workspace_id,hostname,domain_kind,code,title,primary_destination,redirect_status,status,version,risk_fingerprint,routing_json,ab_json,utm_json,access_json)
VALUES (?,?,'official',?,?,?,302,'active',1,?,?,?,'{}','{}')`,
		fixture.WorkspaceID, "go.example.test", "p16-t002", "P16 T002 fixture", fixture.Primary, fixture.Fingerprint, string(routingJSON), string(abJSON))
	if err != nil {
		return fixture, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fixture, err
	}
	fixture.ID = uint64(id)
	return fixture, nil
}

func mutateReachableTarget(ctx context.Context, db *sql.DB, fixture linkFixture) (string, []string, error) {
	newPrimary := "https://primary.example/t002-mutated"
	newFingerprint, newTargets, err := links.RiskFingerprint(newPrimary, fixture.Routing, fixture.Variants)
	if err != nil {
		return "", nil, err
	}
	res, err := db.ExecContext(ctx, `
UPDATE links
SET primary_destination=?, risk_fingerprint=?, version=version+1, updated_at=CURRENT_TIMESTAMP(6)
WHERE id=? AND workspace_id=?`, newPrimary, newFingerprint, fixture.ID, fixture.WorkspaceID)
	if err != nil {
		return "", nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", nil, err
	}
	if n != 1 {
		return "", nil, fmt.Errorf("link mutation affected %d rows", n)
	}
	return newFingerprint, newTargets, nil
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, query, args...).Scan(&n)
	return n, err
}

func sameTargets(a, b []trust.ScanTarget) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Order != b[i].Order || a[i].NormalizedURL != b[i].NormalizedURL || a[i].TargetHash != b[i].TargetHash {
			return false
		}
	}
	return true
}

func targetURLs(actual []trust.ScanTarget, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for i := range expected {
		if actual[i].Order != uint32(i+1) || actual[i].NormalizedURL != expected[i] {
			return false
		}
	}
	return true
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func allTrue(checks map[string]bool) bool {
	for _, ok := range checks {
		if !ok {
			return false
		}
	}
	return len(checks) > 0
}
