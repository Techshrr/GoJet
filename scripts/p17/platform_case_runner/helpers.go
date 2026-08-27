package main

import (
	"context"
	"database/sql"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func bootstrapCaseRoot(ctx context.Context, runtime *adminfixture.Runtime, caseTag string, permissions []string, now time.Time) (*adminaccess.Service, adminaccess.Principal, adminaccess.SessionSecret, error) {
	service, err := adminfixture.NewService(runtime, strings.ToLower(caseTag), 100)
	if err != nil {
		return nil, adminaccess.Principal{}, adminaccess.SessionSecret{}, err
	}
	all := append([]string{adminaccess.PermissionAdminsManage}, permissions...)
	email := strings.ToLower(caseTag) + "-root@p17.test"
	password := "P17-" + strings.ToLower(caseTag) + "-root-password-fixture"
	if _, err := adminfixture.Bootstrap(ctx, service, email, password, all, now); err != nil {
		return nil, adminaccess.Principal{}, adminaccess.SessionSecret{}, err
	}
	principal, login, _, err := adminfixture.LoginAndConfirmMFA(ctx, service, email, password, now.Add(time.Second))
	if err != nil {
		return nil, adminaccess.Principal{}, adminaccess.SessionSecret{}, err
	}
	return service, principal, login, nil
}

func createScopedMFAAdmin(ctx context.Context, service *adminaccess.Service, root adminaccess.Principal, caseTag, label, permission string, now time.Time) (adminaccess.Principal, adminaccess.SessionSecret, error) {
	email := strings.ToLower(caseTag) + "-" + label + "@p17.test"
	password := "P17-" + strings.ToLower(caseTag) + "-" + label + "-password-fixture"
	prefix := "p17-" + strings.ToLower(caseTag) + "-" + label
	role, _, err := service.CreateRole(ctx, root, adminaccess.CreateRoleInput{Name: "P17 " + caseTag + " " + label, Permissions: []string{permission}}, adminaccess.MutationAuthority{Reason: "create exact scoped administrator role fixture", CorrelationID: prefix + "-role", IdempotencyKey: prefix + "-role-key"}, now)
	if err != nil {
		return adminaccess.Principal{}, adminaccess.SessionSecret{}, err
	}
	if _, _, err := service.CreateAdministrator(ctx, root, adminaccess.CreateAdministratorInput{Email: email, DisplayName: label, Password: password, RoleIDs: []string{role.ID}}, adminaccess.MutationAuthority{Reason: "create exact scoped administrator fixture", CorrelationID: prefix + "-admin", IdempotencyKey: prefix + "-admin-key"}, now.Add(time.Second)); err != nil {
		return adminaccess.Principal{}, adminaccess.SessionSecret{}, err
	}
	principal, login, _, err := adminfixture.LoginAndConfirmMFA(ctx, service, email, password, now.Add(2*time.Second))
	return principal, login, err
}

func seedWorkspace(ctx context.Context, db *sql.DB, id, name, ownerID, ownerEmail, memberID, memberEmail string, now time.Time) error {
	if _, err := db.ExecContext(ctx, `INSERT INTO workspaces(id,name,status,version,created_by,created_at,updated_at) VALUES (?,?,'active',1,?,?,?)`, id, name, ownerID, now, now); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,email,display_name,role,joined_at,updated_at) VALUES (?,?,?,'Verified Owner','owner',?,?)`, id, ownerID, ownerEmail, now, now); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,email,display_name,role,joined_at,updated_at) VALUES (?,?,?,'Regular Member','member',?,?)`, id, memberID, memberEmail, now, now)
	return err
}
