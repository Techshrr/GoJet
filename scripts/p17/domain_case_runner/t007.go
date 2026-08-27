package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT007(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL manual entitlement approve/deny through inherited P06 resolver with durable idempotent administrator/domain audit")
	now := time.Now().UTC().Truncate(time.Second)
	service, err := adminfixture.NewService(runtime, "t007", 100)
	if err != nil {
		return out, err
	}
	_, err = adminfixture.Bootstrap(ctx, service, rootEmail, rootPassword, []string{adminaccess.PermissionDomainsEntitlementsManage}, now)
	if err != nil {
		return out, err
	}
	root, login, _, err := adminfixture.LoginAndConfirmMFA(ctx, service, rootEmail, rootPassword, now.Add(time.Second))
	if err != nil {
		return out, err
	}
	for _, id := range []string{"ws-t007-approve", "ws-t007-deny"} {
		if err := seedWorkspace(ctx, runtime.DB, id, now); err != nil {
			return out, err
		}
	}
	store := domains.NewMySQLStore(runtime.DB)
	requested := uint32(4)
	_, err = store.ProjectAccessRequest(ctx, domains.AccessRequestInput{WorkspaceID: "ws-t007-approve", SupportTicketID: "ticket-t007-approve", RequestedDomainLimit: &requested, SubmittedAt: now, CorrelationID: "p17-t007-approve-request"})
	if err != nil {
		return out, err
	}
	_, err = store.ProjectAccessRequest(ctx, domains.AccessRequestInput{WorkspaceID: "ws-t007-deny", SupportTicketID: "ticket-t007-deny", RequestedDomainLimit: &requested, SubmittedAt: now, CorrelationID: "p17-t007-deny-request"})
	if err != nil {
		return out, err
	}

	server, err := newDomainHTTPServer(service)
	if err != nil {
		return out, err
	}
	defer server.Close()
	starts := now.Add(-time.Minute)
	expires := now.Add(30 * 24 * time.Hour)
	approveBody := map[string]any{
		"action": "approve", "domain_limit": 4, "starts_at": starts.Format(time.RFC3339Nano), "expires_at": expires.Format(time.RFC3339Nano),
		"support_ticket_id": "ticket-t007-approve", "reason": "approve bounded manual domain entitlement after reviewed support evidence",
	}
	approveHTTP, err := adminfixture.Request(ctx, server, "POST", "/api/admin/domain-entitlements/ws-t007-approve/decisions", adminfixture.AllowedOrigin, login.Token, login.CSRFToken, "p17-t007-approve-key", "p17-t007-approve", approveBody)
	if err != nil {
		return out, err
	}
	approveReplay, err := adminfixture.Request(ctx, server, "POST", "/api/admin/domain-entitlements/ws-t007-approve/decisions", adminfixture.AllowedOrigin, login.Token, login.CSRFToken, "p17-t007-approve-key", "p17-t007-approve", approveBody)
	if err != nil {
		return out, err
	}
	denyHTTP, err := adminfixture.Request(ctx, server, "POST", "/api/admin/domain-entitlements/ws-t007-deny/decisions", adminfixture.AllowedOrigin, login.Token, login.CSRFToken, "p17-t007-deny-key", "p17-t007-deny", map[string]any{
		"action": "deny", "user_visible_category": "policy_not_met", "reason": "deny request because required entitlement policy evidence is incomplete",
	})
	if err != nil {
		return out, err
	}

	approved, err := store.ResolveEntitlement(ctx, "ws-t007-approve", now.Add(time.Minute))
	if err != nil {
		return out, err
	}
	denied, err := store.ResolveEntitlement(ctx, "ws-t007-deny", now.Add(time.Minute))
	if err != nil {
		return out, err
	}
	manualRows, err := mustRows(ctx, runtime.DB, `SELECT COUNT(*) FROM custom_domain_entitlement_sources WHERE workspace_id='ws-t007-approve' AND source='manual_approval' AND status='active' AND domain_limit=4 AND support_ticket_id='ticket-t007-approve'`)
	if err != nil {
		return out, err
	}
	approvedRequestStatus, err := scalarString(ctx, runtime.DB, `SELECT status FROM custom_domain_entitlement_requests WHERE support_ticket_id='ticket-t007-approve'`)
	if err != nil {
		return out, err
	}
	deniedRequestStatus, err := scalarString(ctx, runtime.DB, `SELECT status FROM custom_domain_entitlement_requests WHERE support_ticket_id='ticket-t007-deny'`)
	if err != nil {
		return out, err
	}
	approveDecisions, err := mustRows(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_domain_entitlement_decisions WHERE workspace_id='ws-t007-approve' AND action='approve' AND request_correlation_id='p17-t007-approve'`)
	if err != nil {
		return out, err
	}
	denyDecisions, err := mustRows(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_domain_entitlement_decisions WHERE workspace_id='ws-t007-deny' AND action='deny' AND user_visible_category='policy_not_met'`)
	if err != nil {
		return out, err
	}
	adminAudits, err := mustRows(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_audit_events WHERE action IN ('admin.domain_entitlement.approve','admin.domain_entitlement.deny')`)
	if err != nil {
		return out, err
	}
	domainAudits, err := mustRows(ctx, runtime.DB, `SELECT COUNT(*) FROM custom_domain_audit_events WHERE action IN ('domain.entitlement.manual_approval.create','domain.entitlement.request.deny')`)
	if err != nil {
		return out, err
	}
	var beforeJSON, afterJSON string
	if err := runtime.DB.QueryRowContext(ctx, `SELECT CAST(before_json AS CHAR),CAST(after_json AS CHAR) FROM admin_domain_entitlement_decisions WHERE workspace_id='ws-t007-approve' AND action='approve' LIMIT 1`).Scan(&beforeJSON, &afterJSON); err != nil {
		return out, err
	}
	var replayed bool
	if value, ok := approveReplay.Body["replayed"].(bool); ok {
		replayed = value
	}
	if approveHTTP.Status != 200 || approveReplay.Status != 200 || denyHTTP.Status != 200 {
		return out, fmt.Errorf("unexpected decision statuses approve=%d replay=%d deny=%d", approveHTTP.Status, approveReplay.Status, denyHTTP.Status)
	}
	var parsedBefore, parsedAfter map[string]any
	_ = json.Unmarshal([]byte(beforeJSON), &parsedBefore)
	_ = json.Unmarshal([]byte(afterJSON), &parsedAfter)
	out.RecordCounts = map[string]int{"manual_sources": manualRows, "approve_decisions": approveDecisions, "deny_decisions": denyDecisions, "admin_audits": adminAudits, "domain_audits": domainAudits}
	out.Checks = map[string]bool{
		"approve_creates_only_structured_manual_approval":        manualRows == 1 && approved.Source == domains.SourceManualApproval && approved.Status == domains.EntitlementActive && approved.DomainLimit == 4 && approved.SupportTicketID == "ticket-t007-approve",
		"approve_closes_request_without_reinterpreting_resolver": approvedRequestStatus == "approved" && approved.MutationAllowed && approved.ExistingRoutingAllowed,
		"deny_grants_no_entitlement":                             deniedRequestStatus == "denied" && denied.Source == domains.SourceNone && denied.Status == domains.EntitlementExpired && !denied.MutationAllowed,
		"approve_and_deny_have_append_only_decisions":            approveDecisions == 1 && denyDecisions == 1 && len(parsedBefore) > 0 && len(parsedAfter) > 0,
		"admin_and_domain_audit_both_record_decisions":           adminAudits == 2 && domainAudits >= 2,
		"approve_replay_is_idempotent_without_duplicate_source":  replayed && manualRows == 1 && approveDecisions == 1,
	}
	pass(&out)
	_ = root
	return out, nil
}
