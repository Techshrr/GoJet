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

func runT004(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL 8.x and production administrator session authority proving actor-bound MFA/session listing, replay-safe revoke, immediate authority loss and immutable audit")
	now := time.Now().UTC().Truncate(time.Second)
	service, err := adminfixture.NewService(runtime, "t004", 100)
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
	role, _, err := service.CreateRole(ctx, root, adminaccess.CreateRoleInput{Name: "Read only", Permissions: []string{adminaccess.PermissionPlatformRead}}, adminaccess.MutationAuthority{Reason: "create scoped session subject", CorrelationID: "p17-t004-role", IdempotencyKey: "p17-t004-role-key"}, now.Add(10*time.Second))
	if err != nil {
		return out, err
	}
	childPassword := "P17-t004-child-password"
	child, _, err := service.CreateAdministrator(ctx, root, adminaccess.CreateAdministratorInput{Email: "session-child@p17.test", DisplayName: "Session Child", Password: childPassword, RoleIDs: []string{role.ID}}, adminaccess.MutationAuthority{Reason: "create session governance subject", CorrelationID: "p17-t004-admin", IdempotencyKey: "p17-t004-admin-key"}, now.Add(11*time.Second))
	if err != nil {
		return out, err
	}
	childLogin, err := service.Login(ctx, child.Email, childPassword, "", "p17-t004-child-login-1", now.Add(12*time.Second))
	if err != nil {
		return out, err
	}
	childPrincipal, err := service.Authenticate(ctx, childLogin.Token, now.Add(13*time.Second))
	if err != nil {
		return out, err
	}
	listed, err := service.ListSessions(ctx, root, child.ID)
	if err != nil {
		return out, err
	}
	_, actorBoundErr := service.ListSessions(ctx, childPrincipal, root.Administrator.ID)
	authority := adminaccess.MutationAuthority{Reason: "terminate compromised administrator session", CorrelationID: "p17-t004-revoke", IdempotencyKey: "p17-t004-revoke-key"}
	revoked, replay, err := service.RevokeSession(ctx, root, childLogin.Session.ID, authority, now.Add(14*time.Second))
	if err != nil || replay {
		return out, fmt.Errorf("first revoke replay=%v err=%w", replay, err)
	}
	_, immediateAuthErr := service.Authenticate(ctx, childLogin.Token, now.Add(15*time.Second))
	replayed, replay, err := service.RevokeSession(ctx, root, childLogin.Session.ID, authority, now.Add(16*time.Second))
	if err != nil || !replay {
		return out, fmt.Errorf("revoke replay replay=%v err=%w", replay, err)
	}
	secondLogin, err := service.Login(ctx, child.Email, childPassword, "", "p17-t004-child-login-2", now.Add(17*time.Second))
	if err != nil {
		return out, err
	}
	_, _, mismatchErr := service.RevokeSession(ctx, root, secondLogin.Session.ID, authority, now.Add(18*time.Second))
	auditRows, err := adminfixture.ScalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_audit_events WHERE action='admin.session.revoke' AND request_correlation_id='p17-t004-revoke'`)
	if err != nil {
		return out, err
	}
	var reason, beforeJSON, afterJSON string
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COALESCE(reason,''),CAST(before_json AS CHAR),CAST(after_json AS CHAR) FROM admin_audit_events WHERE action='admin.session.revoke' AND request_correlation_id='p17-t004-revoke' LIMIT 1`).Scan(&reason, &beforeJSON, &afterJSON); err != nil {
		return out, err
	}
	out.RecordCounts = map[string]int{"target_sessions_before_revoke": len(listed), "revoke_audit_events": auditRows}
	out.Checks = map[string]bool{
		"administrator_with_admins_manage_can_list_target_sessions": len(listed) >= 1 && listed[0].AdministratorID == child.ID,
		"unprivileged_actor_cannot_list_another_admin_sessions":     errors.Is(actorBoundErr, adminaccess.ErrForbidden),
		"revocation_requires_fresh_mfa_and_succeeds":                revoked.Status == "revoked" && revoked.RevokedAt != nil,
		"revoked_session_loses_authority_immediately":               errors.Is(immediateAuthErr, adminaccess.ErrUnauthorized),
		"same_idempotency_replays_same_revocation":                  replay && replayed.ID == revoked.ID && replayed.Status == "revoked",
		"same_key_with_different_target_fails_replay_safe":          errors.Is(mismatchErr, adminaccess.ErrReplayMismatch),
		"replay_does_not_duplicate_audit":                           auditRows == 1,
		"revoke_reason_and_before_after_are_audited":                reason == authority.Reason && strings.Contains(beforeJSON, "active") && strings.Contains(afterJSON, "revoked"),
	}
	pass(&out)
	return out, nil
}
