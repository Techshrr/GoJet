package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT010(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real P15 user and P12 workspace governance with independent permissions, safe enumeration, fresh-MFA reason/idempotency and immutable P17 audit")
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	service, root, rootLogin, err := bootstrapRoot(ctx, runtime, now)
	if err != nil {
		return out, err
	}

	const (
		verifiedUserID   = "usr_p17_t010_verified"
		verifiedEmail    = "verified-user@p17.test"
		unverifiedUserID = "usr_p17_t010_pending"
		unverifiedEmail  = "pending-user@p17.test"
		workspaceID      = "ws_p17_t010"
	)
	if err := seedUser(ctx, runtime.DB, verifiedUserID, verifiedEmail, "Verified User", true, now); err != nil {
		return out, err
	}
	if err := seedUser(ctx, runtime.DB, unverifiedUserID, unverifiedEmail, "Pending User", false, now); err != nil {
		return out, err
	}
	if err := seedCustomerSession(ctx, runtime.DB, verifiedUserID, now); err != nil {
		return out, err
	}
	if err := seedWorkspace(ctx, runtime.DB, workspaceID, "P17 Governance Workspace", verifiedUserID, verifiedEmail, unverifiedUserID, unverifiedEmail, now); err != nil {
		return out, err
	}

	userOnly, err := createScopedAdmin(ctx, service, root, "users-only", "users-only@p17.test", "P17-users-only-password-fixture", adminaccess.PermissionUsersManage, now.Add(10*time.Second))
	if err != nil {
		return out, err
	}
	workspaceOnly, err := createScopedAdmin(ctx, service, root, "workspaces-only", "workspaces-only@p17.test", "P17-workspaces-only-password-fixture", adminaccess.PermissionWorkspacesManage, now.Add(20*time.Second))
	if err != nil {
		return out, err
	}

	server, err := adminfixture.NewHTTPServer(service)
	if err != nil {
		return out, err
	}
	defer server.Close()

	userList, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/users", "", userOnly.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	userDetail, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/users/"+verifiedUserID, "", userOnly.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	userCannotWorkspace, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/workspaces", "", userOnly.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	workspaceList, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/workspaces", "", workspaceOnly.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	workspaceDetail, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/workspaces/"+workspaceID, "", workspaceOnly.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	workspaceCannotUsers, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/users", "", workspaceOnly.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}

	missingReason, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/users/"+verifiedUserID+"/suspend", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t010-missing-reason-key", "p17-t010-missing-reason", map[string]any{"reason": ""})
	if err != nil {
		return out, err
	}
	workspaceDeniedMutation, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/workspaces/"+workspaceID+"/suspend", adminfixture.AllowedOrigin, userOnly.Token, userOnly.CSRFToken, "p17-t010-user-workspace-denied-key", "p17-t010-user-workspace-denied", map[string]any{"reason": "independent permission denial"})
	if err != nil {
		return out, err
	}
	userDeniedMutation, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/users/"+verifiedUserID+"/suspend", adminfixture.AllowedOrigin, workspaceOnly.Token, workspaceOnly.CSRFToken, "p17-t010-workspace-user-denied-key", "p17-t010-workspace-user-denied", map[string]any{"reason": "independent permission denial"})
	if err != nil {
		return out, err
	}
	userNeedsFreshMFA, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/users/"+verifiedUserID+"/suspend", adminfixture.AllowedOrigin, userOnly.Token, userOnly.CSRFToken, "p17-t010-user-mfa-key", "p17-t010-user-mfa", map[string]any{"reason": "permission alone must not satisfy high-risk mutation authority"})
	if err != nil {
		return out, err
	}
	workspaceNeedsFreshMFA, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/workspaces/"+workspaceID+"/suspend", adminfixture.AllowedOrigin, workspaceOnly.Token, workspaceOnly.CSRFToken, "p17-t010-workspace-mfa-key", "p17-t010-workspace-mfa", map[string]any{"reason": "permission alone must not satisfy high-risk mutation authority"})
	if err != nil {
		return out, err
	}

	userSuspend, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/users/"+verifiedUserID+"/suspend", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t010-user-suspend-key", "p17-t010-user-suspend", map[string]any{"reason": "suspend verified account for accountable administrator review"})
	if err != nil {
		return out, err
	}
	userStatusAfterSuspend, err := scalarString(ctx, runtime.DB, `SELECT status FROM auth_users WHERE id=?`, verifiedUserID)
	if err != nil {
		return out, err
	}
	customerSessionStatus, err := scalarString(ctx, runtime.DB, `SELECT status FROM auth_sessions WHERE user_id=?`, verifiedUserID)
	if err != nil {
		return out, err
	}
	userVersionAfterSuspend, err := scalarInt(ctx, runtime.DB, `SELECT version FROM auth_users WHERE id=?`, verifiedUserID)
	if err != nil {
		return out, err
	}
	userSuspendAuditCount, err := auditCount(ctx, runtime.DB, "admin.user.suspend", verifiedUserID)
	if err != nil {
		return out, err
	}
	userSuspendAuditOK, err := auditAccountable(ctx, runtime.DB, "admin.user.suspend", verifiedUserID, "p17-t010-user-suspend")
	if err != nil {
		return out, err
	}
	userSuspendReplay, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/users/"+verifiedUserID+"/suspend", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t010-user-suspend-key", "p17-t010-user-suspend-replay", map[string]any{"reason": "suspend verified account for accountable administrator review"})
	if err != nil {
		return out, err
	}
	userVersionAfterReplay, err := scalarInt(ctx, runtime.DB, `SELECT version FROM auth_users WHERE id=?`, verifiedUserID)
	if err != nil {
		return out, err
	}
	userSuspendAuditAfterReplay, err := auditCount(ctx, runtime.DB, "admin.user.suspend", verifiedUserID)
	if err != nil {
		return out, err
	}
	userReplayMismatch, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/users/"+verifiedUserID+"/suspend", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t010-user-suspend-key", "p17-t010-user-suspend-replay-mismatch", map[string]any{"reason": "different reason must not reuse an existing idempotency key"})
	if err != nil {
		return out, err
	}

	userRestore, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/users/"+verifiedUserID+"/restore", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t010-user-restore-key", "p17-t010-user-restore", map[string]any{"reason": "restore verified account after administrator review"})
	if err != nil {
		return out, err
	}
	verifiedRestoredStatus, err := scalarString(ctx, runtime.DB, `SELECT status FROM auth_users WHERE id=?`, verifiedUserID)
	if err != nil {
		return out, err
	}

	pendingSuspend, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/users/"+unverifiedUserID+"/suspend", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t010-pending-suspend-key", "p17-t010-pending-suspend", map[string]any{"reason": "suspend unverified account without changing verification authority"})
	if err != nil {
		return out, err
	}
	pendingRestore, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/users/"+unverifiedUserID+"/restore", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t010-pending-restore-key", "p17-t010-pending-restore", map[string]any{"reason": "restore unverified account while preserving verification requirement"})
	if err != nil {
		return out, err
	}
	pendingRestoredStatus, err := scalarString(ctx, runtime.DB, `SELECT status FROM auth_users WHERE id=?`, unverifiedUserID)
	if err != nil {
		return out, err
	}

	membersBefore, err := scalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM workspace_memberships WHERE workspace_id=?`, workspaceID)
	if err != nil {
		return out, err
	}
	ownersBefore, err := scalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM workspace_memberships WHERE workspace_id=? AND role='owner'`, workspaceID)
	if err != nil {
		return out, err
	}
	workspaceSuspend, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/workspaces/"+workspaceID+"/suspend", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t010-workspace-suspend-key", "p17-t010-workspace-suspend", map[string]any{"reason": "suspend workspace while preserving owner and member authority"})
	if err != nil {
		return out, err
	}
	workspaceStatusAfterSuspend, err := scalarString(ctx, runtime.DB, `SELECT status FROM workspaces WHERE id=?`, workspaceID)
	if err != nil {
		return out, err
	}
	workspaceVersionAfterSuspend, err := scalarInt(ctx, runtime.DB, `SELECT version FROM workspaces WHERE id=?`, workspaceID)
	if err != nil {
		return out, err
	}
	workspaceAuditCount, err := auditCount(ctx, runtime.DB, "admin.workspace.suspend", workspaceID)
	if err != nil {
		return out, err
	}
	workspaceAuditOK, err := auditAccountable(ctx, runtime.DB, "admin.workspace.suspend", workspaceID, "p17-t010-workspace-suspend")
	if err != nil {
		return out, err
	}
	workspaceReplay, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/workspaces/"+workspaceID+"/suspend", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t010-workspace-suspend-key", "p17-t010-workspace-suspend-replay", map[string]any{"reason": "suspend workspace while preserving owner and member authority"})
	if err != nil {
		return out, err
	}
	workspaceVersionAfterReplay, err := scalarInt(ctx, runtime.DB, `SELECT version FROM workspaces WHERE id=?`, workspaceID)
	if err != nil {
		return out, err
	}
	workspaceAuditAfterReplay, err := auditCount(ctx, runtime.DB, "admin.workspace.suspend", workspaceID)
	if err != nil {
		return out, err
	}
	workspaceRestore, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/workspaces/"+workspaceID+"/restore", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t010-workspace-restore-key", "p17-t010-workspace-restore", map[string]any{"reason": "restore workspace after accountable review"})
	if err != nil {
		return out, err
	}
	workspaceRestoredStatus, err := scalarString(ctx, runtime.DB, `SELECT status FROM workspaces WHERE id=?`, workspaceID)
	if err != nil {
		return out, err
	}
	membersAfter, err := scalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM workspace_memberships WHERE workspace_id=?`, workspaceID)
	if err != nil {
		return out, err
	}
	ownersAfter, err := scalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM workspace_memberships WHERE workspace_id=? AND role='owner'`, workspaceID)
	if err != nil {
		return out, err
	}

	for label, result := range map[string]adminfixture.HTTPResult{
		"user list":           userList,
		"user detail":         userDetail,
		"workspace list":      workspaceList,
		"workspace detail":    workspaceDetail,
		"user suspend":        userSuspend,
		"user suspend replay": userSuspendReplay,
		"user restore":        userRestore,
		"pending suspend":     pendingSuspend,
		"pending restore":     pendingRestore,
		"workspace suspend":   workspaceSuspend,
		"workspace replay":    workspaceReplay,
		"workspace restore":   workspaceRestore,
	} {
		if err := mustStatus(label, result.Status, http.StatusOK); err != nil {
			return out, err
		}
	}

	totalAdminAudit, err := scalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_audit_events WHERE resource_type IN ('auth_user','workspace')`)
	if err != nil {
		return out, err
	}
	revokedCustomerSessions, err := scalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM auth_sessions WHERE user_id=? AND status='revoked' AND revoked_at IS NOT NULL`, verifiedUserID)
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{
		"managed_users":             2,
		"workspace_memberships":     membersAfter,
		"revoked_customer_sessions": revokedCustomerSessions,
		"governance_audit_events":   totalAdminAudit,
	}
	out.Checks = map[string]bool{
		"users_manage_and_workspaces_manage_are_independent":          userList.Status == 200 && userDetail.Status == 200 && userCannotWorkspace.Status == 403 && workspaceList.Status == 200 && workspaceDetail.Status == 200 && workspaceCannotUsers.Status == 403 && workspaceDeniedMutation.Status == 403 && userDeniedMutation.Status == 403,
		"enumeration_is_no_store_noindex_and_secret_safe":             adminfixture.NoStoreNoIndex(userList) && adminfixture.NoStoreNoIndex(userDetail) && adminfixture.NoStoreNoIndex(workspaceList) && adminfixture.NoStoreNoIndex(workspaceDetail) && safeEnumeration(userList.Raw) && safeEnumeration(userDetail.Raw) && safeEnumeration(workspaceList.Raw) && safeEnumeration(workspaceDetail.Raw),
		"high_risk_mutation_requires_permission_fresh_mfa_and_reason": missingReason.Status == 422 && userNeedsFreshMFA.Status == 428 && workspaceNeedsFreshMFA.Status == 428 && userSuspendAuditOK,
		"user_suspend_revokes_customer_sessions_immediately":          userStatusAfterSuspend == "disabled" && customerSessionStatus == "revoked" && revokedCustomerSessions == 1 && userVersionAfterSuspend == 2,
		"user_suspend_idempotency_is_reason_bound_and_replay_safe":    userSuspendReplay.Status == 200 && userReplayMismatch.Status == 409 && userVersionAfterReplay == userVersionAfterSuspend && userSuspendAuditCount == 1 && userSuspendAuditAfterReplay == 1,
		"user_restore_never_bypasses_p15_email_verification":          verifiedRestoredStatus == "active" && pendingSuspend.Status == 200 && pendingRestore.Status == 200 && pendingRestoredStatus == "pending_verification",
		"workspace_suspend_preserves_membership_authority":            workspaceStatusAfterSuspend == "suspended" && workspaceVersionAfterSuspend == 2 && membersBefore == 2 && membersAfter == membersBefore && ownersBefore == 1 && ownersAfter == ownersBefore,
		"workspace_mutation_is_reasoned_audited_and_replay_safe":      workspaceAuditOK && workspaceReplay.Status == 200 && workspaceVersionAfterReplay == workspaceVersionAfterSuspend && workspaceAuditCount == 1 && workspaceAuditAfterReplay == 1 && workspaceRestore.Status == 200 && workspaceRestoredStatus == "active",
	}
	pass(&out)
	if out.Status != "PASS" {
		return out, fmt.Errorf("P17-T010 checks failed: %+v", out.Checks)
	}
	return out, nil
}
