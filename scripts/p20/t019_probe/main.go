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
	ExactHead            string         `json:"exact_head"`
	Errors               []string       `json:"errors"`
	Details              map[string]any `json:"details"`
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	exactHead := strings.TrimSpace(os.Getenv("P20_EXACT_HEAD"))
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	apiBase := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_P20_API_BASE")), "/")
	origin := strings.TrimSpace(os.Getenv("GOJET_AUTH_ALLOWED_ORIGIN"))
	outPath := strings.TrimSpace(os.Getenv("GOJET_P20_T019_PROBE_OUT"))
	if outPath == "" {
		outPath = "artifacts/v10/P20/runtime/t019/probe.json"
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
		fail("T019 probe runtime configuration is incomplete")
		return
	}

	t018, err := readEvidence("artifacts/v10/P20/p0/P20-T018.json")
	if err != nil || t018.Status != "PASS" || t018.ImplementationCommit != exactHead {
		fail("T019 requires same-run exact-head formal P20-T018 PASS evidence")
		return
	}
	t018Details := t018.Details
	userID := stringValue(t018Details["user_id"])
	workspaceID := stringValue(t018Details["workspace_id"])
	if userID == "" || workspaceID == "" || stringValue(t018Details["next_case"]) != "P20-T019" {
		fail("T019 requires P20-T018 to unlock exactly P20-T019 with correlated identity")
		return
	}
	result.Details["t018_evidence_bound"] = true
	result.Details["user_id"] = userID
	result.Details["workspace_id"] = workspaceID

	p06Cases := []string{"P06-T002", "P06-T009", "P06-T010", "P06-T011", "P06-T012", "P06-T013", "P06-T014", "P06-T018"}
	for _, caseID := range p06Cases {
		path := filepath.Join("artifacts", "v10", "P06", "results", caseID+".json")
		evidence, readErr := readEvidence(path)
		if readErr != nil || evidence.Status != "PASS" || evidence.ImplementationCommit != exactHead || len(evidence.Errors) != 0 {
			fail("T019 requires exact-head P06 predecessor PASS evidence: " + caseID)
			return
		}
	}
	result.Details["p06_entitlement_ticket_independence_proven"] = true
	result.Details["p06_ownership_dns_https_risk_axes_proven"] = true
	result.Details["p06_link_assignment_guard_proven"] = true

	for _, caseID := range []string{"P17-T008", "P17-T009"} {
		path := filepath.Join("artifacts", "v10", "P17", "domain", caseID+".json")
		evidence, readErr := readEvidence(path)
		if readErr != nil || evidence.Status != "PASS" || evidence.ExactHead != exactHead || len(evidence.Errors) != 0 {
			fail("T019 requires exact-head P17 predecessor PASS evidence: " + caseID)
			return
		}
	}
	result.Details["p17_ticket_manager_cannot_grant_entitlement"] = true
	result.Details["p17_entitlement_and_safety_conjunctive"] = true

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fail("T019 probe could not open MySQL")
		return
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		fail("T019 probe could not reach MySQL")
		return
	}

	var userStatus, workspaceRole string
	if err := db.QueryRowContext(ctx, `
		SELECT u.status,m.role
		FROM auth_users u JOIN workspace_memberships m ON m.user_id=u.id
		WHERE u.id=? AND m.workspace_id=?`, userID, workspaceID,
	).Scan(&userStatus, &workspaceRole); err != nil || userStatus != "active" || workspaceRole != "owner" {
		fail("T019 predecessor identity is not an active owner Workspace authority")
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
		fail("T019 probe timed out before the real authentication rate window elapsed")
		return
	case <-time.After(time.Duration(windowSeconds+1) * time.Second):
	}

	suffix := strings.ToLower(exactHead[:12])
	email := "p20-t009-" + suffix + "@example.test"
	login, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/auth/login", map[string]any{
		"email":          email,
		"password":       "P20-T009!Strong-Passphrase-2026",
		"correlation_id": "p20-t019-login-" + suffix,
	}, nil)
	if err != nil {
		fail("T019 probe real P15 login request failed")
		return
	}
	result.Details["login_http_status"] = login.Status
	if login.Status != http.StatusOK || stringValue(login.Body["status"]) != "authenticated" {
		result.Details["login_error_code"] = nestedErrorCode(login.Body)
		fail("T019 probe could not establish the real authenticated Domain session")
		return
	}
	sessionCookie := findSessionCookie(login.Cookies)
	if sessionCookie == nil || strings.TrimSpace(sessionCookie.Value) == "" {
		fail("T019 real login did not issue the signed session cookie")
		return
	}
	cookieHeader := "__Host-gojet_session=" + sessionCookie.Value
	csrf, sessionID, err := issueCSRF(ctx, apiBase, cookieHeader, userID)
	if err != nil {
		fail("T019 real session is not accepted by /api/me")
		return
	}
	result.Details["real_session_authenticated"] = true
	result.Details["csrf_authority_issued"] = csrf != ""
	result.Details["session_id_present"] = sessionID != ""

	// Seed only the canonical P06 business prerequisite. This fixture grants no
	// authentication, Workspace membership, CSRF, ownership, DNS, HTTPS or risk
	// authority and cannot hide a production authentication-boundary failure.
	now := time.Now().UTC()
	_, err = db.ExecContext(ctx, `
		INSERT INTO custom_domain_entitlement_sources
		(workspace_id,source,source_key,status,domain_limit,starts_at,expires_at,granted_by,support_ticket_id,decision_reason)
		VALUES (?, 'manual_approval', ?, 'active', 3, ?, ?, 'p20-t019-fixture', ?, 'P20 T019 canonical entitlement prerequisite')`,
		workspaceID, "p20-t019-"+suffix, now.Add(-time.Minute), now.Add(time.Hour), "p20-t019-ticket-"+suffix,
	)
	if err != nil {
		fail("T019 could not establish the canonical P06 entitlement prerequisite")
		return
	}
	result.Details["canonical_entitlement_fixture"] = true

	rowsBefore, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domains WHERE workspace_id=? AND removed_at IS NULL`, workspaceID)
	if err != nil {
		fail("T019 could not inspect pre-create custom-domain row count")
		return
	}
	result.Details["domain_rows_before"] = rowsBefore

	hostname := "p20-t019-" + suffix + ".example.com"
	created, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/workspaces/"+workspaceID+"/domains", map[string]any{
		"hostname":      hostname,
		"change_reason": "P20 T019 real custom-domain create",
	}, unsafeHeaders(cookieHeader, origin, csrf, "p20-t019-create-"+suffix))
	if err != nil {
		fail("T019 real custom-domain create request failed")
		return
	}
	rowsAfter, _ := scalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domains WHERE workspace_id=? AND removed_at IS NULL`, workspaceID)
	result.Details["domain_create_http_status"] = created.Status
	result.Details["domain_create_error_code"] = nestedErrorCode(created.Body)
	result.Details["domain_create_row_delta"] = rowsAfter - rowsBefore
	if created.Status != http.StatusCreated {
		result.Details["domain_create_failed_without_write"] = rowsAfter == rowsBefore
		if created.Status == http.StatusServiceUnavailable && nestedErrorCode(created.Body) == "auth_dependency_unavailable" {
			fail("real authenticated session is not accepted as custom-domain API mutation authority")
		} else {
			fail(fmt.Sprintf("T019 custom-domain create reached business validation but failed with HTTP %d code=%s", created.Status, nestedErrorCode(created.Body)))
		}
		return
	}

	// Reaching this point means the production authentication defect is no longer
	// reproduced. The discovery probe deliberately does not fabricate downstream
	// DNS/TLS/risk observations; the frozen P06/P17 predecessor evidence above
	// remains authoritative until T019 is expanded into its remediation/formal
	// lifecycle case.
	result.Details["domain_create_identity_bound"] = stringValue(created.Body["workspace_id"]) == workspaceID
	result.Details["domain_hostname"] = hostname
	fail("T019 discovery reached custom-domain create; extend the case through the real P06 ownership/DNS/HTTPS/risk workflow before claiming PASS")
}

func readEvidence(path string) (predecessorEvidence, error) {
	var value predecessorEvidence
	raw, err := os.ReadFile(path)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, err
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
