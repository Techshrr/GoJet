package main

import (
	"context"
	"fmt"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT008(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL suspend/revoke/restore enforcement with immediate routing impact, P16 safety block, tickets.manage denial and immutable decision ledger")
	now := time.Now().UTC().Truncate(time.Second)
	service, err := adminfixture.NewService(runtime, "t008", 100)
	if err != nil {
		return out, err
	}
	_, err = adminfixture.Bootstrap(ctx, service, rootEmail, rootPassword, []string{adminaccess.PermissionAdminsManage, adminaccess.PermissionDomainsEntitlementsManage}, now)
	if err != nil {
		return out, err
	}
	root, login, _, err := adminfixture.LoginAndConfirmMFA(ctx, service, rootEmail, rootPassword, now.Add(time.Second))
	if err != nil {
		return out, err
	}
	if err := seedWorkspace(ctx, runtime.DB, "ws-t008", now); err != nil {
		return out, err
	}
	store := domains.NewMySQLStore(runtime.DB)
	_, err = store.UpsertPlanSource(ctx, domains.PlanSourceInput{WorkspaceID: "ws-t008", SourceKey: "billing-plan-t008", Status: domains.EntitlementActive, DomainLimit: 2, StartsAt: now.Add(-time.Hour), DecisionReason: "active billing plan"}, "p17-t008-plan")
	if err != nil {
		return out, err
	}
	domainID, err := seedReadyDomain(ctx, runtime.DB, "ws-t008", "t008.example.com", now)
	if err != nil {
		return out, err
	}
	ticketsLogin, err := createTicketsOnlyAdmin(ctx, service, root, now.Add(10*time.Second))
	if err != nil {
		return out, err
	}

	server, err := newDomainHTTPServer(service)
	if err != nil {
		return out, err
	}
	defer server.Close()
	suspendBody := map[string]any{"action": "suspend", "effective_at": now.Add(-time.Second).Format(time.RFC3339Nano), "scope": "workspace_entitlement", "reason": "suspend entitlement immediately for accountable review"}
	suspendHTTP, err := adminfixture.Request(ctx, server, "POST", "/api/admin/domain-entitlements/ws-t008/decisions", adminfixture.AllowedOrigin, login.Token, login.CSRFToken, "p17-t008-suspend-key", "p17-t008-suspend", suspendBody)
	if err != nil {
		return out, err
	}
	suspended, err := store.ResolveEntitlement(ctx, "ws-t008", now.Add(time.Second))
	if err != nil {
		return out, err
	}
	routingAfterSuspend, err := scalarString(ctx, runtime.DB, `SELECT routing_state FROM custom_domains WHERE id=?`, domainID)
	if err != nil {
		return out, err
	}

	if _, err := runtime.DB.ExecContext(ctx, `UPDATE custom_domains SET security_category='p16_abuse_block' WHERE id=?`, domainID); err != nil {
		return out, err
	}
	restoreBlockedHTTP, err := adminfixture.Request(ctx, server, "POST", "/api/admin/domain-entitlements/ws-t008/decisions", adminfixture.AllowedOrigin, login.Token, login.CSRFToken, "p17-t008-restore-blocked-key", "p17-t008-restore-blocked", map[string]any{"action": "restore", "current_security_ownership_evidence": "review:P16-current-safe-check", "reason": "attempt restore while inherited safety is still active"})
	if err != nil {
		return out, err
	}
	controlAfterBlocked, err := scalarString(ctx, runtime.DB, `SELECT state FROM admin_domain_entitlement_controls WHERE workspace_id='ws-t008'`)
	if err != nil {
		return out, err
	}
	if _, err := runtime.DB.ExecContext(ctx, `UPDATE custom_domains SET security_category=NULL WHERE id=?`, domainID); err != nil {
		return out, err
	}
	restoreHTTP, err := adminfixture.Request(ctx, server, "POST", "/api/admin/domain-entitlements/ws-t008/decisions", adminfixture.AllowedOrigin, login.Token, login.CSRFToken, "p17-t008-restore-key", "p17-t008-restore", map[string]any{"action": "restore", "current_security_ownership_evidence": "review:P16-current-safe-check-cleared", "reason": "restore entitlement after current safety evidence is clear"})
	if err != nil {
		return out, err
	}
	restored, err := store.ResolveEntitlement(ctx, "ws-t008", now.Add(2*time.Second))
	if err != nil {
		return out, err
	}
	routingAfterRestore, err := scalarString(ctx, runtime.DB, `SELECT routing_state FROM custom_domains WHERE id=?`, domainID)
	if err != nil {
		return out, err
	}
	if _, err := runtime.DB.ExecContext(ctx, `UPDATE custom_domains SET routing_state='enabled' WHERE id=?`, domainID); err != nil {
		return out, err
	}

	invalidRevokeHTTP, err := adminfixture.Request(ctx, server, "POST", "/api/admin/domain-entitlements/ws-t008/decisions", adminfixture.AllowedOrigin, login.Token, login.CSRFToken, "p17-t008-revoke-invalid-key", "p17-t008-revoke-invalid", map[string]any{"action": "revoke", "confirmation": "yes", "existing_link_impact": "keep_existing", "reason": "invalid destructive confirmation fixture"})
	if err != nil {
		return out, err
	}
	revokeHTTP, err := adminfixture.Request(ctx, server, "POST", "/api/admin/domain-entitlements/ws-t008/decisions", adminfixture.AllowedOrigin, login.Token, login.CSRFToken, "p17-t008-revoke-key", "p17-t008-revoke", map[string]any{"action": "revoke", "confirmation": "REVOKE", "existing_link_impact": "disable_existing_routing", "reason": "revoke entitlement and disable existing routing immediately"})
	if err != nil {
		return out, err
	}
	revoked, err := store.ResolveEntitlement(ctx, "ws-t008", now.Add(3*time.Second))
	if err != nil {
		return out, err
	}
	routingAfterRevoke, err := scalarString(ctx, runtime.DB, `SELECT routing_state FROM custom_domains WHERE id=?`, domainID)
	if err != nil {
		return out, err
	}

	ticketsHTTP, err := adminfixture.Request(ctx, server, "POST", "/api/admin/domain-entitlements/ws-t008/decisions", adminfixture.AllowedOrigin, ticketsLogin.Token, ticketsLogin.CSRFToken, "p17-t008-tickets-key", "p17-t008-tickets", map[string]any{"action": "restore", "current_security_ownership_evidence": "review:ticket-manager-has-no-authority", "reason": "ticket manager must not restore entitlement"})
	if err != nil {
		return out, err
	}
	_, updateErr := runtime.DB.ExecContext(ctx, `UPDATE admin_domain_entitlement_decisions SET reason='tampered' WHERE workspace_id='ws-t008' LIMIT 1`)
	_, deleteErr := runtime.DB.ExecContext(ctx, `DELETE FROM admin_domain_entitlement_decisions WHERE workspace_id='ws-t008' LIMIT 1`)
	decisionRows, err := mustRows(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_domain_entitlement_decisions WHERE workspace_id='ws-t008'`)
	if err != nil {
		return out, err
	}
	controlRows, err := mustRows(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_domain_entitlement_controls WHERE workspace_id='ws-t008' AND state='revoked'`)
	if err != nil {
		return out, err
	}

	if suspendHTTP.Status != 200 || restoreHTTP.Status != 200 || revokeHTTP.Status != 200 {
		return out, fmt.Errorf("unexpected success statuses suspend=%d restore=%d revoke=%d", suspendHTTP.Status, restoreHTTP.Status, revokeHTTP.Status)
	}
	out.RecordCounts = map[string]int{"decision_rows": decisionRows, "active_revoke_controls": controlRows}
	out.Checks = map[string]bool{
		"suspend_is_immediate_for_mutation_and_existing_routing":     suspended.Status == domains.EntitlementSuspended && !suspended.MutationAllowed && !suspended.ExistingRoutingAllowed && routingAfterSuspend == "suspended",
		"restore_is_blocked_by_inherited_p16_safety":                 restoreBlockedHTTP.Status == 409 && controlAfterBlocked == "suspended",
		"restore_recovers_entitlement_but_not_routing_automatically": restored.Status == domains.EntitlementActive && restored.MutationAllowed && routingAfterRestore == "suspended",
		"revoke_requires_exact_confirmation_and_impact":              invalidRevokeHTTP.Status == 400 && revokeHTTP.Status == 200,
		"revoke_is_immediate_and_disables_existing_routing":          revoked.Status == domains.EntitlementRevoked && !revoked.MutationAllowed && !revoked.ExistingRoutingAllowed && routingAfterRevoke == "suspended" && controlRows == 1,
		"tickets_manage_alone_never_decides_entitlement":             ticketsHTTP.Status == 403,
		"decision_ledger_rejects_update_and_delete":                  updateErr != nil && deleteErr != nil && decisionRows == 3,
	}
	pass(&out)
	return out, nil
}
