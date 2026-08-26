package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

type failureScenario struct {
	Name           string
	Outcome        trust.ProviderOutcome
	SignalCode     string
	DecisionReason string
	Observation    trust.ProviderObservation
	Decision       trust.DestinationDecision
	Scan           trust.DestinationScan
}

func main() {
	result, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		result.Case = "P16-T006"
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
		Case:         "P16-T006",
		Status:       "FAIL",
		Fixture:      "local provider protocol fixture with deterministic timeout, transport, partial, malformed and unavailable modes",
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
		switch r.URL.Path {
		case "/timeout":
			select {
			case <-r.Context().Done():
				return
			case <-time.After(2 * time.Second):
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"complete":true,"verdict":"allow"}`))
			}
		case "/partial":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"complete":false,"verdict":"allow","signal_code":"unsafe-partial","evidence":{"client_secret":"p16-partial-secret-fixture"}}`))
		case "/malformed":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"complete":true,"verdict":"allow","evidence":{"token":"p16-malformed-secret-fixture"}`))
		case "/unavailable":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`provider internal secret p16-unavailable-secret-fixture`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		return out, err
	}
	serverDialer := trust.DialContextFunc(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, server.Listener.Addr().String())
	})
	transportFailureDialer := trust.DialContextFunc(func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("p16-transport-secret-fixture")
	})

	workspace := "p16-t006-workspace"
	linkID, fingerprint, primary, err := createLink(ctx, db, workspace, "p16-t006")
	if err != nil {
		return out, err
	}
	store := trust.NewStore(db)
	policy := trust.DestinationPolicy{
		Version:           "p16-semantic-policy-v1",
		RequiredProviders: []string{"semantic-fixture"},
		AllowTTL:          15 * time.Minute,
	}

	scenarios := []failureScenario{
		{Name: "timeout", Outcome: trust.ProviderUnavailable, SignalCode: "provider-timeout", DecisionReason: "provider-unavailable"},
		{Name: "transport", Outcome: trust.ProviderUnavailable, SignalCode: "provider-transport-error", DecisionReason: "provider-unavailable"},
		{Name: "partial", Outcome: trust.ProviderUnknown, SignalCode: "provider-partial", DecisionReason: "provider-unknown"},
		{Name: "malformed", Outcome: trust.ProviderUnknown, SignalCode: "provider-malformed", DecisionReason: "provider-unknown"},
		{Name: "unavailable", Outcome: trust.ProviderUnavailable, SignalCode: "provider-unavailable", DecisionReason: "provider-unavailable"},
	}

	for i := range scenarios {
		scenario := &scenarios[i]
		var client *http.Client
		endpoint := "http://provider.test:" + port + "/" + scenario.Name
		if scenario.Name == "transport" {
			client = trust.NewInspectionHTTPClient(publicResolver{}, transportFailureDialer)
		} else {
			client = trust.NewInspectionHTTPClient(publicResolver{}, serverDialer)
		}
		provider := trust.SemanticProviderClient{Name: "semantic-fixture", Endpoint: endpoint, HTTPClient: client}
		observeCtx := ctx
		var observeCancel context.CancelFunc
		if scenario.Name == "timeout" {
			observeCtx, observeCancel = context.WithTimeout(ctx, 75*time.Millisecond)
		}
		observation, err := provider.Observe(observeCtx, primary)
		if observeCancel != nil {
			observeCancel()
		}
		if err != nil {
			return out, fmt.Errorf("%s provider normalization: %w", scenario.Name, err)
		}
		correlation := "p16-t006-" + scenario.Name
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
			return out, fmt.Errorf("%s enqueue: %w", scenario.Name, err)
		}
		persistedObservation, err := store.RecordProviderObservation(ctx, trust.RecordProviderObservationInput{
			WorkspaceID:   workspace,
			ScanID:        enqueued.Scan.ID,
			Observation:   observation,
			ActorID:       "p16-ci-security",
			CorrelationID: correlation,
		})
		if err != nil {
			return out, fmt.Errorf("%s observation persist: %w", scenario.Name, err)
		}
		decision, err := store.FinalizeDestinationDecision(ctx, trust.FinalizeDestinationDecisionInput{
			WorkspaceID:       workspace,
			ScanID:            enqueued.Scan.ID,
			Policy:            policy,
			LocalSafetyPassed: true,
			ActorID:           "p16-ci-security",
			CorrelationID:     correlation,
		})
		if err != nil {
			return out, fmt.Errorf("%s decision persist: %w", scenario.Name, err)
		}
		scan, _, err := store.GetDestinationScan(ctx, workspace, enqueued.Scan.ID)
		if err != nil {
			return out, err
		}
		scenario.Observation = persistedObservation
		scenario.Decision = decision
		scenario.Scan = scan
	}

	observationCount, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_provider_observations o JOIN destination_risk_scans s ON s.id=o.scan_id WHERE s.workspace_id=?`, workspace)
	if err != nil {
		return out, err
	}
	decisionCount, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_decisions WHERE workspace_id=?`, workspace)
	if err != nil {
		return out, err
	}
	allowCount, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_decisions WHERE workspace_id=? AND state='allow'`, workspace)
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
 LOWER(CAST(o.evidence_json AS CHAR)) LIKE '%p16-transport-secret-fixture%' OR
 LOWER(CAST(o.evidence_json AS CHAR)) LIKE '%p16-partial-secret-fixture%' OR
 LOWER(CAST(o.evidence_json AS CHAR)) LIKE '%p16-malformed-secret-fixture%' OR
 LOWER(CAST(o.evidence_json AS CHAR)) LIKE '%p16-unavailable-secret-fixture%'
)`, workspace)
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{
		"failure_modes":         len(scenarios),
		"provider_observations": observationCount,
		"policy_decisions":      decisionCount,
		"allow_decisions":       allowCount,
		"completed_scans":       completedCount,
		"audit_events":          auditCount,
		"secret_matches":        secretCount,
	}
	out.Checks = map[string]bool{
		"timeout_maps_to_unavailable_non_allow":          scenarioMatches(scenarios, "timeout"),
		"transport_error_maps_to_unavailable_non_allow":  scenarioMatches(scenarios, "transport"),
		"partial_response_maps_to_unknown_non_allow":     scenarioMatches(scenarios, "partial"),
		"malformed_payload_maps_to_unknown_non_allow":    scenarioMatches(scenarios, "malformed"),
		"provider_503_maps_to_unavailable_non_allow":     scenarioMatches(scenarios, "unavailable"),
		"no_failure_mode_can_produce_allow":              allowCount == 0 && allNonAllow(scenarios),
		"failure_observations_and_decisions_are_durable": observationCount == 5 && decisionCount == 5,
		"failure_scans_close_with_explicit_authority":    completedCount == 5 && allCompleted(scenarios),
		"failure_evidence_is_redacted_and_safe":          secretCount == 0 && allFailureEvidenceSafe(scenarios),
		"correlated_audit_covers_each_failure_lifecycle": auditCount == 15,
	}
	if allTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func scenarioMatches(scenarios []failureScenario, name string) bool {
	for _, scenario := range scenarios {
		if scenario.Name != name {
			continue
		}
		return scenario.Observation.Outcome == scenario.Outcome &&
			scenario.Observation.SignalCode == scenario.SignalCode &&
			scenario.Decision.State == trust.DecisionReview &&
			scenario.Decision.ReasonCategory == scenario.DecisionReason
	}
	return false
}

func allNonAllow(scenarios []failureScenario) bool {
	for _, scenario := range scenarios {
		if scenario.Decision.State == trust.DecisionAllow {
			return false
		}
	}
	return true
}

func allCompleted(scenarios []failureScenario) bool {
	for _, scenario := range scenarios {
		if scenario.Scan.Status != trust.ScanStatusCompleted || scenario.Scan.CompletedAt == nil {
			return false
		}
	}
	return true
}

func allFailureEvidenceSafe(scenarios []failureScenario) bool {
	for _, scenario := range scenarios {
		category, ok := scenario.Observation.Evidence["failure_category"].(string)
		if !ok || strings.TrimSpace(category) == "" || len(scenario.Observation.Evidence) != 1 {
			return false
		}
	}
	return true
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
		workspace, "go.example.test", suffix, "P16 T006 fixture", primary, fingerprint, string(routingJSON), string(abJSON))
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

func allTrue(checks map[string]bool) bool {
	for _, ok := range checks {
		if !ok {
			return false
		}
	}
	return len(checks) > 0
}
