package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/trust"
	"github.com/Techshrr/GoJet/scripts/p16/domainfixture"
)

type output struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	Fixture      string          `json:"fixture"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

type scenario struct {
	name           string
	handler        http.HandlerFunc
	expectedState  trust.DomainRiskState
	expectedRisk   domains.DomainRiskStatus
	expectedReason string
}

func main() {
	out, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		out.Case = "P16-T017"
		out.Status = "FAIL"
		if out.Checks == nil {
			out.Checks = map[string]bool{"runner_completed": false}
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
	if err != nil || out.Status != "PASS" {
		os.Exit(1)
	}
}

func run() (output, error) {
	out := output{
		Case:         "P16-T017",
		Status:       "FAIL",
		Fixture:      "real MySQL plus deterministic local semantic-provider HTTP fixtures proving outage/partial/malformed/review non-allow mapping and durable evidence redaction",
		RecordCounts: map[string]int{},
		Checks:       map[string]bool{},
	}
	db, err := domainfixture.OpenDB()
	if err != nil {
		return out, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	out.MySQLVersion, err = domainfixture.MySQLVersion(ctx, db)
	if err != nil {
		return out, err
	}

	scenarios := []scenario{
		{
			name: "outage",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"unavailable"}`))
			},
			expectedState:  trust.DomainRiskReview,
			expectedRisk:   domains.RiskReview,
			expectedReason: "provider-unavailable",
		},
		{
			name: "partial",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"complete":false,"verdict":"allow","signal_code":"domain-allow","evidence":{"api_secret":"p16-domain-provider-secret-fixture"}}`))
			},
			expectedState:  trust.DomainRiskProviderPartial,
			expectedRisk:   domains.RiskReview,
			expectedReason: "provider-partial",
		},
		{
			name: "malformed",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"complete":true,"verdict":`))
			},
			expectedState:  trust.DomainRiskMalformed,
			expectedRisk:   domains.RiskMalformed,
			expectedReason: "provider-malformed",
		},
		{
			name: "review-redaction",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"complete":true,"verdict":"review","signal_code":"domain-review","evidence":{"summary":"manual review","api_secret":"p16-domain-provider-secret-fixture","nested":{"client_secret":"p16-domain-provider-secret-fixture","safe":"kept"}}}`))
			},
			expectedState:  trust.DomainRiskReview,
			expectedRisk:   domains.RiskReview,
			expectedReason: "provider-review",
		},
	}

	store := trust.NewStore(db)
	policy := trust.DomainRiskPolicy{
		Version:           "p16-domain-policy-t017",
		RequiredProviders: []string{"domain-reputation"},
		AllowTTL:          20 * time.Minute,
		RevalidateAfter:   10 * time.Minute,
		RetryAfter:        time.Minute,
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	allNonAllow := true
	allSafeReasons := true
	allProjected := true
	allObserved := true
	evaluationIDs := make([]uint64, 0, len(scenarios))
	domainIDs := make([]uint64, 0, len(scenarios))

	for index, sc := range scenarios {
		server := httptest.NewServer(sc.handler)
		domain, createErr := domainfixture.CreateReadyDomain(ctx, db, "t017-"+sc.name, now.Add(time.Duration(index)*time.Microsecond))
		if createErr != nil {
			server.Close()
			return out, createErr
		}
		service, serviceErr := trust.NewDomainRiskService(store, policy, trust.SemanticProviderClient{
			Name: "domain-reputation", Endpoint: server.URL, HTTPClient: server.Client(),
		})
		if serviceErr != nil {
			server.Close()
			return out, serviceErr
		}
		result, evaluateErr := service.Evaluate(ctx, trust.EvaluateDomainRiskInput{
			WorkspaceID: domain.WorkspaceID, DomainID: domain.ID, RequestKind: trust.DomainRiskInitial,
			IdempotencyKey: "p16-t017-" + sc.name, ActorID: "p16-t017-worker", Reason: "domain provider failure matrix",
			CorrelationID: "p16-t017-" + sc.name + "-correlation", Now: now.Add(time.Duration(index) * time.Microsecond),
		})
		server.Close()
		if evaluateErr != nil {
			return out, evaluateErr
		}
		stored, getErr := domains.NewMySQLStore(db).GetDomain(ctx, domain.WorkspaceID, domain.ID)
		if getErr != nil {
			return out, getErr
		}
		allNonAllow = allNonAllow && result.Evaluation.State != trust.DomainRiskAllow && result.Evaluation.ValidUntil == nil
		allSafeReasons = allSafeReasons && result.Evaluation.ReasonCategory == sc.expectedReason
		allProjected = allProjected && result.Evaluation.State == sc.expectedState && stored.RiskStatus == sc.expectedRisk
		allObserved = allObserved && len(result.Observations) == 1
		evaluationIDs = append(evaluationIDs, result.Evaluation.ID)
		domainIDs = append(domainIDs, domain.ID)
	}

	evaluationRows, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM domain_risk_evaluations WHERE policy_version='p16-domain-policy-t017'`)
	if err != nil {
		return out, err
	}
	observationRows, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM domain_risk_provider_observations WHERE evaluation_id IN (?,?,?,?)`, evaluationIDs[0], evaluationIDs[1], evaluationIDs[2], evaluationIDs[3])
	if err != nil {
		return out, err
	}
	leakedEvidenceRows, err := domainfixture.ScalarInt(ctx, db, `
SELECT COUNT(*) FROM domain_risk_provider_observations
WHERE evaluation_id IN (?,?,?,?) AND (
  LOWER(CAST(evidence_json AS CHAR)) LIKE '%p16-domain-provider-secret-fixture%' OR
  LOWER(CAST(evidence_json AS CHAR)) LIKE '%api_secret%' OR
  LOWER(CAST(evidence_json AS CHAR)) LIKE '%client_secret%'
)`, evaluationIDs[0], evaluationIDs[1], evaluationIDs[2], evaluationIDs[3])
	if err != nil {
		return out, err
	}
	nonAllowDomains, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domains WHERE id IN (?,?,?,?) AND risk_status <> 'allow'`, domainIDs[0], domainIDs[1], domainIDs[2], domainIDs[3])
	if err != nil {
		return out, err
	}
	axesIntact, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domains WHERE id IN (?,?,?,?) AND ownership_status='verified' AND ingress_dns_status='valid' AND https_status='active' AND routing_state='enabled'`, domainIDs[0], domainIDs[1], domainIDs[2], domainIDs[3])
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{
		"provider_failure_evaluations":   evaluationRows,
		"provider_observations":          observationRows,
		"leaked_evidence_rows":           leakedEvidenceRows,
		"non_allow_domain_projections":   nonAllowDomains,
		"domains_with_other_axes_intact": axesIntact,
	}
	out.Checks = map[string]bool{
		"outage_partial_malformed_and_review_never_allow":    allNonAllow && nonAllowDomains == 4,
		"provider_failure_states_project_fail_closed":        allProjected,
		"reason_categories_are_allowlisted_and_stable":       allSafeReasons,
		"provider_observations_are_durable":                  allObserved && observationRows == 4,
		"provider_evidence_is_redacted_before_storage":       leakedEvidenceRows == 0,
		"provider_failure_does_not_mutate_other_domain_axes": axesIntact == 4,
		"all_failure_matrix_evaluations_are_durable":         evaluationRows == 4 && len(evaluationIDs) == 4,
	}
	if domainfixture.AllTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}
