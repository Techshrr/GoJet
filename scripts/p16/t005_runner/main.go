package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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

type publicResolver struct{}

func (publicResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
}

type scenarioResult struct {
	Observation trust.ProviderObservation
	Decision    trust.DestinationDecision
	Scan        trust.DestinationScan
}

func main() {
	result, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		result.Case = "P16-T005"
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
		Case:         "P16-T005",
		Status:       "FAIL",
		Fixture:      "local deterministic HTTP semantic provider reached only through guarded P16 transport",
		RecordCounts: map[string]int{},
		Checks:       map[string]bool{},
	}
	dsn := os.Getenv("GOJET_MYSQL_DSN")
	if strings.TrimSpace(dsn) == "" {
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		verdict := strings.TrimPrefix(r.URL.Path, "/")
		switch verdict {
		case "allow", "review", "block":
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"complete":    true,
			"verdict":     verdict,
			"signal_code": "semantic-" + verdict,
			"evidence": map[string]any{
				"category":      "deterministic-fixture",
				"confidence":    0.99,
				"client_secret": "p16-provider-secret-fixture",
				"nested": map[string]any{
					"api_token": "p16-provider-token-fixture",
					"safe":      "retained",
				},
			},
		})
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		return out, err
	}
	dialer := trust.DialContextFunc(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, server.Listener.Addr().String())
	})
	httpClient := trust.NewInspectionHTTPClient(publicResolver{}, dialer)

	workspace := "p16-t005-workspace"
	linkID, fingerprint, primary, err := createLink(ctx, db, workspace, "p16-t005")
	if err != nil {
		return out, err
	}
	store := trust.NewStore(db)
	policy := trust.DestinationPolicy{
		Version:           "p16-semantic-policy-v1",
		RequiredProviders: []string{"semantic-fixture"},
		AllowTTL:          15 * time.Minute,
	}

	allowWithoutLocal, err := executeScenario(ctx, store, workspace, linkID, fingerprint, primary, policy, httpClient, port, "allow", false, "s1")
	if err != nil {
		return out, fmt.Errorf("allow without local safety: %w", err)
	}
	allowWithLocal, err := executeScenario(ctx, store, workspace, linkID, fingerprint, primary, policy, httpClient, port, "allow", true, "s2")
	if err != nil {
		return out, fmt.Errorf("allow with local safety: %w", err)
	}
	review, err := executeScenario(ctx, store, workspace, linkID, fingerprint, primary, policy, httpClient, port, "review", true, "s3")
	if err != nil {
		return out, fmt.Errorf("review scenario: %w", err)
	}
	block, err := executeScenario(ctx, store, workspace, linkID, fingerprint, primary, policy, httpClient, port, "block", true, "s4")
	if err != nil {
		return out, fmt.Errorf("block scenario: %w", err)
	}

	pendingEvaluation, err := trust.EvaluateDestinationPolicy(policy, nil, true, time.Now())
	if err != nil {
		return out, err
	}

	observationCount, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_provider_observations o JOIN destination_risk_scans s ON s.id=o.scan_id WHERE s.workspace_id=?`, workspace)
	if err != nil {
		return out, err
	}
	decisionCount, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_decisions WHERE workspace_id=?`, workspace)
	if err != nil {
		return out, err
	}
	completedCount, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_scans WHERE workspace_id=? AND status='completed'`, workspace)
	if err != nil {
		return out, err
	}
	auditCount, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_audit_events WHERE workspace_id=?`, workspace)
	if err != nil {
		return out, err
	}
	secretCount, err := scalarInt(ctx, db, `
SELECT COUNT(*) FROM destination_risk_provider_observations o
JOIN destination_risk_scans s ON s.id=o.scan_id
WHERE s.workspace_id=? AND (
  LOWER(CAST(o.evidence_json AS CHAR)) LIKE '%p16-provider-secret-fixture%' OR
  LOWER(CAST(o.evidence_json AS CHAR)) LIKE '%p16-provider-token-fixture%' OR
  LOWER(CAST(o.evidence_json AS CHAR)) LIKE '%client_secret%' OR
  LOWER(CAST(o.evidence_json AS CHAR)) LIKE '%api_token%'
)`, workspace)
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{
		"provider_observations": observationCount,
		"policy_decisions":      decisionCount,
		"completed_scans":       completedCount,
		"audit_events":          auditCount,
		"secret_matches":        secretCount,
	}
	out.Checks = map[string]bool{
		"provider_allow_without_local_safety_is_not_allow": allowWithoutLocal.Observation.Outcome == trust.ProviderAllow && allowWithoutLocal.Decision.State == trust.DecisionReview && allowWithoutLocal.Decision.ReasonCategory == "local-safety-not-approved",
		"provider_allow_plus_local_policy_can_allow":       allowWithLocal.Observation.Outcome == trust.ProviderAllow && allowWithLocal.Decision.State == trust.DecisionAllow && allowWithLocal.Decision.ValidUntil != nil,
		"provider_review_maps_to_review":                    review.Observation.Outcome == trust.ProviderReview && review.Decision.State == trust.DecisionReview && review.Decision.ReasonCategory == "provider-review",
		"provider_block_maps_to_block":                      block.Observation.Outcome == trust.ProviderBlock && block.Decision.State == trust.DecisionBlock && block.Decision.ReasonCategory == "provider-block",
		"missing_required_provider_remains_pending":         pendingEvaluation.State == trust.DecisionPending && pendingEvaluation.ReasonCategory == "provider-pending",
		"decisions_are_exact_fingerprint_policy_bound":      allAuthorityBound([]scenarioResult{allowWithoutLocal, allowWithLocal, review, block}, fingerprint, policy.Version),
		"all_finalized_scans_are_completed":                 completedCount == 4 && allCompleted([]scenarioResult{allowWithoutLocal, allowWithLocal, review, block}),
		"provider_and_decision_records_are_durable":         observationCount == 4 && decisionCount == 4,
		"provider_evidence_secrets_are_redacted":            secretCount == 0 && safeEvidence(allowWithLocal.Observation.Evidence),
		"correlated_audit_covers_enqueue_observe_decide":    auditCount == 12,
	}
	if allTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func executeScenario(ctx context.Context, store *trust.Store, workspace string, linkID uint64, fingerprint, target string, policy trust.DestinationPolicy, httpClient *http.Client, port, verdict string, localSafety bool, suffix string) (scenarioResult, error) {
	correlation := "p16-t005-" + suffix
	enqueued, err := store.EnqueueDestinationScan(ctx, trust.EnqueueDestinationScanInput{
		WorkspaceID:     workspace,
		LinkID:          linkID,
		RiskFingerprint: fingerprint,
		PolicyVersion:   policy.Version,
		RequestKind:     trust.ScanRequestInitial,
		IdempotencyKey:  correlation,
		CorrelationID:   correlation,
		ActorID:         "p16-ci-security",
		MaxAttempts:     5,
	})
	if err != nil {
		return scenarioResult{}, err
	}
	client := trust.SemanticProviderClient{
		Name:       "semantic-fixture",
		Endpoint:   "http://provider.test:" + port + "/" + verdict,
		HTTPClient: httpClient,
	}
	observation, err := client.Observe(ctx, target)
	if err != nil {
		return scenarioResult{}, err
	}
	persistedObservation, err := store.RecordProviderObservation(ctx, trust.RecordProviderObservationInput{
		WorkspaceID:   workspace,
		ScanID:        enqueued.Scan.ID,
		Observation:   observation,
		ActorID:       "p16-ci-security",
		CorrelationID: correlation,
	})
	if err != nil {
		return scenarioResult{}, err
	}
	decision, err := store.FinalizeDestinationDecision(ctx, trust.FinalizeDestinationDecisionInput{
		WorkspaceID:       workspace,
		ScanID:            enqueued.Scan.ID,
		Policy:            policy,
		LocalSafetyPassed: localSafety,
		ActorID:           "p16-ci-security",
		CorrelationID:     correlation,
	})
	if err != nil {
		return scenarioResult{}, err
	}
	persistedDecision, err := store.GetDestinationDecision(ctx, workspace, enqueued.Scan.ID)
	if err != nil {
		return scenarioResult{}, err
	}
	scan, _, err := store.GetDestinationScan(ctx, workspace, enqueued.Scan.ID)
	if err != nil {
		return scenarioResult{}, err
	}
	if persistedDecision.ID != decision.ID {
		return scenarioResult{}, fmt.Errorf("decision identity mismatch")
	}
	return scenarioResult{Observation: persistedObservation, Decision: persistedDecision, Scan: scan}, nil
}

