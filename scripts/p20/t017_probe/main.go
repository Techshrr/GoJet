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
	"path/filepath"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type predecessorEvidence struct {
	Status               string `json:"status"`
	ImplementationCommit string `json:"implementation_commit"`
	Details              struct {
		UserID      string `json:"user_id"`
		WorkspaceID string `json:"workspace_id"`
		NextCase    string `json:"next_case"`
	} `json:"details"`
}

type probe struct {
	Status               string         `json:"status"`
	ImplementationCommit string         `json:"implementation_commit"`
	Errors               []string       `json:"errors"`
	Details              map[string]any `json:"details"`
}

type httpResult struct {
	Status  int
	Body    map[string]any
	Raw     []byte
	Headers http.Header
	Cookies []*http.Cookie
}

const (
	fixtureV1 = "GoJet P20 T017 real text fixture v1.\n"
	fixtureV2 = "GoJet P20 T017 real text fixture v2.\n"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exactHead := strings.TrimSpace(os.Getenv("P20_EXACT_HEAD"))
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	apiBase := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_P20_API_BASE")), "/")
	origin := strings.TrimSpace(os.Getenv("GOJET_AUTH_ALLOWED_ORIGIN"))
	outPath := strings.TrimSpace(os.Getenv("GOJET_P20_T017_PROBE_OUT"))
	if outPath == "" {
		outPath = "artifacts/v10/P20/runtime/t017/probe.json"
	}

	result := probe{
		Status:               "FAIL",
		ImplementationCommit: exactHead,
		Errors:               []string{},
		Details: map[string]any{
			"real_mysql":                 true,
			"real_platform_api":          true,
			"real_session_authenticated": false,
			"csrf_authority_issued":      false,
			"mock_authority":             false,
			"test_header_authority":      false,
			"secret_material_recorded":   false,
		},
	}
	finish := func() {
		if len(result.Errors) == 0 {
			result.Status = "PASS"
		}
		_ = os.MkdirAll(filepath.Dir(outPath), 0o755)
		raw, _ := json.MarshalIndent(result, "", "  ")
		raw = append(raw, '\n')
		_ = os.WriteFile(outPath, raw, 0o644)
		_, _ = os.Stdout.Write(raw)
		if len(result.Errors) != 0 {
			os.Exit(1)
		}
	}
	fail := func(message string) {
		result.Errors = append(result.Errors, message)
		finish()
	}

	if len(exactHead) != 40 || dsn == "" || apiBase == "" || origin == "" {
		fail("T017 probe runtime configuration is incomplete")
		return
	}

	t016, err := readT016("artifacts/v10/P20/p0/P20-T016.json", exactHead)
	if err != nil || t016.Details.UserID == "" || t016.Details.WorkspaceID == "" || t016.Details.NextCase != "P20-T017" {
		fail("T017 probe requires same-run exact-head P20-T016 PASS evidence with next_case=P20-T017")
		return
	}
	result.Details["t016_evidence_bound"] = true
	result.Details["user_id"] = t016.Details.UserID
	result.Details["workspace_id"] = t016.Details.WorkspaceID

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fail("T017 probe could not open MySQL")
		return
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		fail("T017 probe could not reach MySQL")
		return
	}

	var userStatus, workspaceRole string
	if err := db.QueryRowContext(ctx, `
		SELECT u.status,m.role
		FROM auth_users u JOIN workspace_memberships m ON m.user_id=u.id
		WHERE u.id=? AND m.workspace_id=?`, t016.Details.UserID, t016.Details.WorkspaceID,
	).Scan(&userStatus, &workspaceRole); err != nil || userStatus != "active" || workspaceRole != "owner" {
		fail("T017 predecessor identity is not an active owner Workspace authority")
		return
	}
	result.Details["workspace_role"] = workspaceRole

	suffix := strings.ToLower(exactHead[:12])
	email := "p20-t009-" + suffix + "@example.test"
	login, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/auth/login", map[string]any{
		"email":          email,
		"password":       "P20-T009!Strong-Passphrase-2026",
		"correlation_id": "p20-t017-login-" + suffix,
	}, nil)
	if err != nil {
		fail("T017 probe real P15 login request failed")
		return
	}
	result.Details["login_http_status"] = login.Status
	if login.Status != http.StatusOK || stringValue(login.Body["status"]) != "authenticated" {
		result.Details["login_error_code"] = nestedErrorCode(login.Body)
		fail("T017 probe could not establish the real authenticated Text session")
		return
	}
	sessionCookie := findSessionCookie(login.Cookies)
	if sessionCookie == nil || strings.TrimSpace(sessionCookie.Value) == "" {
		fail("T017 real login did not issue the signed session cookie")
		return
	}
	cookieHeader := "__Host-gojet_session=" + sessionCookie.Value
	csrf, sessionID, err := issueCSRF(ctx, apiBase, cookieHeader, t016.Details.UserID)
	if err != nil {
		fail("T017 real session is not accepted by /api/me")
		return
	}
	result.Details["real_session_authenticated"] = true
	result.Details["csrf_authority_issued"] = csrf != ""
	result.Details["session_id_present"] = sessionID != ""

	rowsBefore, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM text_shares WHERE workspace_id=? AND deleted_at IS NULL`, t016.Details.WorkspaceID)
	if err != nil {
		fail("T017 probe could not inspect pre-create Text row count")
		return
	}
	result.Details["text_rows_before"] = rowsBefore

	created, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/workspaces/"+t016.Details.WorkspaceID+"/text-shares", map[string]any{
		"title":         "P20 T017 real Text",
		"content":       fixtureV1,
		"visibility":    "public",
		"change_reason": "P20 T017 real Text create",
	}, unsafeHeaders(cookieHeader, origin, csrf, "p20-t017-create-"+suffix))
	if err != nil {
		fail("T017 real Text create request failed")
		return
	}
	rowsAfter, _ := scalarInt(ctx, db, `SELECT COUNT(*) FROM text_shares WHERE workspace_id=? AND deleted_at IS NULL`, t016.Details.WorkspaceID)
	result.Details["text_create_http_status"] = created.Status
	result.Details["text_create_error_code"] = nestedErrorCode(created.Body)
	result.Details["text_create_row_delta"] = rowsAfter - rowsBefore
	if created.Status != http.StatusCreated {
		result.Details["text_create_failed_without_write"] = rowsAfter == rowsBefore
		fail("real authenticated session is not accepted as Text API mutation authority")
		return
	}

	shareID := uint64Value(created.Body["id"])
	publicSlug := stringValue(created.Body["public_slug"])
	version := uint64Value(created.Body["version"])
	if shareID == 0 || publicSlug == "" || version != 1 || stringValue(created.Body["workspace_id"]) != t016.Details.WorkspaceID || stringValue(created.Body["visibility"]) != "public" {
		fail("T017 create did not return the expected public Text identity")
		return
	}
	result.Details["text_id"] = shareID
	result.Details["text_public_slug_present"] = true
	result.Details["text_create_identity_bound"] = true

	workspaceRead, err := requestRaw(ctx, apiBase, http.MethodGet, fmt.Sprintf("/api/workspaces/%s/text-shares/%d", t016.Details.WorkspaceID, shareID), nil, map[string]string{"Cookie": cookieHeader})
	if err != nil || workspaceRead.Status != http.StatusOK || stringValue(workspaceRead.Body["content"]) != fixtureV1 {
		fail("T017 authenticated Text read did not preserve created content")
		return
	}
	result.Details["workspace_read_http_status"] = workspaceRead.Status
	result.Details["workspace_read_content_matches"] = true

	publicRead, err := requestRaw(ctx, apiBase, http.MethodGet, "/t/"+publicSlug, nil, nil)
	if err != nil || publicRead.Status != http.StatusOK || !bytes.Contains(publicRead.Raw, []byte(fixtureV1)) {
		fail("T017 public Text page did not expose the authorized public content")
		return
	}
	robots := strings.ToLower(publicRead.Headers.Get("X-Robots-Tag"))
	bodyLower := strings.ToLower(string(publicRead.Raw))
	noindex := strings.Contains(robots, "noindex") && strings.Contains(bodyLower, `name="robots"`) && strings.Contains(bodyLower, "noindex")
	result.Details["public_read_http_status"] = publicRead.Status
	result.Details["public_content_matches"] = true
	result.Details["permanent_noindex"] = noindex
	result.Details["canonical_abuse_entry"] = strings.Contains(bodyLower, `href="/abuse/report"`)
	if !noindex || result.Details["canonical_abuse_entry"] != true {
		fail("T017 public Text page did not preserve permanent noindex/UGC policy")
		return
	}

	updateCSRF, _, err := issueCSRF(ctx, apiBase, cookieHeader, t016.Details.UserID)
	if err != nil {
		fail("T017 could not issue one-time CSRF for Text update")
		return
	}
	updated, err := requestJSON(ctx, apiBase, http.MethodPatch, fmt.Sprintf("/api/workspaces/%s/text-shares/%d", t016.Details.WorkspaceID, shareID), map[string]any{
		"expected_version": version,
		"content":          fixtureV2,
		"change_reason":    "P20 T017 real Text update",
	}, unsafeHeaders(cookieHeader, origin, updateCSRF, "p20-t017-update-"+suffix))
	if err != nil || updated.Status != http.StatusOK || uint64Value(updated.Body["version"]) != 2 || stringValue(updated.Body["content"]) != fixtureV2 {
		fail("T017 real Text update did not preserve versioned content authority")
		return
	}
	result.Details["text_update_http_status"] = updated.Status
	result.Details["text_update_version"] = uint64Value(updated.Body["version"])

	publicUpdated, err := requestRaw(ctx, apiBase, http.MethodGet, "/t/"+publicSlug, nil, nil)
	if err != nil || publicUpdated.Status != http.StatusOK || !bytes.Contains(publicUpdated.Raw, []byte(fixtureV2)) || bytes.Contains(publicUpdated.Raw, []byte(fixtureV1)) {
		fail("T017 public Text page did not reflect the committed update")
		return
	}
	result.Details["public_update_visible"] = true

	expiryCSRF, _, err := issueCSRF(ctx, apiBase, cookieHeader, t016.Details.UserID)
	if err != nil {
		fail("T017 could not issue one-time CSRF for Text expiry update")
		return
	}
	expiresAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	expired, err := requestJSON(ctx, apiBase, http.MethodPatch, fmt.Sprintf("/api/workspaces/%s/text-shares/%d", t016.Details.WorkspaceID, shareID), map[string]any{
		"expected_version": 2,
		"expires_at":       expiresAt,
		"change_reason":    "P20 T017 expire Text",
	}, unsafeHeaders(cookieHeader, origin, expiryCSRF, "p20-t017-expire-"+suffix))
	if err != nil || expired.Status != http.StatusOK || uint64Value(expired.Body["version"]) != 3 {
		fail("T017 Text expiry mutation did not commit")
		return
	}
	result.Details["text_expire_update_http_status"] = expired.Status

	publicExpired, err := requestRaw(ctx, apiBase, http.MethodGet, "/t/"+publicSlug, nil, nil)
	if err != nil || publicExpired.Status != http.StatusGone || bytes.Contains(publicExpired.Raw, []byte(fixtureV2)) {
		fail("T017 expired public Text did not fail closed with HTTP 410 and no content")
		return
	}
	result.Details["expired_public_http_status"] = publicExpired.Status
	result.Details["expired_content_hidden"] = true
	result.Details["next_case"] = "P20-T018"

	finish()
}

func readT016(path, exactHead string) (predecessorEvidence, error) {
	var value predecessorEvidence
	raw, err := os.ReadFile(path)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, err
	}
	if value.Status != "PASS" || value.ImplementationCommit != exactHead {
		return value, fmt.Errorf("not exact-head PASS")
	}
	return value, nil
}

func requestJSON(ctx context.Context, base, method, path string, payload map[string]any, extra map[string]string) (httpResult, error) {
	var raw []byte
	var err error
	if payload != nil {
		raw, err = json.Marshal(payload)
		if err != nil {
			return httpResult{}, err
		}
	}
	headers := map[string]string{}
	for key, value := range extra {
		headers[key] = value
	}
	if payload != nil {
		headers["Content-Type"] = "application/json"
	}
	return requestRaw(ctx, base, method, path, raw, headers)
}

func requestRaw(ctx context.Context, base, method, path string, payload []byte, extra map[string]string) (httpResult, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return httpResult{}, err
	}
	req.Header.Set("Accept", "application/json")
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return httpResult{}, err
	}
	decoded := map[string]any{}
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			decoded = map[string]any{"_non_json": true}
		}
	}
	return httpResult{Status: resp.StatusCode, Body: decoded, Raw: raw, Headers: resp.Header.Clone(), Cookies: resp.Cookies()}, nil
}

func issueCSRF(ctx context.Context, apiBase, cookie, expectedUserID string) (string, string, error) {
	me, err := requestRaw(ctx, apiBase, http.MethodGet, "/api/me", nil, map[string]string{"Cookie": cookie})
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

func unsafeHeaders(cookie, origin, csrf, correlation string) map[string]string {
	return map[string]string{
		"Cookie":       cookie,
		"Origin":       origin,
		"X-CSRF-Token": csrf,
		"X-Request-ID": correlation,
	}
}

func findSessionCookie(cookies []*http.Cookie) *http.Cookie {
	for _, cookie := range cookies {
		if cookie != nil && cookie.Name == "__Host-gojet_session" {
			return cookie
		}
	}
	return nil
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var value int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func uint64Value(value any) uint64 {
	switch typed := value.(type) {
	case float64:
		if typed >= 0 {
			return uint64(typed)
		}
	case json.Number:
		value, _ := typed.Int64()
		if value >= 0 {
			return uint64(value)
		}
	}
	return 0
}

func nestedString(body map[string]any, parent, child string) string {
	value, _ := body[parent].(map[string]any)
	return stringValue(value[child])
}

func nestedErrorCode(body map[string]any) string {
	if code := stringValue(body["code"]); code != "" {
		return code
	}
	if value, ok := body["error"].(map[string]any); ok {
		return stringValue(value["code"])
	}
	return ""
}
