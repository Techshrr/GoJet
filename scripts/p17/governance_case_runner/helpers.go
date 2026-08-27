package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func seedUser(ctx context.Context, db *sql.DB, id, email, displayName string, verified bool, now time.Time) error {
	var verifiedAt any
	status := "pending_verification"
	if verified {
		verifiedAt = now
		status = "active"
	}
	_, err := db.ExecContext(ctx, `INSERT INTO auth_users(id,email,email_normalized,display_name,status,email_verified_at,version,created_at,updated_at) VALUES (?,?,?,?,?,?,1,?,?)`, id, email, strings.ToLower(email), displayName, status, verifiedAt, now, now)
	return err
}

func seedCustomerSession(ctx context.Context, db *sql.DB, userID string, now time.Time) error {
	_, err := db.ExecContext(ctx, `INSERT INTO auth_sessions(id,user_id,token_hash,csrf_secret_hash,status,expires_at,last_seen_at,correlation_id,created_at,updated_at) VALUES ('ses_p17_t010_verified',?,UNHEX(REPEAT('31',32)),UNHEX(REPEAT('32',32)),'active',?,?, 'p17-t010-customer-session',?,?)`, userID, now.Add(time.Hour), now, now, now)
	return err
}

func seedWorkspace(ctx context.Context, db *sql.DB, id, name, ownerID, ownerEmail, memberID, memberEmail string, now time.Time) error {
	if _, err := db.ExecContext(ctx, `INSERT INTO workspaces(id,name,status,version,created_by,created_at,updated_at) VALUES (?,?,'active',1,?,?,?)`, id, name, ownerID, now, now); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,email,display_name,role,joined_at,updated_at) VALUES (?,?,?,'Verified Owner','owner',?,?)`, id, ownerID, ownerEmail, now, now); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,email,display_name,role,joined_at,updated_at) VALUES (?,?,?,'Pending Member','member',?,?)`, id, memberID, memberEmail, now, now)
	return err
}

func createScopedAdmin(ctx context.Context, service *adminaccess.Service, root adminaccess.Principal, label, email, password, permission string, now time.Time) (adminaccess.SessionSecret, error) {
	role, _, err := service.CreateRole(ctx, root, adminaccess.CreateRoleInput{Name: "P17 " + label, Permissions: []string{permission}}, adminaccess.MutationAuthority{Reason: "create exact permission governance fixture", CorrelationID: "p17-t010-" + label + "-role", IdempotencyKey: "p17-t010-" + label + "-role-key"}, now)
	if err != nil {
		return adminaccess.SessionSecret{}, err
	}
	_, _, err = service.CreateAdministrator(ctx, root, adminaccess.CreateAdministratorInput{Email: email, DisplayName: label, Password: password, RoleIDs: []string{role.ID}}, adminaccess.MutationAuthority{Reason: "create exact permission administrator fixture", CorrelationID: "p17-t010-" + label + "-admin", IdempotencyKey: "p17-t010-" + label + "-admin-key"}, now.Add(time.Second))
	if err != nil {
		return adminaccess.SessionSecret{}, err
	}
	return service.Login(ctx, email, password, "", "p17-t010-"+label+"-login", now.Add(2*time.Second))
}

func scalarString(ctx context.Context, db *sql.DB, query string, args ...any) (string, error) {
	var value string
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return "", err
	}
	return value, nil
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var value int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

func auditCount(ctx context.Context, db *sql.DB, action, resourceID string) (int, error) {
	return scalarInt(ctx, db, `SELECT COUNT(*) FROM admin_audit_events WHERE action=? AND resource_id=?`, action, resourceID)
}

func auditAccountable(ctx context.Context, db *sql.DB, action, resourceID, correlation string) (bool, error) {
	var reason string
	var before, after string
	err := db.QueryRowContext(ctx, `SELECT COALESCE(reason,''),CAST(before_json AS CHAR),CAST(after_json AS CHAR) FROM admin_audit_events WHERE action=? AND resource_id=? AND request_correlation_id=? ORDER BY id DESC LIMIT 1`, action, resourceID, correlation).Scan(&reason, &before, &after)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(reason) != "" && before != "{}" && after != "{}", nil
}

func safeEnumeration(raw string) bool {
	lower := strings.ToLower(raw)
	for _, forbidden := range []string{"password_hash", "password_algorithm", "token_hash", "csrf", "secret", "email_normalized", "oauth", "credential", "locked_until"} {
		if strings.Contains(lower, forbidden) {
			return false
		}
	}
	return true
}

func mustStatus(label string, got, want int) error {
	if got != want {
		return fmt.Errorf("%s status=%d want=%d", label, got, want)
	}
	return nil
}

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

type deterministicOpsProbe struct {
	states map[string]map[string]bool
}

func (p deterministicOpsProbe) Probe(_ context.Context, serviceID string) map[string]bool {
	if p.states != nil {
		if state, ok := p.states[serviceID]; ok {
			out := make(map[string]bool, len(state))
			for k, v := range state {
				out[k] = v
			}
			return out
		}
	}
	return map[string]bool{"unit": true}
}

type recordingRestarter struct {
	calls []string
	err   error
}

func (r *recordingRestarter) Restart(_ context.Context, serviceID string) error {
	r.calls = append(r.calls, serviceID)
	return r.err
}
