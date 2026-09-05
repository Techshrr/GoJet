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
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type predecessorEvidence struct {
	Status               string         `json:"status"`
	ImplementationCommit string         `json:"implementation_commit"`
	Errors               []string       `json:"errors"`
	Details              map[string]any `json:"details"`
}

type integrationSummary struct {
	Status               string `json:"status"`
	ImplementationCommit string `json:"implementation_commit"`
	CaseRange            string `json:"case_range"`
	Errors               []string `json:"errors"`
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

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	exactHead := strings.TrimSpace(os.Getenv("P20_EXACT_HEAD"))
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	apiBase := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_P20_API_BASE")), "/")
	origin := strings.TrimSpace(os.Getenv("GOJET_AUTH_ALLOWED_ORIGIN"))
	outPath := strings.TrimSpace(os.Getenv("GOJET_P20_T020_PROBE_OUT"))
	if outPath == "" {
		outPath = "artifacts/v10/P20/runtime/t020/probe.json"
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
			"p14_real_integration_bound": false,
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
		fail("T020 probe runtime configuration is incomplete")
		return
	}

	t019, err := readEvidence("artifacts/v10/P20/p0/P20-T019.json")
	if err != nil || t019.Status != "PASS" || t019.ImplementationCommit != exactHead || len(t019.Errors) != 0 {
		fail("T020 requires same-run exact-head P20-T019 PASS evidence")
		return
	}
	t019Details := t019.Details
	userID := stringValue(t019Details["user_id"])
	workspaceID := stringValue(t019Details["workspace_id"])
	if userID == "" || workspaceID == "" || stringValue(t019Details["next_case"]) != "P20-T020" {
		fail("T020 requires P20-T019 to unlock exactly P20-T020 with correlated identity")
		return
	}
	result.Details["t019_evidence_bound"] = true
	result.Details["user_id"] = userID
	result.Details["workspace_id"] = workspaceID

	var p14 integrationSummary
	if err := readJSON("artifacts/v10/P14/results/integration-summary.json", &p14); err != nil || p14.Status != "PASS" || p14.ImplementationCommit != exactHead || p14.CaseRange != "P14-T001..P14-T021" || len(p14.Errors) != 0 {
		fail("T020 requires exact-head P14-T001..P14-T021 real integration predecessor authority")
		return
	}
	result.Details["p14_real_integration_bound"] = true
	result.Details["p14_ticket_lifecycle_proven"] = true
	result.Details["p14_attachment_clamav_proven"] = true
	result.Details["p14_turnstile_proven"] = true
	result.Details["p14_mail_smtp_retry_proven"] = true
	result.Details["p14_admin_ticket_mail_permission_proven"] = true
	result.Details["p14_audit_correlation_proven"] = true

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fail("T020 probe could not open MySQL")
		return
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		fail("T020 probe could not reach MySQL")
		return
	}

	var userStatus, workspaceRole string
	if err := db.QueryRowContext(ctx, `
		SELECT u.status,m.role
		FROM auth_users u JOIN workspace_memberships m ON m.user_id=u.id
		WHERE u.id=? AND m.workspace_id=?`, userID, workspaceID,
	).Scan(&userStatus, &workspaceRole); err != nil || userStatus != "active" || workspaceRole != "owner" {
		fail("T020 predecessor identity is not an active owner Workspace authority")
		return
	}
	result.Details["workspace_role"] = workspaceRole

	windowSeconds := 60
	if raw := strings.TrimSpace(os.Getenv("GOJET_AUTH_RATE_WINDOW_SECONDS")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 && parsed <= 300 {
			windowSeconds = parsed
		}
	}
	result.Details["auth_rate_window_seconds"] = windowSeconds
	result.Details["auth_rate_window_respected"] = true
	select {
	case <-ctx.Done():
		fail("T020 probe timed out before the real authentication rate window elapsed")
		return
	case <-time.After(time.Duration(windowSeconds+1) * time.Second):
	}

	suffix := strings.ToLower(exactHead[:12])
	email := "p20-t009-" + suffix + "@example.test"
	login, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/auth/login", map[string]any{
		"email":          email,
		"password":       "P20-T009!Strong-Passphrase-2026",
		"correlation_id": "p20-t020-login-" + suffix,
	}, nil)
	if err != nil {
		fail("T020 probe real P15 login request failed")
		return
	}
	result.Details["login_http_status"] = login.Status
	if login.Status != http.StatusOK || stringValue(login.Body["status"]) != "authenticated" {
		result.Details["login_error_code"] = nestedErrorCode(login.Body)
		fail("T020 probe could not establish the real authenticated Support session")
		return
	}
	sessionCookie := findSessionCookie(login.Cookies)
	if sessionCookie == nil || strings.TrimSpace(sessionCookie.Value) == "" {
		fail("T020 real login did not issue the signed session cookie")
		return
	}
	cookieHeader := "__Host-gojet_session=" + sessionCookie.Value
	csrf, sessionID, err := issueCSRF(ctx, apiBase, cookieHeader, userID)
	if err != nil {
		fail("T020 real session is not accepted by /api/me")
		return
	}
	result.Details["real_session_authenticated"] = true
	result.Details["csrf_authority_issued"] = csrf != ""
	result.Details["session_id_present"] = sessionID != ""

	rowsBefore, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM support_tickets WHERE workspace_id=?`, workspaceID)
	if err != nil {
		fail("T020 could not inspect pre-create support-ticket row count")
		return
	}
	messagesBefore, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM support_ticket_messages WHERE ticket_id IN (SELECT id FROM support_tickets WHERE workspace_id=?)`, workspaceID)
	if err != nil {
		fail("T020 could not inspect pre-create support-message row count")
		return
	}
	result.Details["ticket_rows_before"] = rowsBefore
	result.Details["ticket_message_rows_before"] = messagesBefore

	created, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/support/tickets", map[string]any{
		"workspace_id":    workspaceID,
		"category":        "general",
		"subject":         "P20 T020 real support ticket",
		"message":         "P20 T020 correlated requester lifecycle probe.",
		"turnstile_token": "p20-t020-ci-protected-submission",
	}, mergeHeaders(unsafeHeaders(cookieHeader, origin, csrf, "p20-t020-create-"+suffix), map[string]string{
		"Idempotency-Key": "p20-t020-create-" + suffix,
	}))
	if err != nil {
		fail("T020 real support-ticket create request failed")
		return
	}
	rowsAfter, _ := scalarInt(ctx, db, `SELECT COUNT(*) FROM support_tickets WHERE workspace_id=?`, workspaceID)
	messagesAfter, _ := scalarInt(ctx, db, `SELECT COUNT(*) FROM support_ticket_messages WHERE ticket_id IN (SELECT id FROM support_tickets WHERE workspace_id=?)`, workspaceID)
	result.Details["ticket_create_http_status"] = created.Status
	result.Details["ticket_create_error_code"] = nestedErrorCode(created.Body)
	result.Details["ticket_create_row_delta"] = rowsAfter - rowsBefore
	result.Details["ticket_message_row_delta"] = messagesAfter - messagesBefore
	if created.Status != http.StatusCreated {
		result.Details["ticket_create_failed_without_write"] = rowsAfter == rowsBefore && messagesAfter == messagesBefore
		fail("real authenticated session is not accepted as Support API requester authority")
		return
	}

	// Discovery intentionally stops here. If requester create succeeds, T020 must
	// still prove requester reply, real P17 Admin reply/permission authority,
	// durable mailworker SMTP delivery and attachment ClamAV fail-closed behavior
	// in one correlated timeline before the frozen case may report PASS.
	result.Details["ticket_create_identity_bound"] = nestedString(created.Body, "ticket", "workspace_id") == workspaceID
	result.Details["ticket_id_present"] = nestedString(created.Body, "ticket", "id") != ""
	fail("T020 discovery reached support-ticket create; extend through requester reply, real Admin reply, mailworker and attachment safety before claiming PASS")
}

func readEvidence(path string) (predecessorEvidence, error) {
	var value predecessorEvidence
	return value, readJSON(path, &value)
}

func readJSON(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
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

func mergeHeaders(primary, secondary map[string]string) map[string]string {
	merged := map[string]string{}
	for key, value := range primary {
		merged[key] = value
	}
	for key, value := range secondary {
		merged[key] = value
	}
	return merged
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
