package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

const (
	seedEmail       = "seed-admin@p17.test"
	seedPassword    = "P17-browser-seed-password"
	rootEmail       = "admin@p17.test"
	rootPassword    = "P17-browser-root-password"
	limitedEmail    = "limited-admin@p17.test"
	limitedPassword = "P17-browser-limited-password"
)

var allPermissions = []string{
	adminaccess.PermissionPlatformRead,
	adminaccess.PermissionAdminsManage,
	adminaccess.PermissionUsersManage,
	adminaccess.PermissionWorkspacesManage,
	adminaccess.PermissionLinksManage,
	adminaccess.PermissionDomainsManage,
	adminaccess.PermissionDomainsRiskManage,
	adminaccess.PermissionDomainsEntitlementsManage,
	adminaccess.PermissionSecurityManage,
	adminaccess.PermissionFilesManage,
	adminaccess.PermissionTicketsManage,
	adminaccess.PermissionOperationsManage,
	adminaccess.PermissionBillingManage,
	adminaccess.PermissionMailManage,
	adminaccess.PermissionSettingsManage,
	adminaccess.PermissionContentManage,
}

type fixtureOutput struct {
	RootEmail       string `json:"root_email"`
	RootPassword    string `json:"root_password"`
	LimitedEmail    string `json:"limited_email"`
	LimitedPassword string `json:"limited_password"`
	WorkspaceID     string `json:"workspace_id"`
	OwnerActor      string `json:"owner_actor"`
	MemberActor     string `json:"member_actor"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	runtime, err := adminfixture.Open()
	if err != nil {
		fatal(err)
	}
	defer runtime.Close()

	now := time.Now().UTC().Truncate(time.Second)
	service, err := adminfixture.NewService(runtime, "browser", 1000)
	if err != nil {
		fatal(err)
	}
	_, err = adminfixture.Bootstrap(ctx, service, seedEmail, seedPassword, allPermissions, now)
	if err != nil {
		fatal(err)
	}
	seedPrincipal, _, _, err := adminfixture.LoginAndConfirmMFA(ctx, service, seedEmail, seedPassword, now.Add(time.Second))
	if err != nil {
		fatal(err)
	}

	fullRole, _, err := service.CreateRole(ctx, seedPrincipal, adminaccess.CreateRoleInput{
		Name: "P17 Browser Root", Description: "All P17 browser authority", Permissions: allPermissions,
	}, adminaccess.MutationAuthority{
		Reason: "seed P17 root browser role", CorrelationID: "p17-browser-root-role", IdempotencyKey: "p17-browser-root-role",
	}, now.Add(10*time.Second))
	if err != nil {
		fatal(err)
	}
	root, _, err := service.CreateAdministrator(ctx, seedPrincipal, adminaccess.CreateAdministratorInput{
		Email: rootEmail, DisplayName: "P17 Browser Root", Password: rootPassword, RoleIDs: []string{fullRole.ID},
	}, adminaccess.MutationAuthority{
		Reason: "seed P17 root browser admin", CorrelationID: "p17-browser-root-admin", IdempotencyKey: "p17-browser-root-admin",
	}, now.Add(11*time.Second))
	if err != nil {
		fatal(err)
	}

	limitedRole, _, err := service.CreateRole(ctx, seedPrincipal, adminaccess.CreateRoleInput{
		Name: "P17 Browser Limited", Description: "Platform read only", Permissions: []string{adminaccess.PermissionPlatformRead},
	}, adminaccess.MutationAuthority{
		Reason: "seed limited browser role", CorrelationID: "p17-browser-limited-role", IdempotencyKey: "p17-browser-limited-role",
	}, now.Add(12*time.Second))
	if err != nil {
		fatal(err)
	}
	_, _, err = service.CreateAdministrator(ctx, seedPrincipal, adminaccess.CreateAdministratorInput{
		Email: limitedEmail, DisplayName: "P17 Browser Limited", Password: limitedPassword, RoleIDs: []string{limitedRole.ID},
	}, adminaccess.MutationAuthority{
		Reason: "seed limited browser admin", CorrelationID: "p17-browser-limited-admin", IdempotencyKey: "p17-browser-limited-admin",
	}, now.Add(13*time.Second))
	if err != nil {
		fatal(err)
	}

	// Keep a second active root session so the browser can exercise accountable
	// session revocation without revoking the session currently driving the UI.
	if _, err := service.Login(ctx, rootEmail, rootPassword, "", "p17-browser-preexisting-session", now.Add(14*time.Second)); err != nil {
		fatal(err)
	}

	for _, user := range []struct {
		id, email, name, status string
	}{
		{"user-p17-active", "active-user@p17.test", "Active User", "active"},
		{"user-p17-disabled", "disabled-user@p17.test", "Disabled User", "disabled"},
	} {
		_, err = runtime.DB.ExecContext(ctx, `
INSERT INTO auth_users(id,email,email_normalized,display_name,status,email_verified_at,created_at,updated_at)
VALUES (?,?,?,?,?,UTC_TIMESTAMP(6),?,?)`, user.id, user.email, user.email, user.name, user.status, now, now)
		if err != nil {
			fatal(err)
		}
	}

	workspaces := []struct {
		id, name, status string
	}{
		{"ws-p17-browser", "P17 Browser Workspace", "active"},
		{"ws-p17-suspended", "Suspended Workspace", "suspended"},
		{"ws-ent-requested", "Requested Entitlement", "active"},
		{"ws-ent-plan", "Plan Entitlement", "active"},
		{"ws-ent-manual", "Manual Entitlement", "active"},
		{"ws-ent-expired", "Expired Entitlement", "active"},
		{"ws-ent-suspended", "Suspended Entitlement", "active"},
		{"ws-ent-revoked", "Revoked Entitlement", "active"},
	}
	for _, workspace := range workspaces {
		_, err = runtime.DB.ExecContext(ctx, `
INSERT INTO workspaces(id,name,status,version,created_by,created_at,updated_at)
VALUES (?,?,?,1,'p17-browser-fixture',?,?)`, workspace.id, workspace.name, workspace.status, now, now)
		if err != nil {
			fatal(err)
		}
	}
	for actor, role := range map[string]string{
		"owner-p17-browser":  "owner",
		"member-p17-browser": "member",
	} {
		_, err = runtime.DB.ExecContext(ctx, `
INSERT INTO workspace_memberships(workspace_id,user_id,email,display_name,role,joined_at,updated_at)
VALUES ('ws-p17-browser',?,?,?,?,?,?)`, actor, actor+"@p17.test", actor, role, now, now)
		if err != nil {
			fatal(err)
		}
	}

	_, err = runtime.DB.ExecContext(ctx, `
INSERT INTO custom_domain_entitlement_requests(workspace_id,support_ticket_id,requested_domain_limit,status,submitted_at)
VALUES ('ws-ent-requested','TICKET-P17-REQ',2,'requested',?)`, now.Add(-time.Hour))
	if err != nil {
		fatal(err)
	}
	_, err = runtime.DB.ExecContext(ctx, `
INSERT INTO custom_domain_entitlement_sources(workspace_id,source,source_key,status,domain_limit,starts_at,expires_at)
VALUES ('ws-ent-plan','plan','plan-browser','active',3,?,?)`, now.Add(-24*time.Hour), now.Add(30*24*time.Hour))
	if err != nil {
		fatal(err)
	}
	_, err = runtime.DB.ExecContext(ctx, `
INSERT INTO custom_domain_entitlement_sources(
  workspace_id,source,source_key,status,domain_limit,starts_at,expires_at,granted_by,support_ticket_id,decision_reason
) VALUES ('ws-ent-manual','manual_approval','manual-browser','active',2,?,?,?,'TICKET-P17-MANUAL','approved by browser fixture')`,
		now.Add(-24*time.Hour), now.Add(30*24*time.Hour), root.ID)
	if err != nil {
		fatal(err)
	}
	_, err = runtime.DB.ExecContext(ctx, `
INSERT INTO custom_domain_entitlement_sources(workspace_id,source,source_key,status,domain_limit,starts_at,expires_at)
VALUES ('ws-ent-expired','plan','expired-browser','expired',1,?,?)`, now.Add(-60*24*time.Hour), now.Add(-24*time.Hour))
	if err != nil {
		fatal(err)
	}
	for _, state := range []struct {
		ws, control string
	}{
		{"ws-ent-suspended", "suspended"},
		{"ws-ent-revoked", "revoked"},
	} {
		_, err = runtime.DB.ExecContext(ctx, `
INSERT INTO custom_domain_entitlement_sources(workspace_id,source,source_key,status,domain_limit,starts_at,expires_at)
VALUES (?, 'plan', CONCAT('plan-',?), 'active',2,?,?)`, state.ws, state.ws, now.Add(-24*time.Hour), now.Add(30*24*time.Hour))
		if err != nil {
			fatal(err)
		}
		_, err = runtime.DB.ExecContext(ctx, `
INSERT INTO admin_domain_entitlement_controls(workspace_id,state,reason,actor_id,decision_id,effective_at)
VALUES (?,?,'browser fixture control',?,CONCAT('decision-',?),?)`, state.ws, state.control, root.ID, state.ws, now.Add(-time.Hour))
		if err != nil {
			fatal(err)
		}
	}

	_, err = runtime.DB.ExecContext(ctx, `
INSERT INTO admin_platform_settings(setting_key,value_json,version,updated_by,updated_at)
VALUES ('general',CAST('{"site_name":"GoJet","public_base_url":"https://gojet.cc","support_url":"https://gojet.cc/contact"}' AS JSON),1,?,?)`, root.ID, now)
	if err != nil {
		fatal(err)
	}
	_, err = runtime.DB.ExecContext(ctx, `
INSERT INTO admin_turnstile_config(id,site_key,enabled,provider_state,version,updated_by,updated_at)
VALUES (1,'1x00000000000000000000AA',1,'healthy',1,?,?)`, root.ID, now)
	if err != nil {
		fatal(err)
	}
	_, err = runtime.DB.ExecContext(ctx, `
INSERT INTO admin_official_domains(id,hostname_ascii,enabled,is_default,https_state,version,created_by,updated_by,created_at,updated_at)
VALUES ('od_p17','gojet.cc',1,1,'active',1,?,?,?,?)`, root.ID, root.ID, now, now)
	if err != nil {
		fatal(err)
	}
	_, err = runtime.DB.ExecContext(ctx, `
INSERT INTO admin_announcements(id,title,summary,body,scope,lifecycle_state,published_at,version,created_by,updated_by,created_at,updated_at)
VALUES ('ann_p17','P17 Browser Announcement','Browser fixture summary','Browser fixture body','global','published',?,1,?,?,?,?)`, now.Add(-time.Hour), root.ID, root.ID, now, now)
	if err != nil {
		fatal(err)
	}

	result, err := runtime.DB.ExecContext(ctx, `
INSERT INTO links(
  workspace_id,hostname,domain_kind,code,title,primary_destination,redirect_status,status,version,risk_fingerprint,
  routing_json,ab_json,utm_json,access_json,created_at,updated_at
) VALUES ('ws-p17-browser','gojet.cc','official','p17-browser-job','Browser Job','https://example.com',302,'active',1,REPEAT('a',64),JSON_OBJECT(),JSON_OBJECT(),JSON_OBJECT(),JSON_OBJECT(),?,?)`, now, now)
	if err != nil {
		fatal(err)
	}
	linkID, err := result.LastInsertId()
	if err != nil {
		fatal(err)
	}
	_, err = runtime.DB.ExecContext(ctx, `
INSERT INTO destination_risk_scans(
  workspace_id,link_id,risk_fingerprint,policy_version,request_kind,idempotency_key,status,attempts,max_attempts,
  available_at,correlation_id,last_error_code,created_at,updated_at
) VALUES ('ws-p17-browser',?,REPEAT('a',64),'p17-browser-policy','rescan','p17-browser-job','retry',1,5,?,'p17-browser-job','provider-timeout',?,?)`,
		linkID, now.Add(-time.Minute), now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	if err != nil {
		fatal(err)
	}
	_, err = runtime.DB.ExecContext(ctx, `
INSERT INTO admin_audit_events(
  actor_kind,actor_id,action,resource_type,resource_id,result,request_correlation_id,reason,before_json,after_json,metadata_json,created_at
) VALUES ('system','p17-browser-fixture','browser.stale.fixture','fixture','stale','success','p17-browser-stale','browser stale evidence',JSON_OBJECT(),JSON_OBJECT(),JSON_OBJECT(),?)`, now.Add(-48*time.Hour))
	if err != nil {
		fatal(err)
	}

	_ = json.NewEncoder(os.Stdout).Encode(fixtureOutput{
		RootEmail: rootEmail, RootPassword: rootPassword,
		LimitedEmail: limitedEmail, LimitedPassword: limitedPassword,
		WorkspaceID: "ws-p17-browser", OwnerActor: "owner-p17-browser", MemberActor: "member-p17-browser",
	})
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
