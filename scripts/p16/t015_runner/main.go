package main

import (
	"context"
	"encoding/json"
	"fmt"
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

func main() {
	out, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		out.Case = "P16-T015"
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
		Case:         "P16-T015",
		Status:       "FAIL",
		Fixture:      "real MySQL durable P16 domain reputation authority projected through inherited P06 independent entitlement/ownership/DNS/HTTPS/routing axes",
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

	now := time.Now().UTC().Truncate(time.Microsecond)
	allowDomain, err := domainfixture.CreateReadyDomain(ctx, db, "t015-allow", now)
	if err != nil {
		return out, err
	}
	reviewDomain, err := domainfixture.CreateReadyDomain(ctx, db, "t015-review", now)
	if err != nil {
		return out, err
	}

	store := trust.NewStore(db)
	policy := trust.DomainRiskPolicy{
		Version:           "p16-domain-policy-t015",
		RequiredProviders: []string{"domain-reputation"},
		AllowTTL:          20 * time.Minute,
		RevalidateAfter:   10 * time.Minute,
		RetryAfter:        time.Minute,
	}
	allowService, err := trust.NewDomainRiskService(store, policy, domainfixture.Provider{
		Name: "domain-reputation", Outcome: trust.ProviderAllow, SignalCode: "domain-allow", Evidence: map[string]any{"fixture": "t015-allow"},
	})
	if err != nil {
		return out, err
	}
	reviewService, err := trust.NewDomainRiskService(store, policy, domainfixture.Provider{
		Name: "domain-reputation", Outcome: trust.ProviderReview, SignalCode: "domain-review", Evidence: map[string]any{"fixture": "t015-review"},
	})
	if err != nil {
		return out, err
	}

	allowResult, err := allowService.Evaluate(ctx, trust.EvaluateDomainRiskInput{
		WorkspaceID: allowDomain.WorkspaceID, DomainID: allowDomain.ID, RequestKind: trust.DomainRiskInitial,
		IdempotencyKey: "p16-t015-allow", ActorID: "p16-t015-worker", Reason: "initial domain reputation evaluation",
		CorrelationID: "p16-t015-allow-correlation", Now: now,
	})
	if err != nil {
		return out, err
	}
	reviewResult, err := reviewService.Evaluate(ctx, trust.EvaluateDomainRiskInput{
		WorkspaceID: reviewDomain.WorkspaceID, DomainID: reviewDomain.ID, RequestKind: trust.DomainRiskInitial,
		IdempotencyKey: "p16-t015-review", ActorID: "p16-t015-worker", Reason: "initial domain reputation evaluation",
		CorrelationID: "p16-t015-review-correlation", Now: now,
	})
	if err != nil {
		return out, err
	}

	p06Store := domains.NewMySQLStore(db)
	allowStored, _, allowReadiness, err := p06Store.ResolveDomainReadiness(ctx, allowDomain.WorkspaceID, allowDomain.ID, now.Add(time.Second))
	if err != nil {
		return out, err
	}
	reviewStored, _, reviewReadiness, err := p06Store.ResolveDomainReadiness(ctx, reviewDomain.WorkspaceID, reviewDomain.ID, now.Add(time.Second))
	if err != nil {
		return out, err
	}

	evaluations, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM domain_risk_evaluations WHERE id IN (?,?)`, allowResult.Evaluation.ID, reviewResult.Evaluation.ID)
	if err != nil {
		return out, err
	}
	observations, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM domain_risk_provider_observations WHERE evaluation_id IN (?,?)`, allowResult.Evaluation.ID, reviewResult.Evaluation.ID)
	if err != nil {
		return out, err
	}
	audits, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM domain_risk_audit_events WHERE evaluation_id IN (?,?)`, allowResult.Evaluation.ID, reviewResult.Evaluation.ID)
	if err != nil {
		return out, err
	}
	p06Revalidations, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domain_revalidations WHERE domain_id IN (?,?) AND axis='risk'`, allowDomain.ID, reviewDomain.ID)
	if err != nil {
		return out, err
	}
	axesUnchanged, err := domainfixture.ScalarInt(ctx, db, `
SELECT COUNT(*) FROM custom_domains
WHERE id IN (?,?) AND routing_state='enabled' AND ownership_status='verified' AND ingress_dns_status='valid' AND https_status='active'`, allowDomain.ID, reviewDomain.ID)
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{
		"domain_risk_evaluations":        evaluations,
		"provider_observations":          observations,
		"domain_risk_audit_events":       audits,
		"p06_risk_revalidation_records":  p06Revalidations,
		"domains_with_other_axes_intact": axesUnchanged,
	}
	out.Checks = map[string]bool{
		"allow_decision_is_durable_and_projected":  allowResult.Created && allowResult.Evaluation.State == trust.DomainRiskAllow && allowStored.RiskStatus == domains.RiskAllow,
		"review_decision_is_durable_and_non_allow": reviewResult.Created && reviewResult.Evaluation.State == trust.DomainRiskReview && reviewStored.RiskStatus == domains.RiskReview,
		"provider_observations_are_durable":        observations == 2 && len(allowResult.Observations) == 1 && len(reviewResult.Observations) == 1,
		"domain_risk_audit_is_durable":             audits >= 4,
		"p06_risk_revalidation_seam_is_used":       p06Revalidations >= 4,
		"entitlement_snapshot_is_independent":      allowResult.Evaluation.EntitlementSnapshot == "active-source-present" && reviewResult.Evaluation.EntitlementSnapshot == "active-source-present",
		"ownership_dns_https_routing_axes_unchanged": axesUnchanged == 2 &&
			allowResult.Evaluation.OwnershipSnapshot == "verified" && allowResult.Evaluation.IngressDNSSnapshot == "valid" && allowResult.Evaluation.HTTPSSnapshot == "active" && allowResult.Evaluation.RoutingSnapshot == "enabled" &&
			reviewResult.Evaluation.OwnershipSnapshot == "verified" && reviewResult.Evaluation.IngressDNSSnapshot == "valid" && reviewResult.Evaluation.HTTPSSnapshot == "active" && reviewResult.Evaluation.RoutingSnapshot == "enabled",
		"only_risk_allow_satisfies_inherited_readiness_axis": allowReadiness.RiskReady && allowReadiness.ReadyForRouting && !reviewReadiness.RiskReady && !reviewReadiness.ReadyForRouting,
		"both_correlations_are_persisted":                    evaluations == 2 && allowResult.Evaluation.CorrelationID == "p16-t015-allow-correlation" && reviewResult.Evaluation.CorrelationID == "p16-t015-review-correlation",
	}
	if domainfixture.AllTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}
