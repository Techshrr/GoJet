package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
)

func seedWorkspace(ctx context.Context, db *sql.DB, id string, now time.Time) error {
	_, err := db.ExecContext(ctx, `INSERT INTO workspaces(id,name,status,version,created_by,created_at,updated_at) VALUES (?,?,'active',1,'p17-fixture',?,?)`, id, "P17 "+id, now, now)
	return err
}

func seedReadyDomain(ctx context.Context, db *sql.DB, workspaceID, hostname string, now time.Time) (uint64, error) {
	result, err := db.ExecContext(ctx, `
		INSERT INTO custom_domains(
			workspace_id,hostname_ascii,display_hostname,routing_state,
			ownership_status,ingress_dns_status,https_status,risk_status,
			ownership_token_version,ownership_secret_hash,ownership_secret_issued_at,
			ownership_verified_at,ingress_dns_checked_at,https_checked_at,risk_checked_at,risk_policy_version
		) VALUES (?, ?, ?, 'enabled','verified','valid','active','allow',1,UNHEX(REPEAT('11',32)),?,?,?,?,?,'p17-fixture')`,
		workspaceID, hostname, hostname, now, now, now, now, now)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), err
}

func createTicketsOnlyAdmin(ctx context.Context, service *adminaccess.Service, root adminaccess.Principal, now time.Time) (adminaccess.SessionSecret, error) {
	role, _, err := service.CreateRole(ctx, root, adminaccess.CreateRoleInput{Name: "P17 Tickets Only", Permissions: []string{adminaccess.PermissionTicketsManage}}, adminaccess.MutationAuthority{Reason: "create independent tickets-only authority fixture", CorrelationID: "p17-domain-tickets-role", IdempotencyKey: "p17-domain-tickets-role-key"}, now)
	if err != nil {
		return adminaccess.SessionSecret{}, err
	}
	password := "P17-tickets-only-password-fixture"
	_, _, err = service.CreateAdministrator(ctx, root, adminaccess.CreateAdministratorInput{Email: "tickets-only@p17.test", DisplayName: "Tickets Only", Password: password, RoleIDs: []string{role.ID}}, adminaccess.MutationAuthority{Reason: "create tickets-only administrator fixture", CorrelationID: "p17-domain-tickets-admin", IdempotencyKey: "p17-domain-tickets-admin-key"}, now.Add(time.Second))
	if err != nil {
		return adminaccess.SessionSecret{}, err
	}
	login, err := service.Login(ctx, "tickets-only@p17.test", password, "", "p17-domain-tickets-login", now.Add(2*time.Second))
	if err != nil {
		return adminaccess.SessionSecret{}, err
	}
	return login, nil
}

func scalarString(ctx context.Context, db *sql.DB, query string, args ...any) (string, error) {
	var value string
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return "", err
	}
	return value, nil
}

func mustRows(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var n int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count rows: %w", err)
	}
	return n, nil
}
