package main

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT024(ctx context.Context, runtime *adminfixture.Runtime) (map[string]bool, map[string]int, error) {
	now := time.Date(2026, 8, 28, 8, 24, 0, 0, time.UTC)
	workspaceA := "ws-p17-t024-a"
	workspaceB := "ws-p17-t024-b"
	if err := seedWorkspaceRoles(ctx, runtime.DB, workspaceA, map[string]string{"owner-a": "owner", "admin-a": "admin", "member-a": "member"}, now); err != nil {
		return nil, nil, err
	}
	if err := seedWorkspaceRoles(ctx, runtime.DB, workspaceB, map[string]string{"owner-b": "owner"}, now.Add(time.Second)); err != nil {
		return nil, nil, err
	}
	authority, err := adminaccess.NewWorkspaceAPIKeyAuthority(runtime.DB, runtime.Redis)
	if err != nil {
		return nil, nil, err
	}
	created, err := authority.Create(ctx, workspaceA, "owner-a", adminaccess.WorkspaceAPIKeyInput{Name: "tenant-a", Scopes: []string{"links:read"}, RateLimitPerMinute: 5}, "p17-t024-create", now.Add(2*time.Second))
	if err != nil {
		return nil, nil, err
	}
	api, err := adminaccess.NewWorkspaceAPIKeyHTTPAPI(authority, true)
	if err != nil {
		return nil, nil, err
	}
	handler := api.Handler()
	adminStatus, adminBody, err := apiRequest(handler, "GET", "/api/workspaces/"+workspaceA+"/api-keys", "admin-a", nil, nil)
	if err != nil {
		return nil, nil, err
	}
	memberListStatus, _, err := apiRequest(handler, "GET", "/api/workspaces/"+workspaceA+"/api-keys", "member-a", nil, nil)
	if err != nil {
		return nil, nil, err
	}
	memberCreateStatus, _, err := apiRequest(handler, "POST", "/api/workspaces/"+workspaceA+"/api-keys", "member-a", map[string]any{"name": "denied", "scopes": []string{"links:read"}, "rate_limit_per_minute": 1}, nil)
	if err != nil {
		return nil, nil, err
	}
	forgedStatus, _, err := apiRequest(handler, "GET", "/api/workspaces/"+workspaceA+"/api-keys", "member-a", nil, map[string]string{"X-GoJet-Test-Workspace-Role": "owner"})
	if err != nil {
		return nil, nil, err
	}
	crossStatus, crossBody, err := apiRequest(handler, "POST", "/api/workspaces/"+workspaceB+"/api-keys/"+created.Key.ID+"/rotate", "owner-b", nil, nil)
	if err != nil {
		return nil, nil, err
	}
	randomStatus, randomBody, err := apiRequest(handler, "POST", "/api/workspaces/"+workspaceB+"/api-keys/wak_nonexistent_fixture/rotate", "owner-b", nil, nil)
	if err != nil {
		return nil, nil, err
	}
	workspaceBStatus, workspaceBBody, err := apiRequest(handler, "GET", "/api/workspaces/"+workspaceB+"/api-keys", "owner-b", nil, nil)
	if err != nil {
		return nil, nil, err
	}
	adminRaw, _ := json.Marshal(adminBody)
	workspaceBRaw, _ := json.Marshal(workspaceBBody)
	deniedAudit, err := scalar(ctx, runtime.DB, `SELECT COUNT(*) FROM workspace_audit_events WHERE workspace_id=? AND actor_id='member-a' AND action='api_key.create' AND result='denied'`, workspaceA)
	if err != nil {
		return nil, nil, err
	}
	keyRowsA, err := scalar(ctx, runtime.DB, `SELECT COUNT(*) FROM workspace_api_keys WHERE workspace_id=?`, workspaceA)
	if err != nil {
		return nil, nil, err
	}
	keyRowsB, err := scalar(ctx, runtime.DB, `SELECT COUNT(*) FROM workspace_api_keys WHERE workspace_id=?`, workspaceB)
	if err != nil {
		return nil, nil, err
	}
	checks := map[string]bool{
		"owner_admin_manage_member_denied":  adminStatus == 200 && strings.Contains(string(adminRaw), created.Key.ID) && memberListStatus == 403 && memberCreateStatus == 403,
		"frontend_role_not_authority":       forgedStatus == 403,
		"cross_workspace_no_existence_leak": crossStatus == 404 && randomStatus == 404 && reflect.DeepEqual(crossBody, randomBody),
		"workspace_list_isolated":           workspaceBStatus == 200 && !strings.Contains(string(workspaceBRaw), created.Key.ID) && keyRowsA == 1 && keyRowsB == 0,
		"denied_mutation_audited":           deniedAudit == 1,
	}
	counts := map[string]int{"workspace_a_keys": keyRowsA, "workspace_b_keys": keyRowsB, "denied_audit_events": deniedAudit}
	return checks, counts, nil
}
