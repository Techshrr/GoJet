package main

import (
	"context"
	"encoding/json"
	"errors"
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
		out.Case = "P16-T016"
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
		Case:         "P16-T016",
		Status:       "FAIL",
		Fixture:      "real MySQL revalidation lifecycle proving idempotency/rate authority, durable last/next checks, fail-closed begin projection and stale expiry without cross-axis mutation",
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
	domain, err := domainfixture.CreateReadyDomain(ctx, db, "t016", now)
	if err != nil {
		return out, err
	}
	store := trust.NewStore(db)
	policy := trust.DomainRiskPolicy{
		Version:           "p16-domain-policy-t016",
		RequiredProviders: []string{"domain-reputation"},
		AllowTTL:          2 * time.Minute,
		RevalidateAfter:   time.Minute,
		RetryAfter:        30 * time.Second,
	}
	service, err := trust.NewDomainRiskService(store, policy, domainfixture.Provider{
		Name: "domain-reputation", Outcome: trust.ProviderAllow, SignalCode: "domain-allow", Evidence: map[string]any{"fixture": "t016"},
	})
	if err != nil {
		return out, err
	}

	initialInput := trust.EvaluateDomainRiskInput{
		WorkspaceID: domain.WorkspaceID, DomainID: domain.ID, RequestKind: trust.DomainRiskInitial,
		IdempotencyKey: "p16-t016-initial", ActorID: "p16-t016-worker", Reason: "initial domain reputation evaluation",
		CorrelationID: "p16-t016-initial-correlation", Now: now,
	}
	initial, err := service.Evaluate(ctx, initialInput)
	if err != nil {
		return out, err
	}
	replay, err := service.Evaluate(ctx, initialInput)
	if err != nil {
		return out, err
	}

	_, rateErr := service.Evaluate(ctx, trust.EvaluateDomainRiskInput{
		WorkspaceID: domain.WorkspaceID, DomainID: domain.ID, RequestKind: trust.DomainRiskRevalidation,
		IdempotencyKey: "p16-t016-too-soon", ActorID: "p16-t016-worker", Reason: "scheduled domain risk revalidation",
		CorrelationID: "p16-t016-too-soon-correlation", Now: now.Add(30 * time.Second),
	})

	revalidationNow := now.Add(61 * time.Second)
	revalidated, err := service.Evaluate(ctx, trust.EvaluateDomainRiskInput{
		WorkspaceID: domain.WorkspaceID, DomainID: domain.ID, RequestKind: trust.DomainRiskRevalidation,
		IdempotencyKey: "p16-t016-revalidate", ActorID: "p16-t016-worker", Reason: "scheduled domain risk revalidation",
		CorrelationID: "p16-t016-revalidate-correlation", Now: revalidationNow,
	})
	if err != nil {
		return out, err
	}
	staleNow := revalidationNow.Add(2*time.Minute + time.Second)
	expired, err := service.ExpireAllowIfStale(ctx, domain.WorkspaceID, domain.ID, "p16-t016-worker", "p16-t016-expire-correlation", staleNow)
	if err != nil {
		return out, err
	}
	staleEvaluation, err := service.GetDomainRiskEvaluation(ctx, domain.WorkspaceID, revalidated.Evaluation.ID)
	if err != nil {
		return out, err
	}
	stored, _, readiness, err := domains.NewMySQLStore(db).ResolveDomainReadiness(ctx, domain.WorkspaceID, domain.ID, staleNow)
	if err != nil {
		return out, err
	}

	evaluationRows, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM domain_risk_evaluations WHERE domain_id=?`, domain.ID)
	if err != nil {
		return out, err
	}
	pendingRevalidationRows, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domain_revalidations WHERE domain_id=? AND axis='risk' AND result='pending'`, domain.ID)
	if err != nil {
		return out, err
	}
	staleRevalidationRows, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domain_revalidations WHERE domain_id=? AND axis='risk' AND result='stale'`, domain.ID)
	if err != nil {
		return out, err
	}
	rateAuditRows, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM domain_risk_audit_events WHERE domain_id=? AND action='domain-risk.revalidate' AND result='conflict' AND reason_category='revalidation-rate-limited'`, domain.ID)
	if err != nil {
		return out, err
	}
	axesUnchanged, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domains WHERE id=? AND ownership_status='verified' AND ingress_dns_status='valid' AND https_status='active' AND routing_state='enabled'`, domain.ID)
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{
		"domain_risk_evaluations":           evaluationRows,
		"pending_p06_revalidations":         pendingRevalidationRows,
		"stale_p06_revalidations":           staleRevalidationRows,
		"rate_limited_audit_events":         rateAuditRows,
		"domains_with_other_axes_unchanged": axesUnchanged,
	}
	out.Checks = map[string]bool{
		"initial_allow_has_last_and_next_check":                           initial.Created && initial.Evaluation.State == trust.DomainRiskAllow && initial.Evaluation.CheckedAt != nil && initial.Evaluation.NextDueAt != nil && initial.Evaluation.ValidUntil != nil,
		"same_idempotency_replays_existing_evaluation":                    !replay.Created && replay.Evaluation.ID == initial.Evaluation.ID && evaluationRows == 2,
		"early_revalidation_is_rate_limited_and_audited":                  errors.Is(rateErr, trust.ErrConflict) && rateAuditRows == 1,
		"revalidation_is_durable_and_returns_allow_only_after_completion": revalidated.Created && revalidated.Evaluation.RequestKind == trust.DomainRiskRevalidation && revalidated.Evaluation.State == trust.DomainRiskAllow && revalidated.Evaluation.CheckedAt != nil && revalidated.Evaluation.NextDueAt != nil,
		"revalidation_records_fail_closed_pending_before_completion":      pendingRevalidationRows >= 2,
		"expired_allow_becomes_stale":                                     expired && staleEvaluation.State == trust.DomainRiskStale && stored.RiskStatus == domains.RiskStale && staleRevalidationRows == 1,
		"stale_risk_fails_inherited_readiness":                            !readiness.RiskReady && !readiness.ReadyForRouting,
		"stale_transition_does_not_overwrite_other_axes":                  axesUnchanged == 1,
		"policy_timing_is_monotonic":                                      initial.Evaluation.CheckedAt != nil && initial.Evaluation.NextDueAt != nil && initial.Evaluation.ValidUntil != nil && initial.Evaluation.NextDueAt.After(*initial.Evaluation.CheckedAt) && initial.Evaluation.ValidUntil.After(*initial.Evaluation.CheckedAt),
	}
	if domainfixture.AllTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}
