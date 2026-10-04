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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	apiBase := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_P20_API_BASE")), "/")
	exactHead := strings.TrimSpace(os.Getenv("P20_EXACT_HEAD"))
	if dsn == "" || redisAddr == "" || apiBase == "" || len(exactHead) < 12 {
		writeResult(result{Errors: []string{"T011 runtime configuration is incomplete"}, Details: baseDetails()})
		return
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		writeResult(result{Errors: []string{"T011 could not open MySQL"}, Details: baseDetails()})
		return
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		writeResult(result{Errors: []string{"T011 could not reach MySQL"}, Details: baseDetails()})
		return
	}

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		writeResult(result{Errors: []string{"T011 could not reach Redis"}, Details: baseDetails()})
		return
	}

	suffix := strings.ToLower(exactHead[:12])
	email := "p20-t009-" + suffix + "@example.test"
	password := "P20-T009!Strong-Passphrase-2026"
	wrongPassword := password + "-wrong"
	wrongCorrelation := "p20-t011-wrong-" + suffix
	loginCorrelation := "p20-t011-login-" + suffix
	logoutCorrelation := "p20-t011-logout-" + suffix

	details := baseDetails()
	errors := make([]string, 0, 1)

	var userID, userStatus string
	if err := db.QueryRowContext(ctx,
		"SELECT id,status FROM auth_users WHERE email_normalized=?", email,
	).Scan(&userID, &userStatus); err != nil {
		errors = append(errors, "T011 could not bind to the T009/T010 auth user")
		writeResult(result{Errors: errors, Details: details})
		return
	}
	var workspaceID, workspaceRole string
	if err := db.QueryRowContext(ctx,
		"SELECT workspace_id,role FROM workspace_memberships WHERE user_id=?", userID,
	).Scan(&workspaceID, &workspaceRole); err != nil {
		errors = append(errors, "T011 could not bind to the T009/T010 Workspace membership")
		writeResult(result{Errors: errors, Details: details})
		return
	}
	details["user_id"] = userID
	details["workspace_id"] = workspaceID
	details["workspace_role"] = workspaceRole
	details["account_status_before_login"] = userStatus
	if userStatus != "active" || workspaceRole != "owner" {
		errors = append(errors, "T011 predecessor identity/Workspace state is not active owner authority")
		writeResult(result{Errors: errors, Details: details})
		return
	}

	preSessions, err := count(ctx, db, "SELECT COUNT(*) FROM auth_sessions WHERE user_id=?", userID)
	if err != nil {
		errors = append(errors, "T011 could not inspect pre-login session state")
		writeResult(result{Errors: errors, Details: details})
		return
	}
	wrong, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/auth/login", map[string]any{
		"email": email, "password": wrongPassword, "correlation_id": wrongCorrelation,
	}, nil)
	if err != nil {
		errors = append(errors, "T011 wrong-password HTTP probe failed")
		writeResult(result{Errors: errors, Details: details})
		return
	}
	postWrongSessions, _ := count(ctx, db, "SELECT COUNT(*) FROM auth_sessions WHERE user_id=?", userID)
	deniedAudits, _ := count(ctx, db,
		"SELECT COUNT(*) FROM auth_audit_events WHERE user_id=? AND action='auth.login.password' AND result='denied' AND request_correlation_id=?",
		userID, wrongCorrelation,
	)
	wrongCode := nestedErrorCode(wrong.Body)
	details["invalid_login_http_status"] = wrong.Status
	details["invalid_login_error_code"] = wrongCode
	details["invalid_login_created_session"] = postWrongSessions != preSessions
	details["invalid_login_audit_count"] = deniedAudits
	if wrong.Status != http.StatusUnauthorized || wrongCode != "invalid_credentials" || postWrongSessions != preSessions || deniedAudits != 1 {
		errors = append(errors, "real wrong-password login did not fail closed with the signed credential/session semantics")
		writeResult(result{Errors: errors, Details: details})
		return
	}

	rateKeys, err := scanKeys(ctx, redisClient, "auth:rate:login:*")
	if err != nil {
		errors = append(errors, "T011 could not inspect Redis login rate authority")
		writeResult(result{Errors: errors, Details: details})
		return
	}
	details["login_rate_protection_redis_key_count"] = len(rateKeys)
	details["login_rate_protection_observed"] = len(rateKeys) > 0
	if len(rateKeys) == 0 {
		errors = append(errors, "real password login path did not traverse signed Redis auth rate protection")
		writeResult(result{Errors: errors, Details: details})
		return
	}

	login, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/auth/login", map[string]any{
		"email": email, "password": password, "correlation_id": loginCorrelation,
	}, nil)
	if err != nil {
		errors = append(errors, "T011 real password login HTTP request failed")
		writeResult(result{Errors: errors, Details: details})
		return
	}
	details["login_http_status"] = login.Status
	details["login_response_status"] = stringValue(login.Body["status"])
	if login.Status != http.StatusOK || stringValue(login.Body["status"]) != "authenticated" {
		errors = append(errors, "verified T009/T010 account did not authenticate through the real login API")
		writeResult(result{Errors: errors, Details: details})
		return
	}

	sessionCookie := findSessionCookie(login.Cookies)
	if sessionCookie == nil || strings.TrimSpace(sessionCookie.Value) == "" {
		errors = append(errors, "real login did not establish the signed session cookie")
		writeResult(result{Errors: errors, Details: details})
		return
	}
	details["session_cookie_http_only"] = sessionCookie.HttpOnly
	details["session_cookie_secure"] = sessionCookie.Secure
	details["session_cookie_same_site_lax"] = sessionCookie.SameSite == http.SameSiteLaxMode

	var sessionID, sessionStatus, sessionCorrelation string
	if err := db.QueryRowContext(ctx,
		"SELECT id,status,correlation_id FROM auth_sessions WHERE user_id=? AND correlation_id=? ORDER BY created_at DESC LIMIT 1",
		userID, loginCorrelation,
	).Scan(&sessionID, &sessionStatus, &sessionCorrelation); err != nil {
		errors = append(errors, "real login did not create a correlated durable session")
		writeResult(result{Errors: errors, Details: details})
		return
	}
	details["session_id"] = sessionID
	details["session_status_after_login"] = sessionStatus
	details["session_correlation_preserved"] = sessionCorrelation == loginCorrelation
	if sessionStatus != "active" || sessionCorrelation != loginCorrelation {
		errors = append(errors, "real login session is not active/correlated")
		writeResult(result{Errors: errors, Details: details})
		return
	}

	cookieHeader := "__Host-gojet_session=" + sessionCookie.Value
	meHeaders := map[string]string{"Cookie": cookieHeader}
	me1, err := requestJSON(ctx, apiBase, http.MethodGet, "/api/me", nil, meHeaders)
	if err != nil {
		errors = append(errors, "T011 authenticated /api/me request failed")
		writeResult(result{Errors: errors, Details: details})
		return
	}
	me2, err := requestJSON(ctx, apiBase, http.MethodGet, "/api/me", nil, meHeaders)
	if err != nil {
		errors = append(errors, "T011 repeated authenticated /api/me request failed")
		writeResult(result{Errors: errors, Details: details})
		return
	}
	meUserID := nestedString(me1.Body, "user", "id")
	meSessionID := nestedString(me1.Body, "session", "id")
	csrf := stringValue(me1.Body["csrf_token"])
	details["me_http_status"] = me1.Status
	details["repeated_me_http_status"] = me2.Status
	details["api_identity_matches_t009"] = meUserID == userID
	details["api_session_matches_login"] = meSessionID == sessionID
	details["repeated_session_continuity"] = nestedString(me2.Body, "session", "id") == sessionID
	details["csrf_authority_issued"] = csrf != ""
	if me1.Status != http.StatusOK || me2.Status != http.StatusOK || meUserID != userID || meSessionID != sessionID || nestedString(me2.Body, "session", "id") != sessionID || csrf == "" {
		errors = append(errors, "browser-compatible cookie session and API identity are not continuous")
		writeResult(result{Errors: errors, Details: details})
		return
	}

	workspace, err := requestJSON(ctx, apiBase, http.MethodGet, "/api/workspaces", nil, meHeaders)
	if err != nil {
		errors = append(errors, "T011 real Workspace identity probe failed")
		writeResult(result{Errors: errors, Details: details})
		return
	}
	details["workspace_api_http_status"] = workspace.Status
	workspaceObserved := workspace.Status == http.StatusOK && workspaceListContains(workspace.Body, workspaceID)
	details["workspace_api_identity_matches_t009"] = workspaceObserved
	if !workspaceObserved {
		details["workspace_api_error_code"] = nestedErrorCode(workspace.Body)
		errors = append(errors, "real authenticated session is not accepted as Workspace API identity authority")
		writeResult(result{Errors: errors, Details: details})
		return
	}

	logoutHeaders := map[string]string{
		"Cookie":       cookieHeader,
		"Origin":       strings.TrimSpace(os.Getenv("GOJET_AUTH_ALLOWED_ORIGIN")),
		"X-CSRF-Token": csrf,
		"X-Request-ID": logoutCorrelation,
	}
	logout, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/auth/logout", nil, logoutHeaders)
	if err != nil {
		errors = append(errors, "T011 logout request failed")
		writeResult(result{Errors: errors, Details: details})
		return
	}
	details["logout_http_status"] = logout.Status
	details["logout_response_status"] = stringValue(logout.Body["status"])
	if logout.Status != http.StatusOK || stringValue(logout.Body["status"]) != "logged_out" {
		errors = append(errors, "real logout did not complete with signed CSRF/Origin authority")
		writeResult(result{Errors: errors, Details: details})
		return
	}

	var revokedStatus string
	var revokedAt sql.NullTime
	if err := db.QueryRowContext(ctx, "SELECT status,revoked_at FROM auth_sessions WHERE id=?", sessionID).Scan(&revokedStatus, &revokedAt); err != nil {
		errors = append(errors, "T011 could not inspect revoked session state")
		writeResult(result{Errors: errors, Details: details})
		return
	}
	revokeAudits, _ := count(ctx, db,
		"SELECT COUNT(*) FROM auth_audit_events WHERE user_id=? AND action='auth.session.revoked' AND resource_id=? AND result='success' AND request_correlation_id=?",
		userID, sessionID, logoutCorrelation,
	)
	details["session_status_after_logout"] = revokedStatus
	details["session_revoked_at_recorded"] = revokedAt.Valid
	details["logout_revoke_audit_count"] = revokeAudits
	if revokedStatus != "revoked" || !revokedAt.Valid || revokeAudits != 1 {
		errors = append(errors, "logout did not durably revoke/audit the current server session")
		writeResult(result{Errors: errors, Details: details})
		return
	}

	stale, err := requestJSON(ctx, apiBase, http.MethodGet, "/api/me", nil, meHeaders)
	if err != nil {
		errors = append(errors, "T011 stale-cookie revocation probe failed")
		writeResult(result{Errors: errors, Details: details})
		return
	}
	details["stale_cookie_http_status"] = stale.Status
	details["stale_cookie_error_code"] = nestedErrorCode(stale.Body)
	details["server_side_revocation_enforced"] = stale.Status == http.StatusGone && nestedErrorCode(stale.Body) == "revoked_token"
	if stale.Status != http.StatusGone || nestedErrorCode(stale.Body) != "revoked_token" {
		errors = append(errors, "revoked session remained usable on a later authenticated request")
	}

	writeResult(result{Errors: errors, Details: details})
}

func baseDetails() map[string]any {
	return map[string]any{
		"real_platform_api":        true,
		"real_mysql":               true,
		"real_redis":               true,
		"mock_authority":           false,
		"secret_material_recorded": false,
	}
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

func workspaceListContains(body map[string]any, workspaceID string) bool {
	items, ok := body["items"].([]any)
	if !ok {
		return false
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if ok && stringValue(item["id"]) == workspaceID {
			return true
		}
	}
	return false
}

func count(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var value int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

func scanKeys(ctx context.Context, client *redis.Client, pattern string) ([]string, error) {
	keys := make([]string, 0, 4)
	var cursor uint64
	for {
		batch, next, err := client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 {
			return keys, nil
		}
	}
}

func writeResult(value result) {
	if value.Details == nil {
		value.Details = baseDetails()
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, "T011 could not encode safe evidence")
	}
}
