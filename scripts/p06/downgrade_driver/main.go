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

func main() {
	caseFlag := flag.String("case", "P06-T015", "P06 normal-downgrade case ID")
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
	if *caseFlag != "P06-T015" {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, fmt.Sprintf("unsupported case %s", *caseFlag))
	} else if err := caseT015(ctx, db, &result); err != nil {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, err.Error())
	}
	writeJSON(map[string]caseResult{*caseFlag: result})
	if result.Status != "PASS" {
		os.Exit(1)
	}
}

func caseT015(ctx context.Context, db *sql.DB, out *caseResult) error {
	workspace := "p06-t015-grace"
	store := domains.NewMySQLStore(db)
	downgradeAt := fixedNow()
	startsAt := downgradeAt.Add(-30 * 24 * time.Hour)
	expiresAt := downgradeAt

	if _, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace,
		SourceKey: "subscription-business-t015",
		Status: domains.EntitlementActive,
		DomainLimit: 2,
		StartsAt: startsAt,
		ExpiresAt: &expiresAt,
		DecisionReason: "active business plan before normal downgrade",
	}, "corr-p06-t015-plan"); err != nil {
		return err
	}
	created, err := store.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspace,
		ActorID: "actor-t015",
		CorrelationID: "corr-p06-t015-create-before",
		Reason: "existing active domain before downgrade",
		Hostname: "grace-t015.example.com",
		Now: downgradeAt.Add(-time.Minute),
	})
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE custom_domains
		SET routing_state='enabled', ownership_status='verified', ingress_dns_status='valid',
		    https_status='active', risk_status='allow', ownership_verified_at=?,
		    ingress_dns_checked_at=?, https_checked_at=?, risk_checked_at=?,
		    risk_policy_version='t015-ready-fixture', risk_evidence_ref='risk:t015:ready'
		WHERE workspace_id=? AND id=?`,
		downgradeAt.Add(-time.Minute), downgradeAt.Add(-time.Minute), downgradeAt.Add(-time.Minute), downgradeAt.Add(-time.Minute),
		workspace, created.Domain.ID); err != nil {
		return err
	}
	beforeDomain, err := store.GetDomain(ctx, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	beforeEntitlement, err := store.ResolveEntitlement(ctx, workspace, downgradeAt.Add(-time.Microsecond))
	if err != nil {
		return err
	}
	if !beforeEntitlement.MutationAllowed || !beforeEntitlement.ExistingRoutingAllowed || !beforeDomain.Readiness(beforeEntitlement).ReadyForRouting {
		return fmt.Errorf("precondition domain not active/ready: entitlement=%+v readiness=%+v", beforeEntitlement, beforeDomain.Readiness(beforeEntitlement))
	}

	downgraded, err := store.ApplyNormalPlanDowngrade(ctx, domains.NormalPlanDowngradeInput{
		WorkspaceID: workspace,
		SourceKey: "subscription-business-t015",
		DegradedAt: downgradeAt,
		DecisionReason: "normal_plan_downgrade_grace",
		CorrelationID: "corr-p06-t015-downgrade",
	})
	if err != nil {
		return err
	}
	expectedDeadline := downgradeAt.AddDate(0, 0, 7)
	if downgraded.DegradedAt == nil || downgraded.GraceUntil == nil || !downgraded.DegradedAt.Equal(downgradeAt) || !downgraded.GraceUntil.Equal(expectedDeadline) {
		return fmt.Errorf("persisted grace window degraded=%v until=%v want=%s..%s", downgraded.DegradedAt, downgraded.GraceUntil, downgradeAt, expectedDeadline)
	}

	immediate, err := store.ResolveEntitlement(ctx, workspace, downgradeAt)
	if err != nil {
		return err
	}
	if immediate.Source != domains.SourcePlan || immediate.Status != domains.EntitlementActive || !immediate.GracePeriod || immediate.MutationAllowed || !immediate.ExistingRoutingAllowed || immediate.GraceUntil == nil || !immediate.GraceUntil.Equal(expectedDeadline) {
		return fmt.Errorf("immediate downgrade authority incorrect: %+v", immediate)
	}
	immediateDomain, err := store.GetDomain(ctx, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	immediateReadiness := immediateDomain.Readiness(immediate)
	if immediateReadiness.ReadyForNewLinks || !immediateReadiness.ReadyForRouting {
		return fmt.Errorf("grace readiness incorrect: %+v", immediateReadiness)
	}

	_, createErr := store.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspace,
		ActorID: "actor-t015",
		CorrelationID: "corr-p06-t015-create-during-grace",
		Reason: "attempt create during downgrade grace",
		Hostname: "blocked-t015.example.com",
		Now: downgradeAt.Add(time.Minute),
	})
	if !errors.Is(createErr, domains.ErrEntitlementRequired) {
		return fmt.Errorf("create during grace error=%v want entitlement required", createErr)
	}

	beforeToken := immediateDomain.OwnershipTokenVersion
	_, rotateErr := store.RotateOwnershipSecret(ctx, domains.RotateOwnershipSecretInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t015",
		CorrelationID: "corr-p06-t015-rotate-during-grace",
		Reason: "attempt ownership rotation during downgrade grace",
		Now: downgradeAt.Add(2 * time.Minute),
	})
	if !errors.Is(rotateErr, domains.ErrEntitlementRequired) {
		return fmt.Errorf("rotation during grace error=%v want entitlement required", rotateErr)
	}
	for _, kind := range []domains.DomainMutationKind{domains.DomainMutationActivate, domains.DomainMutationRestore, domains.DomainMutationAssignLink} {
		decision, err := store.CheckDomainMutationAuthority(ctx, workspace, created.Domain.ID, kind, downgradeAt.Add(3*time.Minute))
		if !errors.Is(err, domains.ErrEntitlementRequired) || decision.Allowed || decision.Code != "entitlement_required" {
			return fmt.Errorf("mutation %s during grace decision=%+v err=%v", kind, decision, err)
		}
	}

	domainRows, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM custom_domains WHERE workspace_id=?", workspace)
	if err != nil {
		return err
	}
	allocated, _, err := store.Usage(ctx, workspace)
	if err != nil {
		return err
	}
	afterDeniedDomain, err := store.GetDomain(ctx, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	if domainRows != 1 || allocated != 1 || afterDeniedDomain.OwnershipTokenVersion != beforeToken {
		return fmt.Errorf("denied grace mutations changed state: domains=%d allocated=%d token=%d want token=%d", domainRows, allocated, afterDeniedDomain.OwnershipTokenVersion, beforeToken)
	}

	lastInstant := expectedDeadline.Add(-time.Microsecond)
	lastGrace, err := store.ResolveEntitlement(ctx, workspace, lastInstant)
	if err != nil {
		return err
	}
	if !lastGrace.GracePeriod || lastGrace.MutationAllowed || !lastGrace.ExistingRoutingAllowed || !afterDeniedDomain.Readiness(lastGrace).ReadyForRouting {
		return fmt.Errorf("grace did not survive through last instant: %+v readiness=%+v", lastGrace, afterDeniedDomain.Readiness(lastGrace))
	}
	exactDeadline, err := store.ResolveEntitlement(ctx, workspace, expectedDeadline)
	if err != nil {
		return err
	}
	deadlineReadiness := afterDeniedDomain.Readiness(exactDeadline)
	if exactDeadline.Status != domains.EntitlementExpired || exactDeadline.GracePeriod || exactDeadline.MutationAllowed || exactDeadline.ExistingRoutingAllowed || deadlineReadiness.ReadyForRouting || deadlineReadiness.ReadyForNewLinks {
		return fmt.Errorf("grace did not expire exactly at deadline: entitlement=%+v readiness=%+v", exactDeadline, deadlineReadiness)
	}

	replay, err := store.ApplyNormalPlanDowngrade(ctx, domains.NormalPlanDowngradeInput{
		WorkspaceID: workspace,
		SourceKey: "subscription-business-t015",
		DegradedAt: downgradeAt,
		DecisionReason: "normal_plan_downgrade_grace",
		CorrelationID: "corr-p06-t015-downgrade-replay",
	})
	if err != nil || replay.GraceUntil == nil || !replay.GraceUntil.Equal(expectedDeadline) {
		return fmt.Errorf("idempotent downgrade replay changed deadline: source=%+v err=%v", replay, err)
	}
	_, extendErr := store.ApplyNormalPlanDowngrade(ctx, domains.NormalPlanDowngradeInput{
		WorkspaceID: workspace,
		SourceKey: "subscription-business-t015",
		DegradedAt: downgradeAt.Add(time.Hour),
		DecisionReason: "attempt to extend grace",
		CorrelationID: "corr-p06-t015-downgrade-extend",
	})
	if !errors.Is(extendErr, domains.ErrEntitlementConflict) {
		return fmt.Errorf("grace extension attempt error=%v want entitlement conflict", extendErr)
	}

	auditCount, err := scalarInt(ctx, db, `
		SELECT COUNT(*) FROM custom_domain_audit_events
		WHERE workspace_id=? AND action='domain.entitlement.plan.downgrade'`, workspace)
	if err != nil {
		return err
	}
	if auditCount != 1 {
		return fmt.Errorf("downgrade audit count=%d want=1", auditCount)
	}

	manualWorkspace := "p06-t015-manual-coexist"
	manualExpires := downgradeAt.Add(30 * 24 * time.Hour)
	if _, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: manualWorkspace,
		SourceKey: "subscription-business-t015-manual",
		Status: domains.EntitlementActive,
		DomainLimit: 2,
		StartsAt: startsAt,
		ExpiresAt: &expiresAt,
		DecisionReason: "business plan before downgrade with manual source",
	}, "corr-p06-t015-manual-plan"); err != nil {
		return err
	}
	if _, err := store.CreateManualApproval(ctx, domains.ManualApprovalInput{
		WorkspaceID: manualWorkspace,
		SourceKey: "approval-t015-manual",
		DomainLimit: 5,
		StartsAt: downgradeAt.Add(-24 * time.Hour),
		ExpiresAt: manualExpires,
		GrantedBy: "admin-t015",
		SupportTicketID: "ticket-t015-manual",
		DecisionReason: "independent manual authority survives plan downgrade",
		CorrelationID: "corr-p06-t015-manual-approval",
	}); err != nil {
		return err
	}
	if _, err := store.ApplyNormalPlanDowngrade(ctx, domains.NormalPlanDowngradeInput{
		WorkspaceID: manualWorkspace,
		SourceKey: "subscription-business-t015-manual",
		DegradedAt: downgradeAt,
		DecisionReason: "normal_plan_downgrade_grace",
		CorrelationID: "corr-p06-t015-manual-downgrade",
	}); err != nil {
		return err
	}
	coexisting, err := store.ResolveEntitlement(ctx, manualWorkspace, downgradeAt)
	if err != nil {
		return err
	}
	if coexisting.Source != domains.SourceManualApproval || coexisting.DomainLimit != 5 || coexisting.GracePeriod || !coexisting.MutationAllowed || !coexisting.ExistingRoutingAllowed {
		return fmt.Errorf("valid manual source incorrectly entered grace: %+v", coexisting)
	}
	afterPlanGrace, err := store.ResolveEntitlement(ctx, manualWorkspace, expectedDeadline.Add(24*time.Hour))
	if err != nil {
		return err
	}
	if afterPlanGrace.Source != domains.SourceManualApproval || afterPlanGrace.DomainLimit != 5 || afterPlanGrace.GracePeriod || !afterPlanGrace.MutationAllowed {
		return fmt.Errorf("manual source did not remain authoritative after plan grace: %+v", afterPlanGrace)
	}

	out.Details = map[string]any{
		"downgrade_at": downgradeAt,
		"grace_until": expectedDeadline,
		"grace_duration_hours": expectedDeadline.Sub(downgradeAt).Hours(),
		"plan_expiry_equal_downgrade_at": true,
		"immediate_mutation_allowed": immediate.MutationAllowed,
		"immediate_existing_routing_allowed": immediate.ExistingRoutingAllowed,
		"immediate_ready_for_new_links": immediateReadiness.ReadyForNewLinks,
		"immediate_ready_for_routing": immediateReadiness.ReadyForRouting,
		"create_during_grace_denied": true,
		"activation_during_grace_denied": true,
		"restoration_during_grace_denied": true,
		"rotation_during_grace_denied": true,
		"new_link_assignment_during_grace_denied": true,
		"domain_rows_after_denials": domainRows,
		"allocated_count_after_denials": allocated,
		"ownership_token_version_unchanged": afterDeniedDomain.OwnershipTokenVersion == beforeToken,
		"last_grace_instant_routing_allowed": lastGrace.ExistingRoutingAllowed,
		"deadline_status": exactDeadline.Status,
		"deadline_routing_allowed": exactDeadline.ExistingRoutingAllowed,
		"deadline_ready_for_routing": deadlineReadiness.ReadyForRouting,
		"idempotent_replay_kept_deadline": true,
		"grace_extension_rejected": true,
		"downgrade_audit_events": auditCount,
		"manual_source_prevented_grace": !coexisting.GracePeriod,
		"manual_source_limit": coexisting.DomainLimit,
		"manual_source_authoritative_after_plan_grace": afterPlanGrace.Source == domains.SourceManualApproval,
	}
	return nil
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var value int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
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
