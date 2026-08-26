package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

func main() {
	result, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		result.Case = "P16-T008"
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
		Case:         "P16-T008",
		Status:       "FAIL",
		Fixture:      "real MySQL durable decisions projected through inherited P05 RedisRiskStore into real Redis 7.x",
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return out, err
	}
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&out.MySQLVersion); err != nil {
		return out, err
	}

	redisClient := links.NewRedisClient(redisAddr, "", 0)
	defer redisClient.Close()
	if err := redisClient.FlushDB(ctx).Err(); err != nil {
		return out, err
	}
	runtime := links.NewRedisRiskStore(redisClient)
	if err := runtime.Ping(ctx); err != nil {
		return out, err
	}

	workspace := "p16-t008-workspace"
	linkID, fingerprint, primary, err := createLink(ctx, db, workspace, "p16-t008")
	if err != nil {
		return out, err
	}
	store := trust.NewStore(db)
	policy := trust.DestinationPolicy{Version: "p16-projection-policy-v1", RequiredProviders: []string{"semantic-fixture"}, AllowTTL: 10 * time.Minute}

	allowDecision, err := finalize(ctx, store, workspace, linkID, fingerprint, policy, "allow", trust.ProviderAllow, trust.ScanRequestInitial)
	if err != nil {
		return out, err
	}
	projectionNow := time.Now().UTC()
	allowProjection, err := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, workspace, linkID, policy.Version, projectionNow, 20*time.Second)
	if err != nil {
		return out, fmt.Errorf("project allow: %w", err)
	}
	allowRuntime, allowState, err := runtime.Resolve(ctx, linkID, fingerprint, time.Now().UTC())
	if err != nil {
		return out, err
	}
	allowRedisTTL, err := redisClient.TTL(ctx, links.RiskDecisionKey(linkID, fingerprint)).Result()
	if err != nil {
		return out, err
	}

	reviewDecision, err := finalize(ctx, store, workspace, linkID, fingerprint, policy, "review", trust.ProviderReview, trust.ScanRequestRescan)
	if err != nil {
		return out, err
	}
	reviewProjection, err := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, workspace, linkID, policy.Version, time.Now().UTC(), 20*time.Second)
	if err != nil {
		return out, fmt.Errorf("project review: %w", err)
	}
	reviewRuntime, reviewState, err := runtime.Resolve(ctx, linkID, fingerprint, time.Now().UTC())
	if err != nil {
		return out, err
	}

	_, policyMismatchErr := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, workspace, linkID, "p16-other-policy-v1", time.Now().UTC(), 20*time.Second)
	afterMismatch, afterMismatchState, err := runtime.Resolve(ctx, linkID, fingerprint, time.Now().UTC())
	if err != nil {
		return out, err
	}

	newPrimary := primary + "/changed"
	newFingerprint, _, err := links.RiskFingerprint(newPrimary, nil, nil)
	if err != nil {
		return out, err
	}
	if _, err := db.ExecContext(ctx, `UPDATE links SET primary_destination=?,risk_fingerprint=?,version=version+1 WHERE id=?`, newPrimary, newFingerprint, linkID); err != nil {
		return out, err
	}
	_, staleProjectionErr := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, workspace, linkID, policy.Version, time.Now().UTC(), 20*time.Second)
	_, currentState, err := runtime.Resolve(ctx, linkID, newFingerprint, time.Now().UTC())
	if err != nil {
		return out, err
	}
	oldRuntimeAfterMutation, oldStateAfterMutation, err := runtime.Resolve(ctx, linkID, fingerprint, time.Now().UTC())
	if err != nil {
		return out, err
	}

	candidates, err := store.ListProjectionCandidates(ctx, policy.Version, 100)
	if err != nil {
		return out, err
	}
	decisionCount, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_decisions WHERE workspace_id=?`, workspace)
	if err != nil {
		return out, err
	}
	redisKeys, err := redisClient.DBSize(ctx).Result()
	if err != nil {
		return out, err
	}

	allowTTLBounded := allowRedisTTL > 0 && allowRedisTTL <= 20*time.Second
	if allowDecision.ValidUntil != nil {
		allowTTLBounded = allowTTLBounded && !allowProjection.Runtime.ValidUntil.After(*allowDecision.ValidUntil)
	} else {
		allowTTLBounded = false
	}
	out.RecordCounts = map[string]int{
		"durable_decisions":            decisionCount,
		"redis_keys":                   int(redisKeys),
		"current_projection_candidates": len(candidates),
	}
	out.Checks = map[string]bool{
		"durable_current_allow_projects_exact_key":      allowState == links.RiskAllow && allowRuntime.Fingerprint == fingerprint && allowProjection.Decision.ID == allowDecision.ID,
		"allow_projection_policy_version_is_exact":      allowRuntime.PolicyVersion == policy.Version && allowProjection.Runtime.PolicyVersion == policy.Version,
		"allow_projection_ttl_is_bounded":               allowTTLBounded,
		"durable_review_overwrites_prior_runtime_allow": reviewDecision.ID != allowDecision.ID && reviewState == links.RiskReview && reviewRuntime.Decision == links.RiskReview && reviewProjection.Decision.ID == reviewDecision.ID,
		"policy_mismatch_cannot_replace_projection":      errors.Is(policyMismatchErr, trust.ErrNotFound) && afterMismatchState == links.RiskReview && afterMismatch.PolicyVersion == policy.Version,
		"target_mutation_invalidates_old_durable_authority": newFingerprint != fingerprint && errors.Is(staleProjectionErr, trust.ErrNotFound),
		"current_fingerprint_has_no_stale_projection":    currentState == links.RiskMissing,
		"old_fingerprint_projection_is_not_current_authority": oldStateAfterMutation == links.RiskReview && oldRuntimeAfterMutation.Fingerprint == fingerprint && newFingerprint != fingerprint,
		"projection_candidate_query_excludes_stale_fingerprint": len(candidates) == 0,
		"durable_authority_remains_mysql_source":         decisionCount == 2 && redisKeys == 1,
	}
	if allTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func finalize(ctx context.Context, store *trust.Store, workspace string, linkID uint64, fingerprint string, policy trust.DestinationPolicy, suffix string, outcome trust.ProviderOutcome, kind trust.ScanRequestKind) (trust.DestinationDecision, error) {
	key := "p16-t008-" + suffix
	enqueued, err := store.EnqueueDestinationScan(ctx, trust.EnqueueDestinationScanInput{
		WorkspaceID:     workspace,
		LinkID:          linkID,
		RiskFingerprint: fingerprint,
		PolicyVersion:   policy.Version,
		RequestKind:     kind,
		IdempotencyKey:  key,
		CorrelationID:   key,
		ActorID:         "p16-ci-projector",
		MaxAttempts:     3,
	})
	if err != nil {
		return trust.DestinationDecision{}, err
	}
	if _, err := store.RecordProviderObservation(ctx, trust.RecordProviderObservationInput{
		WorkspaceID: workspace,
		ScanID:      enqueued.Scan.ID,
		Observation: trust.ProviderObservation{
			Provider:   "semantic-fixture",
			Outcome:    outcome,
			SignalCode: "projection-" + suffix,
			Evidence:   map[string]any{"fixture": "t008"},
			ObservedAt: time.Now().UTC(),
		},
		ActorID:       "p16-ci-projector",
		CorrelationID: key,
	}); err != nil {
		return trust.DestinationDecision{}, err
	}
	return store.FinalizeDestinationDecision(ctx, trust.FinalizeDestinationDecisionInput{
		WorkspaceID:       workspace,
		ScanID:            enqueued.Scan.ID,
		Policy:            policy,
		LocalSafetyPassed: true,
		ActorID:           "p16-ci-projector",
		CorrelationID:     key,
	})
}

func createLink(ctx context.Context, db *sql.DB, workspace, suffix string) (uint64, string, string, error) {
	primary := "https://customer.example/" + suffix
	fingerprint, _, err := links.RiskFingerprint(primary, nil, nil)
	if err != nil {
		return 0, "", "", err
	}
	res, err := db.ExecContext(ctx, `
INSERT INTO links
(workspace_id,hostname,domain_kind,code,title,primary_destination,redirect_status,status,version,risk_fingerprint,routing_json,ab_json,utm_json,access_json)
VALUES (?,?,'official',?,?,?,302,'active',1,?,'[]','[]','{}','{}')`,
		workspace, "go.example.test", suffix, "P16 T008 fixture", primary, fingerprint)
	if err != nil {
		return 0, "", "", err
	}
	id, err := res.LastInsertId()
	return uint64(id), fingerprint, primary, err
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
