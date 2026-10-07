package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/links"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

type httpResult struct {
	Status  int
	Body    map[string]any
	Headers http.Header
	Cookies []*http.Cookie
}

type result struct {
	Errors  []string       `json:"errors"`
	Details map[string]any `json:"details"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	apiBase := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_P20_API_BASE")), "/")
	exactHead := strings.TrimSpace(os.Getenv("P20_EXACT_HEAD"))
	origin := strings.TrimSpace(os.Getenv("GOJET_AUTH_ALLOWED_ORIGIN"))
	if dsn == "" || redisAddr == "" || apiBase == "" || origin == "" || len(exactHead) < 12 {
		writeResult(result{Errors: []string{"T012 runtime configuration is incomplete"}, Details: baseDetails()})
		return
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		writeResult(result{Errors: []string{"T012 could not open MySQL"}, Details: baseDetails()})
		return
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		writeResult(result{Errors: []string{"T012 could not reach MySQL"}, Details: baseDetails()})
		return
	}

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		writeResult(result{Errors: []string{"T012 could not reach Redis"}, Details: baseDetails()})
		return
	}
	riskStore := links.NewRedisRiskStore(redisClient)

	suffix := strings.ToLower(exactHead[:12])
	email := "p20-t009-" + suffix + "@example.test"
	password := "P20-T009!Strong-Passphrase-2026"
	loginCorrelation := "p20-t012-login-" + suffix
	createCorrelation := "p20-t012-create-" + suffix
	conflictCorrelation := "p20-t012-conflict-" + suffix
	updateCorrelation := "p20-t012-update-" + suffix
	viewerCorrelation := "p20-t012-viewer-" + suffix
	code := "p20t012" + suffix
	initialDestination := "https://example.com/p20/" + suffix + "?source=t012"
	updatedDestination := "https://example.net/p20/" + suffix + "?source=t012-update"

	details := baseDetails()
	errors := make([]string, 0, 1)

	var userID, userStatus string
	if err := db.QueryRowContext(ctx, "SELECT id,status FROM auth_users WHERE email_normalized=?", email).Scan(&userID, &userStatus); err != nil {
		fail(&errors, details, "T012 could not bind to the T009-T011 auth user")
		return
	}
	var workspaceID, workspaceRole string
	if err := db.QueryRowContext(ctx, "SELECT workspace_id,role FROM workspace_memberships WHERE user_id=?", userID).Scan(&workspaceID, &workspaceRole); err != nil {
		fail(&errors, details, "T012 could not bind to the T009-T011 Workspace membership")
		return
	}
	details["user_id"] = userID
	details["workspace_id"] = workspaceID
	details["workspace_role_before_link"] = workspaceRole
	if userStatus != "active" || workspaceRole != "owner" {
		fail(&errors, details, "T012 predecessor identity/Workspace state is not active owner authority")
		return
	}

	login, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/auth/login", map[string]any{
		"email": email, "password": password, "correlation_id": loginCorrelation,
	}, nil)
	if err != nil {
		fail(&errors, details, "T012 real password login HTTP request failed")
		return
	}
	details["login_http_status"] = login.Status
	if login.Status != http.StatusOK || stringValue(login.Body["status"]) != "authenticated" {
		details["login_error_code"] = nestedErrorCode(login.Body)
		fail(&errors, details, "T012 could not re-establish the same real authenticated user session")
		return
	}
	sessionCookie := findSessionCookie(login.Cookies)
	if sessionCookie == nil || strings.TrimSpace(sessionCookie.Value) == "" {
		fail(&errors, details, "T012 real login did not establish the signed session cookie")
		return
	}
	cookieHeader := "__Host-gojet_session=" + sessionCookie.Value

	csrf, meSessionID, err := issueCSRF(ctx, apiBase, cookieHeader, userID)
	if err != nil {
		fail(&errors, details, "T012 could not bind Link authority to the real P15 session")
		return
	}
	details["api_identity_matches_t009"] = true
	details["session_id"] = meSessionID
	details["csrf_authority_issued"] = csrf != ""

	createPayload := linkPayload(code, "P20 T012 Link", initialDestination, 0, "active", "P20 T012 create")
	created, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/workspaces/"+workspaceID+"/links", createPayload, unsafeHeaders(cookieHeader, origin, csrf, createCorrelation))
	if err != nil {
		fail(&errors, details, "T012 real Link creation HTTP request failed")
		return
	}
	details["link_create_http_status"] = created.Status
	details["link_create_error_code"] = nestedErrorCode(created.Body)
	if created.Status != http.StatusCreated {
		fail(&errors, details, "real authenticated session is not accepted as Links API mutation authority")
		return
	}

	linkID := uint64Value(created.Body["id"])
	version := uint64Value(created.Body["version"])
	fingerprint := stringValue(created.Body["risk_fingerprint"])
	createdWorkspace := stringValue(created.Body["workspace_id"])
	details["link_id"] = linkID
	details["link_create_version"] = version
	details["link_create_workspace_matches_t009"] = createdWorkspace == workspaceID
	details["link_create_fingerprint_present"] = validFingerprint(fingerprint)
	if linkID == 0 || version != 1 || createdWorkspace != workspaceID || !validFingerprint(fingerprint) {
		fail(&errors, details, "T012 Link creation response did not preserve workspace/version/fingerprint authority")
		return
	}

	var dbWorkspace, dbDestination, dbStatus, dbFingerprint string
	var dbVersion uint64
	if err := db.QueryRowContext(ctx, "SELECT workspace_id,primary_destination,status,version,risk_fingerprint FROM links WHERE id=?", linkID).Scan(&dbWorkspace, &dbDestination, &dbStatus, &dbVersion, &dbFingerprint); err != nil {
		fail(&errors, details, "T012 could not inspect the created Link in MySQL")
		return
	}
	details["mysql_create_matches_api"] = dbWorkspace == workspaceID && dbVersion == 1 && dbFingerprint == fingerprint && dbStatus == "active"
	if details["mysql_create_matches_api"] != true {
		fail(&errors, details, "T012 MySQL Link state did not match the API creation authority")
		return
	}
	createVersions, _ := count(ctx, db, "SELECT COUNT(*) FROM link_versions WHERE workspace_id=? AND link_id=? AND version=1 AND actor_id=?", workspaceID, linkID, userID)
	createAudits, _ := count(ctx, db, "SELECT COUNT(*) FROM link_audit_events WHERE workspace_id=? AND link_id=? AND actor_id=? AND action='link.create' AND request_correlation_id=? AND result='success'", workspaceID, linkID, userID, createCorrelation)
	details["link_create_version_record_count"] = createVersions
	details["link_create_audit_count"] = createAudits
	if createVersions != 1 || createAudits != 1 {
		fail(&errors, details, "T012 Link creation did not persist signed version/audit authority")
		return
	}

	if _, err := riskStore.PutDecision(ctx, linkID, fingerprint, links.RiskAllow, "p20-t012-v1", 10*time.Minute); err != nil {
		fail(&errors, details, "T012 could not establish the Redis risk decision fixture")
		return
	}
	_, riskState, err := riskStore.Resolve(ctx, linkID, fingerprint, time.Now().UTC())
	if err != nil || riskState != links.RiskAllow {
		fail(&errors, details, "T012 Redis risk decision was not bound to the created destination fingerprint")
		return
	}
	details["redis_initial_risk_state"] = string(riskState)

	got, err := requestJSON(ctx, apiBase, http.MethodGet, fmt.Sprintf("/api/workspaces/%s/links/%d", workspaceID, linkID), nil, map[string]string{"Cookie": cookieHeader})
	if err != nil || got.Status != http.StatusOK || uint64Value(got.Body["id"]) != linkID || stringValue(got.Body["risk_fingerprint"]) != fingerprint {
		fail(&errors, details, "T012 real authenticated Link read did not preserve created state")
		return
	}
	details["link_get_http_status"] = got.Status

	conflictCSRF, _, err := issueCSRF(ctx, apiBase, cookieHeader, userID)
	if err != nil {
		fail(&errors, details, "T012 could not issue fresh CSRF authority for version-conflict probe")
		return
	}
	conflictPayload := linkPayload(code, "P20 T012 stale update", updatedDestination, version+7, "active", "P20 T012 stale update")
	conflict, err := requestJSON(ctx, apiBase, http.MethodPatch, fmt.Sprintf("/api/workspaces/%s/links/%d", workspaceID, linkID), conflictPayload, unsafeHeaders(cookieHeader, origin, conflictCSRF, conflictCorrelation))
	if err != nil {
		fail(&errors, details, "T012 stale-version Link update request failed")
		return
	}
	details["version_conflict_http_status"] = conflict.Status
	details["version_conflict_error_code"] = nestedErrorCode(conflict.Body)
	postConflictVersions, _ := count(ctx, db, "SELECT COUNT(*) FROM link_versions WHERE workspace_id=? AND link_id=?", workspaceID, linkID)
	var postConflictVersion uint64
	var postConflictFingerprint, postConflictDestination string
	_ = db.QueryRowContext(ctx, "SELECT version,risk_fingerprint,primary_destination FROM links WHERE workspace_id=? AND id=?", workspaceID, linkID).Scan(&postConflictVersion, &postConflictFingerprint, &postConflictDestination)
	conflictFailClosed := conflict.Status == http.StatusConflict && postConflictVersion == version && postConflictFingerprint == fingerprint && postConflictDestination == dbDestination && postConflictVersions == 1
	details["if_match_conflict"] = conflictFailClosed
	if !conflictFailClosed {
		fail(&errors, details, "T012 stale Link version did not fail closed without durable mutation")
		return
	}

	updateCSRF, _, err := issueCSRF(ctx, apiBase, cookieHeader, userID)
	if err != nil {
		fail(&errors, details, "T012 could not issue fresh CSRF authority for Link mutation")
		return
	}
	updatePayload := linkPayload(code, "P20 T012 Link Updated", updatedDestination, version, "active", "P20 T012 destination mutation")
	updated, err := requestJSON(ctx, apiBase, http.MethodPatch, fmt.Sprintf("/api/workspaces/%s/links/%d", workspaceID, linkID), updatePayload, unsafeHeaders(cookieHeader, origin, updateCSRF, updateCorrelation))
	if err != nil {
		fail(&errors, details, "T012 real Link mutation HTTP request failed")
		return
	}
	updatedVersion := uint64Value(updated.Body["version"])
	updatedFingerprint := stringValue(updated.Body["risk_fingerprint"])
	details["link_update_http_status"] = updated.Status
	details["link_update_version"] = updatedVersion
	details["destination_fingerprint_changed"] = validFingerprint(updatedFingerprint) && updatedFingerprint != fingerprint
	if updated.Status != http.StatusOK || updatedVersion != version+1 || !validFingerprint(updatedFingerprint) || updatedFingerprint == fingerprint {
		fail(&errors, details, "T012 Link mutation did not increment version and invalidate the destination fingerprint")
		return
	}

	var mysqlUpdatedVersion uint64
	var mysqlUpdatedDestination, mysqlUpdatedFingerprint string
	if err := db.QueryRowContext(ctx, "SELECT version,primary_destination,risk_fingerprint FROM links WHERE workspace_id=? AND id=?", workspaceID, linkID).Scan(&mysqlUpdatedVersion, &mysqlUpdatedDestination, &mysqlUpdatedFingerprint); err != nil {
		fail(&errors, details, "T012 could not inspect mutated Link in MySQL")
		return
	}
	updateVersions, _ := count(ctx, db, "SELECT COUNT(*) FROM link_versions WHERE workspace_id=? AND link_id=? AND version=? AND actor_id=?", workspaceID, linkID, updatedVersion, userID)
	updateAudits, _ := count(ctx, db, "SELECT COUNT(*) FROM link_audit_events WHERE workspace_id=? AND link_id=? AND actor_id=? AND action='link.update' AND request_correlation_id=? AND result='success' AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.risk_invalidated'))='true'", workspaceID, linkID, userID, updateCorrelation)
	details["mysql_update_matches_api"] = mysqlUpdatedVersion == updatedVersion && mysqlUpdatedFingerprint == updatedFingerprint && mysqlUpdatedDestination == stringValue(updated.Body["primary_destination"])
	details["link_update_version_record_count"] = updateVersions
	details["link_update_audit_count"] = updateAudits
	if details["mysql_update_matches_api"] != true || updateVersions != 1 || updateAudits != 1 {
		fail(&errors, details, "T012 Link mutation did not preserve MySQL version/audit/fingerprint authority")
		return
	}

	_, newRiskState, err := riskStore.Resolve(ctx, linkID, updatedFingerprint, time.Now().UTC())
	if err != nil || newRiskState != links.RiskMissing {
		fail(&errors, details, "T012 changed destination incorrectly inherited the previous Redis risk decision")
		return
	}
	_, oldRiskState, err := riskStore.Resolve(ctx, linkID, fingerprint, time.Now().UTC())
	if err != nil || oldRiskState != links.RiskAllow {
		fail(&errors, details, "T012 previous fingerprint risk decision lost its exact-key continuity")
		return
	}
	details["redis_new_fingerprint_state_before_scan"] = string(newRiskState)
	details["redis_old_fingerprint_state_after_mutation"] = string(oldRiskState)
	details["destination_fingerprint_continuity"] = newRiskState == links.RiskMissing && oldRiskState == links.RiskAllow

	if _, err := riskStore.PutDecision(ctx, linkID, updatedFingerprint, links.RiskAllow, "p20-t012-v2", 10*time.Minute); err != nil {
		fail(&errors, details, "T012 could not bind a fresh Redis risk decision to the mutated fingerprint")
		return
	}
	_, refreshedRiskState, err := riskStore.Resolve(ctx, linkID, updatedFingerprint, time.Now().UTC())
	if err != nil || refreshedRiskState != links.RiskAllow {
		fail(&errors, details, "T012 fresh Redis risk decision did not bind to the mutated fingerprint")
		return
	}

	if _, err := db.ExecContext(ctx, "UPDATE workspace_memberships SET role='viewer' WHERE workspace_id=? AND user_id=?", workspaceID, userID); err != nil {
		fail(&errors, details, "T012 could not establish the viewer RBAC fixture")
		return
	}
	roleRestored := false
	defer func() {
		if !roleRestored {
			_, _ = db.ExecContext(context.Background(), "UPDATE workspace_memberships SET role='owner' WHERE workspace_id=? AND user_id=?", workspaceID, userID)
		}
	}()

	viewerCSRF, _, err := issueCSRF(ctx, apiBase, cookieHeader, userID)
	if err != nil {
		fail(&errors, details, "T012 could not issue fresh CSRF authority for viewer RBAC probe")
		return
	}
	viewerPayload := linkPayload(code, "P20 T012 Viewer Mutation", updatedDestination, updatedVersion, "active", "P20 T012 viewer mutation")
	viewerMutation, err := requestJSON(ctx, apiBase, http.MethodPatch, fmt.Sprintf("/api/workspaces/%s/links/%d", workspaceID, linkID), viewerPayload, unsafeHeaders(cookieHeader, origin, viewerCSRF, viewerCorrelation))
	if err != nil {
		fail(&errors, details, "T012 viewer Link mutation request failed")
		return
	}
	var afterViewerVersion uint64
	_ = db.QueryRowContext(ctx, "SELECT version FROM links WHERE workspace_id=? AND id=?", workspaceID, linkID).Scan(&afterViewerVersion)
	details["viewer_mutation_http_status"] = viewerMutation.Status
	details["viewer_mutation_error_code"] = nestedErrorCode(viewerMutation.Body)
	details["links_rbac_server_side"] = viewerMutation.Status == http.StatusForbidden && afterViewerVersion == updatedVersion
	if details["links_rbac_server_side"] != true {
		fail(&errors, details, "T012 viewer role was not enforced server-side for Link mutation")
		return
	}
	if _, err := db.ExecContext(ctx, "UPDATE workspace_memberships SET role='owner' WHERE workspace_id=? AND user_id=?", workspaceID, userID); err != nil {
		fail(&errors, details, "T012 could not restore the owner Workspace fixture")
		return
	}
	roleRestored = true
	details["workspace_role_restored"] = true
	details["audit_log"] = createAudits == 1 && updateAudits == 1
	details["version_contract"] = conflictFailClosed && updatedVersion == 2
	details["risk_invariants"] = details["destination_fingerprint_continuity"] == true && refreshedRiskState == links.RiskAllow

	writeResult(result{Errors: errors, Details: details})
}

func baseDetails() map[string]any {
	return map[string]any{
		"real_platform_api":        true,
		"real_mysql":               true,
		"real_redis":               true,
		"real_p05_links":           true,
		"mock_authority":           false,
		"test_header_authority":    false,
		"secret_material_recorded": false,
	}
}

func fail(errors *[]string, details map[string]any, message string) {
	*errors = append(*errors, message)
	writeResult(result{Errors: *errors, Details: details})
}

func linkPayload(code, title, destination string, expectedVersion uint64, status, reason string) map[string]any {
	payload := map[string]any{
		"hostname":            "gojet.cc",
		"domain_kind":         "official",
		"code":                code,
		"title":               title,
		"primary_destination": destination,
		"redirect_status":     http.StatusFound,
		"routing":             []any{},
		"ab":                  []any{},
		"utm":                 map[string]any{},
		"access":              map[string]any{},
		"one_time":            false,
		"change_reason":       reason,
	}
	if expectedVersion != 0 {
		payload["expected_version"] = expectedVersion
		payload["status"] = status
	}
	return payload
}

func unsafeHeaders(cookie, origin, csrf, correlation string) map[string]string {
	return map[string]string{
		"Cookie":       cookie,
		"Origin":       origin,
		"X-CSRF-Token": csrf,
		"X-Request-ID": correlation,
	}
}

func issueCSRF(ctx context.Context, apiBase, cookie, expectedUserID string) (string, string, error) {
	me, err := requestJSON(ctx, apiBase, http.MethodGet, "/api/me", nil, map[string]string{"Cookie": cookie})
	if err != nil || me.Status != http.StatusOK || nestedString(me.Body, "user", "id") != expectedUserID {
		return "", "", fmt.Errorf("real session identity mismatch")
	}
	csrf := stringValue(me.Body["csrf_token"])
	sessionID := nestedString(me.Body, "session", "id")
	if csrf == "" || sessionID == "" {
		return "", "", fmt.Errorf("missing session csrf authority")
	}
	return csrf, sessionID, nil
}

func requestJSON(ctx context.Context, base, method, path string, payload map[string]any, extra map[string]string) (httpResult, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return httpResult{}, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return httpResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range extra {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return httpResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return httpResult{}, err
	}
	decoded := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			decoded = map[string]any{"_non_json": true}
		}
	}
	return httpResult{Status: resp.StatusCode, Body: decoded, Headers: resp.Header.Clone(), Cookies: resp.Cookies()}, nil
}

func findSessionCookie(cookies []*http.Cookie) *http.Cookie {
	for _, cookie := range cookies {
		if cookie != nil && cookie.Name == "__Host-gojet_session" {
			return cookie
		}
	}
	return nil
}

func nestedErrorCode(body map[string]any) string {
	return nestedString(body, "error", "code")
}

func nestedString(body map[string]any, first, second string) string {
	outer, ok := body[first].(map[string]any)
	if !ok {
		return ""
	}
	return stringValue(outer[second])
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func uint64Value(value any) uint64 {
	typed, ok := value.(float64)
	if !ok || typed <= 0 || typed != float64(uint64(typed)) {
		return 0
	}
	return uint64(typed)
}

func validFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

func count(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var value int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

func writeResult(value result) {
	if value.Details == nil {
		value.Details = baseDetails()
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, "T012 could not encode safe evidence")
	}
}
