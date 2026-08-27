package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT006(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL P06/P13/P14 entitlement queue/detail with dedicated domains.entitlements.manage authorization")
	now := time.Now().UTC().Truncate(time.Second)
	service, err := adminfixture.NewService(runtime, "t006", 100)
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
	if err := seedWorkspace(ctx, runtime.DB, "ws-t006", now); err != nil {
		return out, err
	}
	store := domains.NewMySQLStore(runtime.DB)
	_, err = store.UpsertPlanSource(ctx, domains.PlanSourceInput{WorkspaceID: "ws-t006", SourceKey: "billing-plan-t006", Status: domains.EntitlementActive, DomainLimit: 3, StartsAt: now.Add(-time.Hour), DecisionReason: "billing plan active"}, "p17-t006-plan")
	if err != nil {
		return out, err
	}
	requested := uint32(5)
	_, err = store.ProjectAccessRequest(ctx, domains.AccessRequestInput{WorkspaceID: "ws-t006", SupportTicketID: "ticket-t006", RequestedDomainLimit: &requested, SubmittedAt: now, CorrelationID: "p17-t006-request"})
	if err != nil {
		return out, err
	}

	items, err := service.ListDomainEntitlements(ctx, root, now.Add(10*time.Second))
	if err != nil {
		return out, err
	}
	detail, err := service.GetDomainEntitlement(ctx, root, "ws-t006", now.Add(10*time.Second))
	if err != nil {
		return out, err
	}
	ticketsLogin, err := createTicketsOnlyAdmin(ctx, service, root, now.Add(20*time.Second))
	if err != nil {
		return out, err
	}
	ticketsPrincipal, err := service.Authenticate(ctx, ticketsLogin.Token, now.Add(23*time.Second))
	if err != nil {
		return out, err
	}
	_, deniedErr := service.ListDomainEntitlements(ctx, ticketsPrincipal, now.Add(24*time.Second))

	server, err := newDomainHTTPServer(service)
	if err != nil {
		return out, err
	}
	defer server.Close()
	listHTTP, err := adminfixture.Request(ctx, server, "GET", "/api/admin/domain-entitlements", "", login.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	detailHTTP, err := adminfixture.Request(ctx, server, "GET", "/api/admin/domain-entitlements/ws-t006", "", login.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	ticketsHTTP, err := adminfixture.Request(ctx, server, "GET", "/api/admin/domain-entitlements", "", ticketsLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}

	if len(items) != 1 {
		return out, fmt.Errorf("expected one entitlement queue item, got %d", len(items))
	}
	out.RecordCounts["queue_items"] = len(items)
	out.RecordCounts["valid_sources"] = len(detail.Entitlement.ValidSources)
	out.Checks = map[string]bool{
		"queue_consumes_p06_plan_authority":                  items[0].WorkspaceID == "ws-t006" && items[0].Entitlement.Source == domains.SourcePlan && items[0].Entitlement.DomainLimit == 3,
		"detail_preserves_p14_request_without_grant":         detail.Request != nil && detail.Request.SupportTicketID == "ticket-t006" && detail.Request.Status == "requested" && detail.Entitlement.Status == domains.EntitlementActive,
		"dedicated_permission_allows_queue_and_detail":       listHTTP.Status == 200 && detailHTTP.Status == 200,
		"admin_surface_is_no_store_noindex":                  adminfixture.NoStoreNoIndex(listHTTP) && adminfixture.NoStoreNoIndex(detailHTTP),
		"tickets_manage_does_not_imply_entitlement_manage":   errors.Is(deniedErr, adminaccess.ErrForbidden) && ticketsHTTP.Status == 403,
		"resolver_source_and_limit_are_server_authoritative": detail.Entitlement.MutationAllowed && detail.Entitlement.ExistingRoutingAllowed && detail.Entitlement.DomainLimit == 3,
	}
	pass(&out)
	return out, nil
}
