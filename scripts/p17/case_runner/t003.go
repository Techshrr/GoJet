package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT003(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL 8.x administrator RBAC authority proving exact 16-permission catalog and non-transitive independent authorization decisions")
	now := time.Now().UTC().Truncate(time.Second)
	service, err := adminfixture.NewService(runtime, "t003", 100)
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
	type scoped struct {
		name       string
		permission string
		principal  adminaccess.Principal
	}
	var scopedPrincipals []scoped
	for i, item := range []struct{ name, permission string }{{"Tickets", adminaccess.PermissionTicketsManage}, {"Domains", adminaccess.PermissionDomainsManage}, {"Security", adminaccess.PermissionSecurityManage}} {
		role, _, createErr := service.CreateRole(ctx, root, adminaccess.CreateRoleInput{Name: item.name + " scoped", Permissions: []string{item.permission}}, adminaccess.MutationAuthority{Reason: "prove independent permission boundary", CorrelationID: fmt.Sprintf("p17-t003-role-%d", i), IdempotencyKey: fmt.Sprintf("p17-t003-role-key-%d", i)}, now.Add(time.Duration(10+i)*time.Second))
		if createErr != nil {
			return out, createErr
		}
		password := fmt.Sprintf("P17-t003-%s-password", strings.ToLower(item.name))
		child, _, createErr := service.CreateAdministrator(ctx, root, adminaccess.CreateAdministratorInput{Email: strings.ToLower(item.name) + "@p17.test", DisplayName: item.name, Password: password, RoleIDs: []string{role.ID}}, adminaccess.MutationAuthority{Reason: "bind one explicit scoped role", CorrelationID: fmt.Sprintf("p17-t003-admin-%d", i), IdempotencyKey: fmt.Sprintf("p17-t003-admin-key-%d", i)}, now.Add(time.Duration(20+i)*time.Second))
		if createErr != nil {
			return out, createErr
		}
		login, loginErr := service.Login(ctx, child.Email, password, "", fmt.Sprintf("p17-t003-login-%d", i), now.Add(time.Duration(30+i)*time.Second))
		if loginErr != nil {
			return out, loginErr
		}
		principal, authErr := service.Authenticate(ctx, login.Token, now.Add(time.Duration(40+i)*time.Second))
		if authErr != nil {
			return out, authErr
		}
		scopedPrincipals = append(scopedPrincipals, scoped{item.name, item.permission, principal})
	}
	var ticket, domains, security adminaccess.Principal
	for _, item := range scopedPrincipals {
		switch item.permission {
		case adminaccess.PermissionTicketsManage:
			ticket = item.principal
		case adminaccess.PermissionDomainsManage:
			domains = item.principal
		case adminaccess.PermissionSecurityManage:
			security = item.principal
		}
	}
	catalogCount, err := adminfixture.ScalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_permissions`)
	if err != nil {
		return out, err
	}
	wildcards, err := adminfixture.ScalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_permissions WHERE permission LIKE '%*%'`)
	if err != nil {
		return out, err
	}
	_, _, wildcardErr := service.CreateRole(ctx, root, adminaccess.CreateRoleInput{Name: "Invalid wildcard", Permissions: []string{"*"}}, adminaccess.MutationAuthority{Reason: "must fail", CorrelationID: "p17-t003-wildcard", IdempotencyKey: "p17-t003-wildcard-key"}, now.Add(50*time.Second))
	out.RecordCounts = map[string]int{"permission_catalog": catalogCount, "wildcard_permissions": wildcards, "scoped_principals": len(scopedPrincipals)}
	out.Checks = map[string]bool{
		"catalog_is_exactly_16_permissions":                    catalogCount == 16 && len(adminaccess.PermissionCatalog) == 16,
		"wildcard_and_superuser_authority_are_invalid":         wildcards == 0 && !adminaccess.ValidPermission("*") && !adminaccess.ValidPermission("superuser") && errors.Is(wildcardErr, adminaccess.ErrInvalid),
		"tickets_manage_is_server_authorized_for_ticket_actor": service.Require(ticket, adminaccess.PermissionTicketsManage) == nil,
		"tickets_manage_never_implies_domain_entitlement":      errors.Is(service.Require(ticket, adminaccess.PermissionDomainsEntitlementsManage), adminaccess.ErrForbidden),
		"domains_manage_never_implies_domain_risk":             errors.Is(service.Require(domains, adminaccess.PermissionDomainsRiskManage), adminaccess.ErrForbidden),
		"domains_manage_never_implies_domain_entitlement":      errors.Is(service.Require(domains, adminaccess.PermissionDomainsEntitlementsManage), adminaccess.ErrForbidden),
		"security_manage_never_implies_admins_manage":          errors.Is(service.Require(security, adminaccess.PermissionAdminsManage), adminaccess.ErrForbidden),
		"security_manage_never_implies_operations_manage":      errors.Is(service.Require(security, adminaccess.PermissionOperationsManage), adminaccess.ErrForbidden),
		"security_manage_never_implies_domain_entitlement":     errors.Is(service.Require(security, adminaccess.PermissionDomainsEntitlementsManage), adminaccess.ErrForbidden),
	}
	pass(&out)
	return out, nil
}
