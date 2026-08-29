package main

import (
	"context"
	"fmt"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT001(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL 8.x and Redis 7.x using production administrator service proving durable identities, roles, explicit permissions, audit and separation from customer Workspace RBAC")
	now := time.Now().UTC().Truncate(time.Second)
	service, err := adminfixture.NewService(runtime, "t001", 100)
	if err != nil {
		return out, err
	}
	_, err = adminfixture.Bootstrap(ctx, service, rootEmail, rootPassword, []string{adminaccess.PermissionAdminsManage, adminaccess.PermissionPlatformRead}, now)
	if err != nil {
		return out, err
	}
	root, _, _, err := adminfixture.LoginAndConfirmMFA(ctx, service, rootEmail, rootPassword, now.Add(time.Second))
	if err != nil {
		return out, err
	}
	role, replay, err := service.CreateRole(ctx, root, adminaccess.CreateRoleInput{Name: "Ticket operators", Description: "P17 independent ticket permission", Permissions: []string{adminaccess.PermissionTicketsManage}}, adminaccess.MutationAuthority{Reason: "provision dedicated ticket operator role", CorrelationID: "p17-t001-role", IdempotencyKey: "p17-t001-role-key"}, now.Add(10*time.Second))
	if err != nil || replay {
		return out, fmt.Errorf("create role: replay=%v err=%w", replay, err)
	}
	childPassword := "P17-t001-child-password"
	child, replay, err := service.CreateAdministrator(ctx, root, adminaccess.CreateAdministratorInput{Email: "ticket-admin@p17.test", DisplayName: "Ticket Admin", Password: childPassword, RoleIDs: []string{role.ID}}, adminaccess.MutationAuthority{Reason: "provision scoped ticket administrator", CorrelationID: "p17-t001-admin", IdempotencyKey: "p17-t001-admin-key"}, now.Add(11*time.Second))
	if err != nil || replay {
		return out, fmt.Errorf("create administrator: replay=%v err=%w", replay, err)
	}
	childLogin, err := service.Login(ctx, child.Email, childPassword, "", "p17-t001-child-login", now.Add(12*time.Second))
	if err != nil {
		return out, err
	}
	childPrincipal, err := service.Authenticate(ctx, childLogin.Token, now.Add(13*time.Second))
	if err != nil {
		return out, err
	}
	service2, err := adminfixture.NewService(runtime, "t001", 100)
	if err != nil {
		return out, err
	}
	durablePrincipal, err := service2.Authenticate(ctx, childLogin.Token, now.Add(14*time.Second))
	if err != nil {
		return out, err
	}
	adminCount, err := adminfixture.ScalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_administrators`)
	if err != nil {
		return out, err
	}
	roleCount, err := adminfixture.ScalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_roles`)
	if err != nil {
		return out, err
	}
	assignmentCount, err := adminfixture.ScalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_role_assignments`)
	if err != nil {
		return out, err
	}
	auditCount, err := adminfixture.ScalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_audit_events WHERE action IN ('admin.bootstrap','admin.role.create','admin.administrator.create')`)
	if err != nil {
		return out, err
	}
	separateTables, err := adminfixture.ScalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name IN ('auth_users','admin_administrators')`)
	if err != nil {
		return out, err
	}
	out.RecordCounts = map[string]int{"administrators": adminCount, "roles": roleCount, "role_assignments": assignmentCount, "governance_audit_events": auditCount}
	out.Checks = map[string]bool{
		"administrator_identity_is_durable":                       adminCount == 2 && durablePrincipal.Administrator.ID == child.ID,
		"role_and_assignment_are_durable":                         roleCount == 2 && assignmentCount == 2,
		"permission_is_loaded_server_side_from_durable_role":      childPrincipal.Has(adminaccess.PermissionTicketsManage) && durablePrincipal.Has(adminaccess.PermissionTicketsManage),
		"ticket_permission_does_not_gain_entitlement_authority":   !durablePrincipal.Has(adminaccess.PermissionDomainsEntitlementsManage),
		"ticket_permission_does_not_gain_admins_authority":        !durablePrincipal.Has(adminaccess.PermissionAdminsManage),
		"administrator_governance_is_separate_from_customer_auth": separateTables == 2,
		"identity_role_mutations_are_audited":                     auditCount >= 3,
	}
	pass(&out)
	return out, nil
}