func createLink(ctx context.Context, db *sql.DB, workspace, suffix string) (uint64, string, string, error) {
	primary := "https://customer.example/" + suffix
	routing := []links.RoutingRule{}
	variants := []links.ABVariant{}
	fingerprint, _, err := links.RiskFingerprint(primary, routing, variants)
	if err != nil {
		return 0, "", "", err
	}
	routingJSON, _ := json.Marshal(routing)
	abJSON, _ := json.Marshal(variants)
	res, err := db.ExecContext(ctx, `
INSERT INTO links
(workspace_id,hostname,domain_kind,code,title,primary_destination,redirect_status,status,version,risk_fingerprint,routing_json,ab_json,utm_json,access_json)
VALUES (?,?,'official',?,?,?,302,'active',1,?,?,?,'{}','{}')`,
		workspace, "go.example.test", suffix, "P16 T005 fixture", primary, fingerprint, string(routingJSON), string(abJSON))
	if err != nil {
		return 0, "", "", err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, "", "", err
	}
	return uint64(id), fingerprint, primary, nil
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func safeEvidence(evidence map[string]any) bool {
	if evidence["category"] != "deterministic-fixture" {
		return false
	}
	nested, ok := evidence["nested"].(map[string]any)
	if !ok || nested["safe"] != "retained" {
		return false
	}
	_, hasSecret := evidence["client_secret"]
	_, hasToken := nested["api_token"]
	return !hasSecret && !hasToken
}

func allAuthorityBound(results []scenarioResult, fingerprint, policyVersion string) bool {
	for _, result := range results {
		if result.Decision.RiskFingerprint != fingerprint || result.Decision.PolicyVersion != policyVersion || result.Decision.ScanID != result.Scan.ID {
			return false
		}
	}
	return true
}

func allCompleted(results []scenarioResult) bool {
	for _, result := range results {
		if result.Scan.Status != trust.ScanStatusCompleted || result.Scan.CompletedAt == nil {
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
