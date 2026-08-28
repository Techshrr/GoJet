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

func runT022(ctx context.Context, runtime *adminfixture.Runtime) (map[string]bool, map[string]int, error) {
	now := time.Date(2026, 8, 28, 8, 22, 0, 0, time.UTC)
	workspaceID := "ws-p17-t022"
	if err := seedWorkspaceRoles(ctx, runtime.DB, workspaceID, map[string]string{"owner-t022": "owner"}, now); err != nil {
		return nil, nil, err
	}
	authority, err := adminaccess.NewWorkspaceAPIKeyAuthority(runtime.DB, runtime.Redis)
	if err != nil {
		return nil, nil, err
	}
	api, err := adminaccess.NewWorkspaceAPIKeyHTTPAPI(authority, true)
	if err != nil {
		return nil, nil, err
	}
	handler := api.Handler()
	status, createdBody, err := apiRequest(handler, "POST", "/api/workspaces/"+workspaceID+"/api-keys", "owner-t022", map[string]any{
		"name": "CI automation", "scopes": []string{"links:write", "links:read", "links:read"}, "rate_limit_per_minute": 3,
	}, nil)
	if err != nil {
		return nil, nil, err
	}
	secret, _ := createdBody["secret"].(string)
	keyMap, _ := createdBody["key"].(map[string]any)
	keyID, _ := keyMap["id"].(string)
	scopesValue, _ := keyMap["scopes"].([]any)
	statusList, listBody, err := apiRequest(handler, "GET", "/api/workspaces/"+workspaceID+"/api-keys", "owner-t022", nil, nil)
	if err != nil {
		return nil, nil, err
	}
	listedRaw, _ := json.Marshal(listBody)
	wildcardStatus, _, err := apiRequest(handler, "POST", "/api/workspaces/"+workspaceID+"/api-keys", "owner-t022", map[string]any{
		"name": "invalid wildcard", "scopes": []string{"links:*"}, "rate_limit_per_minute": 1,
	}, nil)
	if err != nil {
		return nil, nil, err
	}
	var storedHash []byte
	var storedPrefix string
	if err := runtime.DB.QueryRowContext(ctx, `SELECT secret_hash,secret_prefix FROM workspace_api_keys WHERE workspace_id=? AND id=?`, workspaceID, keyID).Scan(&storedHash, &storedPrefix); err != nil {
		return nil, nil, err
	}
	keyRows, err := scalar(ctx, runtime.DB, `SELECT COUNT(*) FROM workspace_api_keys WHERE workspace_id=?`, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	auditRows, err := scalar(ctx, runtime.DB, `SELECT COUNT(*) FROM workspace_audit_events WHERE workspace_id=? AND action='api_key.create' AND result='success'`, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	secretInAudit, err := auditContains(ctx, runtime.DB, workspaceID, secret)
	if err != nil {
		return nil, nil, err
	}
	checks := map[string]bool{
		"workspace_authorized_create":       status == 201 && keyID != "",
		"secret_returned_once":              strings.HasPrefix(secret, "gak_") && statusList == 200 && !strings.Contains(string(listedRaw), secret),
		"sha256_hash_only_storage":          len(storedHash) == 32 && string(storedHash) != secret && storedPrefix != "" && storedPrefix != secret,
		"scope_normalized_without_wildcard": wildcardStatus == 400 && reflect.DeepEqual(scopesValue, []any{"links:read", "links:write"}),
		"audit_omits_raw_secret":            auditRows == 1 && !secretInAudit,
	}
	counts := map[string]int{"api_keys": keyRows, "create_audit_events": auditRows}
	return checks, counts, nil
}
