package main

import (
	"context"
	"errors"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT009(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real P06/P13 resolver source precedence/domain_limit/seven-day grace preserved across P17 control, with P16 safety remaining conjunctive")
	now := time.Now().UTC().Truncate(time.Second)
	service, err := adminfixture.NewService(runtime, "t009", 100)
	if err != nil {
		return out, err
	}
	_, err = adminfixture.Bootstrap(ctx, service, rootEmail, rootPassword, []string{adminaccess.PermissionDomainsEntitlementsManage}, now)
	if err != nil {
		return out, err
	}
	root, _, _, err := adminfixture.LoginAndConfirmMFA(ctx, service, rootEmail, rootPassword, now.Add(time.Second))
	if err != nil {
		return out, err
	}
	for _, id := range []string{"ws-t009-tie", "ws-t009-limit", "ws-t009-grace", "ws-t009-safety"} {
		if err := seedWorkspace(ctx, runtime.DB, id, now); err != nil {
			return out, err
		}
	}
	store := domains.NewMySQLStore(runtime.DB)

	// P06 tie precedence: equal domain_limit keeps plan ahead of manual approval.
	_, err = store.UpsertPlanSource(ctx, domains.PlanSourceInput{WorkspaceID: "ws-t009-tie", SourceKey: "plan-tie", Status: domains.EntitlementActive, DomainLimit: 5, StartsAt: now.Add(-2 * time.Hour)}, "p17-t009-plan-tie")
	if err != nil {
		return out, err
	}
	_, err = store.CreateManualApproval(ctx, domains.ManualApprovalInput{WorkspaceID: "ws-t009-tie", SourceKey: "manual-tie", DomainLimit: 5, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), GrantedBy: "fixture-admin", SupportTicketID: "ticket-t009-tie", DecisionReason: "manual tie fixture", CorrelationID: "p17-t009-manual-tie"})
	if err != nil {
		return out, err
	}
	tieBefore, err := store.ResolveEntitlement(ctx, "ws-t009-tie", now)
	if err != nil {
		return out, err
	}

	// P06 domain_limit authority: the highest valid limit wins across sources.
	_, err = store.UpsertPlanSource(ctx, domains.PlanSourceInput{WorkspaceID: "ws-t009-limit", SourceKey: "plan-limit", Status: domains.EntitlementActive, DomainLimit: 2, StartsAt: now.Add(-2 * time.Hour)}, "p17-t009-plan-limit")
	if err != nil {
		return out, err
	}
	_, err = store.CreateManualApproval(ctx, domains.ManualApprovalInput{WorkspaceID: "ws-t009-limit", SourceKey: "manual-limit", DomainLimit: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), GrantedBy: "fixture-admin", SupportTicketID: "ticket-t009-limit", DecisionReason: "higher manual limit fixture", CorrelationID: "p17-t009-manual-limit"})
	if err != nil {
		return out, err
	}
	limitResolved, err := store.ResolveEntitlement(ctx, "ws-t009-limit", now)
	if err != nil {
		return out, err
	}

	// P06 normal downgrade grace remains exactly seven days and keeps existing
	// routing only, even if ordinary plan expiry is already behind now.
	degradedAt := now.Add(-2 * time.Hour)
	graceUntil := degradedAt.Add(domains.NormalDowngradeGrace)
	expiresAt := now.Add(-time.Hour)
	_, err = store.UpsertPlanSource(ctx, domains.PlanSourceInput{WorkspaceID: "ws-t009-grace", SourceKey: "plan-grace", Status: domains.EntitlementActive, DomainLimit: 3, StartsAt: now.Add(-48 * time.Hour), ExpiresAt: &expiresAt, DegradedAt: &degradedAt, GraceUntil: &graceUntil, DecisionReason: "normal downgrade fixture"}, "p17-t009-plan-grace")
	if err != nil {
		return out, err
	}
	graceResolved, err := store.ResolveEntitlement(ctx, "ws-t009-grace", now)
	if err != nil {
		return out, err
	}
	graceExpired, err := store.ResolveEntitlement(ctx, "ws-t009-grace", graceUntil)
	if err != nil {
		return out, err
	}

	// P17 control round-trip must reveal the identical P06/P13 result rather than
	// replacing source precedence or domain_limit with a new resolver.
	effective := now
	_, _, err = service.DecideDomainEntitlement(ctx, root, "ws-t009-tie", adminaccess.DomainEntitlementDecisionInput{Action: "suspend", EffectiveAt: &effective, Scope: "workspace_entitlement"}, adminaccess.MutationAuthority{Reason: "temporary governance suspension for resolver preservation fixture", CorrelationID: "p17-t009-suspend", IdempotencyKey: "p17-t009-suspend-key"}, now.Add(10*time.Second))
	if err != nil {
		return out, err
	}
	suspendedTie, err := store.ResolveEntitlement(ctx, "ws-t009-tie", now.Add(11*time.Second))
	if err != nil {
		return out, err
	}
	activeSourceRowsDuringControl, err := mustRows(ctx, runtime.DB, `SELECT COUNT(*) FROM custom_domain_entitlement_sources WHERE workspace_id='ws-t009-tie' AND status='active'`)
	if err != nil {
		return out, err
	}
	_, _, err = service.DecideDomainEntitlement(ctx, root, "ws-t009-tie", adminaccess.DomainEntitlementDecisionInput{Action: "restore", CurrentSecurityOwnershipEvidence: "review:P16-clear-t009"}, adminaccess.MutationAuthority{Reason: "restore after current safety evidence remains clear", CorrelationID: "p17-t009-restore", IdempotencyKey: "p17-t009-restore-key"}, now.Add(12*time.Second))
	if err != nil {
		return out, err
	}
	tieAfter, err := store.ResolveEntitlement(ctx, "ws-t009-tie", now.Add(13*time.Second))
	if err != nil {
		return out, err
	}

	// P16 safety remains a separate conjunctive authority after entitlement is valid.
	_, err = store.UpsertPlanSource(ctx, domains.PlanSourceInput{WorkspaceID: "ws-t009-safety", SourceKey: "plan-safety", Status: domains.EntitlementActive, DomainLimit: 2, StartsAt: now.Add(-time.Hour)}, "p17-t009-plan-safety")
	if err != nil {
		return out, err
	}
	domainID, err := seedReadyDomain(ctx, runtime.DB, "ws-t009-safety", "t009.example.com", now)
	if err != nil {
		return out, err
	}
	if _, err := runtime.DB.ExecContext(ctx, `UPDATE custom_domains SET security_category='p16_domain_risk_block' WHERE id=?`, domainID); err != nil {
		return out, err
	}
	_, safetyErr := store.CheckDomainMutationAuthority(ctx, "ws-t009-safety", domainID, domains.DomainMutationRestore, now.Add(time.Second))

	controlRows, err := mustRows(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_domain_entitlement_controls`)
	if err != nil {
		return out, err
	}
	out.RecordCounts = map[string]int{"remaining_control_rows": controlRows, "tie_valid_sources": len(tieAfter.ValidSources)}
	out.Checks = map[string]bool{
		"equal_limit_source_precedence_remains_plan":           tieBefore.Source == domains.SourcePlan && tieBefore.DomainLimit == 5,
		"highest_domain_limit_remains_authoritative":           limitResolved.Source == domains.SourceManualApproval && limitResolved.DomainLimit == 7,
		"seven_day_grace_keeps_existing_routing_only":          domains.NormalDowngradeGrace == 7*24*time.Hour && graceResolved.GracePeriod && !graceResolved.MutationAllowed && graceResolved.ExistingRoutingAllowed && graceResolved.GraceUntil != nil && graceResolved.GraceUntil.Equal(graceUntil),
		"grace_stops_at_exact_deadline":                        !graceExpired.GracePeriod && !graceExpired.MutationAllowed && !graceExpired.ExistingRoutingAllowed && graceExpired.Status == domains.EntitlementExpired,
		"p17_suspend_is_conjunctive_without_rewriting_sources": suspendedTie.Status == domains.EntitlementSuspended && !suspendedTie.MutationAllowed && !suspendedTie.ExistingRoutingAllowed && activeSourceRowsDuringControl == 2,
		"p17_restore_reveals_unchanged_source_and_limit":       tieAfter.Source == tieBefore.Source && tieAfter.DomainLimit == tieBefore.DomainLimit && tieAfter.Status == tieBefore.Status && controlRows == 0,
		"p16_safety_remains_independent_and_conjunctive":       errors.Is(safetyErr, domains.ErrDomainSecuritySuspended),
	}
	pass(&out)
	return out, nil
}
