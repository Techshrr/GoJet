package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
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

type p09Evidence struct {
	Status               string         `json:"status"`
	ImplementationCommit string         `json:"implementation_commit"`
	Errors               []string       `json:"errors"`
	Observations         map[string]any `json:"observations"`
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

const fixture = "GoJet P20 T016 real file fixture.\n"

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	exactHead := strings.TrimSpace(os.Getenv("P20_EXACT_HEAD"))
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	apiBase := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_P20_API_BASE")), "/")
	origin := strings.TrimSpace(os.Getenv("GOJET_AUTH_ALLOWED_ORIGIN"))
	workerBin := strings.TrimSpace(os.Getenv("GOJET_P20_FILEWORKER_BIN"))
	storageRoot := strings.TrimSpace(os.Getenv("GOJET_FILE_STORAGE_ROOT"))
	outPath := strings.TrimSpace(os.Getenv("GOJET_P20_T016_PROBE_OUT"))
	if outPath == "" {
		outPath = "artifacts/v10/P20/runtime/t016/probe.json"
	}
	artifactDir := filepath.Dir(outPath)

	result := probe{
		Status:               "FAIL",
		ImplementationCommit: exactHead,
		Errors:               []string{},
		Details: map[string]any{
			"real_mysql":                 true,
			"real_platform_api":          true,
			"real_clamd_preflight":       false,
			"native_fileworker":          false,
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
		_ = os.MkdirAll(artifactDir, 0o755)
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

	if len(exactHead) != 40 || dsn == "" || apiBase == "" || origin == "" || workerBin == "" || storageRoot == "" {
		fail("T016 probe runtime configuration is incomplete")
		return
	}

	t015, err := readT015("artifacts/v10/P20/p0/P20-T015.json", exactHead)
	if err != nil || t015.Details.UserID == "" || t015.Details.WorkspaceID == "" || t015.Details.NextCase != "P20-T016" {
		fail("T016 probe requires same-run exact-head P20-T015 PASS evidence with next_case=P20-T016")
		return
	}
	result.Details["t015_evidence_bound"] = true
	result.Details["user_id"] = t015.Details.UserID
	result.Details["workspace_id"] = t015.Details.WorkspaceID

	preflight, err := readP09Preflight(exactHead)
	if err != nil {
		fail(err.Error())
		return
	}
	for key, value := range preflight {
		result.Details[key] = value
	}
	result.Details["real_clamd_preflight"] = true

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fail("T016 probe could not open MySQL")
		return
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		fail("T016 probe could not reach MySQL")
		return
	}

	var userStatus, workspaceRole string
	if err := db.QueryRowContext(ctx, `
		SELECT u.status,m.role
		FROM auth_users u JOIN workspace_memberships m ON m.user_id=u.id
		WHERE u.id=? AND m.workspace_id=?`, t015.Details.UserID, t015.Details.WorkspaceID,
	).Scan(&userStatus, &workspaceRole); err != nil || userStatus != "active" || workspaceRole != "owner" {
		fail("T016 predecessor identity is not an active owner Workspace authority")
		return
	}
	result.Details["workspace_role"] = workspaceRole

	suffix := strings.ToLower(exactHead[:12])
	email := "p20-t009-" + suffix + "@example.test"
	login, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/auth/login", map[string]any{
		"email":          email,
		"password":       "P20-T009!Strong-Passphrase-2026",
		"correlation_id": "p20-t016-login-" + suffix,
	}, nil)
	if err != nil {
		fail("T016 probe real P15 login request failed")
		return
	}
	result.Details["login_http_status"] = login.Status
	if login.Status != http.StatusOK || stringValue(login.Body["status"]) != "authenticated" {
		result.Details["login_error_code"] = nestedErrorCode(login.Body)
		fail("T016 probe could not establish the real authenticated Files session")
		return
	}
	sessionCookie := findSessionCookie(login.Cookies)
	if sessionCookie == nil || strings.TrimSpace(sessionCookie.Value) == "" {
		fail("T016 real login did not issue the signed session cookie")
		return
	}
	cookieHeader := "__Host-gojet_session=" + sessionCookie.Value
	csrf, sessionID, err := issueCSRF(ctx, apiBase, cookieHeader, t015.Details.UserID)
	if err != nil {
		fail("T016 real session is not accepted by /api/me")
		return
	}
	result.Details["real_session_authenticated"] = true
	result.Details["csrf_authority_issued"] = csrf != ""
	result.Details["session_id_present"] = sessionID != ""

	rowsBefore, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM files WHERE workspace_id=? AND deleted_at IS NULL`, t015.Details.WorkspaceID)
	if err != nil {
		fail("T016 probe could not inspect pre-upload file row count")
		return
	}
	result.Details["file_rows_before"] = rowsBefore

	upload, err := uploadFile(ctx, apiBase, t015.Details.WorkspaceID, cookieHeader, origin, csrf, "p20-t016-upload-"+suffix)
	if err != nil {
		fail("T016 real Files multipart upload request failed")
		return
	}
	rowsAfter, _ := scalarInt(ctx, db, `SELECT COUNT(*) FROM files WHERE workspace_id=? AND deleted_at IS NULL`, t015.Details.WorkspaceID)
	result.Details["file_create_http_status"] = upload.Status
	result.Details["file_create_error_code"] = nestedErrorCode(upload.Body)
	result.Details["file_create_row_delta"] = rowsAfter - rowsBefore
	if upload.Status != http.StatusCreated {
		result.Details["file_create_failed_without_write"] = rowsAfter == rowsBefore
		fail("real authenticated session is not accepted as Files API mutation authority")
		return
	}

	fileID := uint64Value(upload.Body["id"])
	if fileID == 0 || stringValue(upload.Body["workspace_id"]) != t015.Details.WorkspaceID || stringValue(upload.Body["scan_state"]) != "quarantined" || boolValue(upload.Body["published"]) {
		fail("T016 upload did not create the expected quarantined unpublished file resource")
		return
	}
	result.Details["file_id"] = fileID
	result.Details["upload_quarantined"] = true
	result.Details["upload_unpublished"] = true

	var dbWorkspace, storageKey, contentSHA, createdBy, scanState, publicSlug string
	var published int
	if err := db.QueryRowContext(ctx, `
		SELECT workspace_id,storage_key,content_sha256,created_by,scan_state,published,public_slug
		FROM files WHERE id=?`, fileID,
	).Scan(&dbWorkspace, &storageKey, &contentSHA, &createdBy, &scanState, &published, &publicSlug); err != nil {
		fail("T016 could not inspect uploaded file authority in MySQL")
		return
	}
	identityBound := dbWorkspace == t015.Details.WorkspaceID && createdBy == t015.Details.UserID && scanState == "quarantined" && published == 0 && len(storageKey) == 64
	result.Details["mysql_file_identity_bound"] = identityBound
	if !identityBound {
		fail("T016 MySQL file identity does not match the real-session upload authority")
		return
	}

	quarantinePath := storagePath(storageRoot, "quarantine", storageKey)
	if raw, err := os.ReadFile(quarantinePath); err != nil || string(raw) != fixture || digest(raw) != contentSHA {
		fail("T016 quarantine bytes are missing or do not match the authoritative digest")
		return
	}
	result.Details["quarantine_bytes_bound"] = true

	preDownload, err := requestRaw(ctx, apiBase, http.MethodGet, fmt.Sprintf("/api/workspaces/%s/files/%d/download", t015.Details.WorkspaceID, fileID), nil, map[string]string{"Cookie": cookieHeader})
	if err != nil {
		fail("T016 pre-scan workspace download request failed")
		return
	}
	result.Details["pre_scan_download_http_status"] = preDownload.Status
	result.Details["pre_scan_download_error_code"] = nestedErrorCode(preDownload.Body)
	if preDownload.Status != http.StatusConflict || nestedErrorCode(preDownload.Body) != "file_not_safe" || bytes.Contains(preDownload.Raw, []byte(fixture)) {
		fail("T016 pre-scan workspace download did not fail closed")
		return
	}
	result.Details["pre_scan_download_denied"] = true

	publicBefore, err := requestRaw(ctx, apiBase, http.MethodGet, "/api/public/files/"+publicSlug, nil, nil)
	if err != nil {
		fail("T016 pre-publish public download request failed")
		return
	}
	result.Details["pre_publish_public_http_status"] = publicBefore.Status
	if publicBefore.Status != http.StatusForbidden || bytes.Contains(publicBefore.Raw, []byte(fixture)) {
		fail("T016 pre-publish public download did not fail closed")
		return
	}
	result.Details["pre_publish_public_denied"] = true

	publishCSRF, _, err := issueCSRF(ctx, apiBase, cookieHeader, t015.Details.UserID)
	if err != nil {
		fail("T016 could not issue one-time CSRF for pre-scan publish denial")
		return
	}
	prePublish, err := requestJSON(ctx, apiBase, http.MethodPost, fmt.Sprintf("/api/workspaces/%s/files/%d/publish", t015.Details.WorkspaceID, fileID), map[string]any{
		"change_reason": "P20 T016 pre-scan publish denial",
	}, unsafeHeaders(cookieHeader, origin, publishCSRF, "p20-t016-prepublish-"+suffix))
	if err != nil {
		fail("T016 pre-scan publish request failed")
		return
	}
	result.Details["pre_scan_publish_http_status"] = prePublish.Status
	result.Details["pre_scan_publish_error_code"] = nestedErrorCode(prePublish.Body)
	if prePublish.Status != http.StatusConflict || nestedErrorCode(prePublish.Body) != "file_not_safe" {
		fail("T016 pre-scan publish did not fail closed")
		return
	}
	result.Details["pre_scan_publish_denied"] = true

	worker := exec.CommandContext(ctx, workerBin)
	worker.Env = append(os.Environ(),
		"GOJET_FILE_WORKER_MAX_JOBS=1",
		"GOJET_FILE_WORKER_ID=p20-t016-"+suffix,
		"GOJET_FILE_WORKER_POLL_INTERVAL=50ms",
		"GOJET_FILE_SCAN_CLAIM_LEASE=2s",
	)
	workerOutput, workerErr := worker.CombinedOutput()
	_ = os.MkdirAll(artifactDir, 0o755)
	_ = os.WriteFile(filepath.Join(artifactDir, "fileworker.log"), workerOutput, 0o644)
	result.Details["fileworker_exit_code"] = exitCode(workerErr)
	if workerErr != nil {
		fail("T016 native fileworker did not complete the queued scan")
		return
	}
	result.Details["native_fileworker"] = true

	var attemptStatus, engineVersion, signatureVersion, verdictCode, errorCode string
	if err := db.QueryRowContext(ctx, `
		SELECT status,COALESCE(engine_version,''),COALESCE(signature_version,''),COALESCE(verdict_code,''),COALESCE(error_code,'')
		FROM file_scan_attempts WHERE file_id=? ORDER BY generation DESC LIMIT 1`, fileID,
	).Scan(&attemptStatus, &engineVersion, &signatureVersion, &verdictCode, &errorCode); err != nil {
		fail("T016 could not inspect the real fileworker scan attempt")
		return
	}
	if err := db.QueryRowContext(ctx, `SELECT scan_state,published FROM files WHERE id=?`, fileID).Scan(&scanState, &published); err != nil {
		fail("T016 could not inspect post-scan file state")
		return
	}
	result.Details["scan_status"] = attemptStatus
	result.Details["scan_state"] = scanState
	result.Details["scan_engine_version"] = engineVersion
	result.Details["scan_signature_version"] = signatureVersion
	result.Details["scan_verdict_code"] = verdictCode
	result.Details["scan_error_code"] = errorCode
	if attemptStatus != "clean" || scanState != "safe" || published != 0 || engineVersion == "" || signatureVersion == "" || errorCode != "" {
		fail("T016 native fileworker did not produce a current real ClamAV clean verdict")
		return
	}
	result.Details["real_clamav_scan"] = true
	result.Details["scan_safe"] = true

	finalCSRF, _, err := issueCSRF(ctx, apiBase, cookieHeader, t015.Details.UserID)
	if err != nil {
		fail("T016 could not issue one-time CSRF for final publish")
		return
	}
	publishedResult, err := requestJSON(ctx, apiBase, http.MethodPost, fmt.Sprintf("/api/workspaces/%s/files/%d/publish", t015.Details.WorkspaceID, fileID), map[string]any{
		"change_reason": "P20 T016 publish after real clean scan",
	}, unsafeHeaders(cookieHeader, origin, finalCSRF, "p20-t016-publish-"+suffix))
	if err != nil {
		fail("T016 final publish request failed")
		return
	}
	result.Details["publish_http_status"] = publishedResult.Status
	if publishedResult.Status != http.StatusOK || !boolValue(publishedResult.Body["published"]) || stringValue(publishedResult.Body["scan_state"]) != "safe" {
		fail("T016 current safe scan verdict did not authorize publication")
		return
	}
	result.Details["publish_authorized"] = true

	publishedPath := storagePath(storageRoot, "published", storageKey)
	if raw, err := os.ReadFile(publishedPath); err != nil || string(raw) != fixture || digest(raw) != contentSHA {
		fail("T016 published bytes are missing or do not preserve the authoritative digest")
		return
	}
	if _, err := os.Stat(quarantinePath); !os.IsNotExist(err) {
		fail("T016 publication did not move bytes out of quarantine")
		return
	}
	result.Details["published_storage_bound"] = true

	workspaceDownload, err := requestRaw(ctx, apiBase, http.MethodGet, fmt.Sprintf("/api/workspaces/%s/files/%d/download", t015.Details.WorkspaceID, fileID), nil, map[string]string{"Cookie": cookieHeader})
	if err != nil || workspaceDownload.Status != http.StatusOK || string(workspaceDownload.Raw) != fixture || digest(workspaceDownload.Raw) != contentSHA {
		fail("T016 authenticated download does not match the published authoritative bytes")
		return
	}
	result.Details["workspace_download_http_status"] = workspaceDownload.Status
	result.Details["workspace_download_digest_matches"] = true

	publicDownload, err := requestRaw(ctx, apiBase, http.MethodGet, "/api/public/files/"+publicSlug, nil, nil)
	if err != nil || publicDownload.Status != http.StatusOK || string(publicDownload.Raw) != fixture || digest(publicDownload.Raw) != contentSHA {
		fail("T016 public download does not match the published authoritative bytes")
		return
	}
	result.Details["public_download_http_status"] = publicDownload.Status
	result.Details["public_download_digest_matches"] = true
	result.Details["file_create_row_delta"] = rowsAfter - rowsBefore
	result.Details["next_case"] = "P20-T017"

	finish()
}

func readT015(path, exactHead string) (predecessorEvidence, error) {
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

func readP09Preflight(exactHead string) (map[string]any, error) {
	details := map[string]any{"p09_clamav_preflight_bound": false, "p09_fail_closed_preflight": false}
	failClosed := true
	for number := 5; number <= 10; number++ {
		caseID := fmt.Sprintf("P09-T%03d", number)
		path := filepath.Join("artifacts", "v10", "P09", "clamav", caseID+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("T016 requires same-run %s evidence", caseID)
		}
		var evidence p09Evidence
		if err := json.Unmarshal(raw, &evidence); err != nil || evidence.Status != "PASS" || evidence.ImplementationCommit != exactHead || len(evidence.Errors) != 0 {
			return nil, fmt.Errorf("T016 %s evidence is not exact-head PASS", caseID)
		}
		details[strings.ToLower(strings.ReplaceAll(caseID, "-", "_"))+"_bound"] = true
		if number == 5 {
			engine := stringValue(evidence.Observations["engine_version"])
			signature := stringValue(evidence.Observations["signature_version"])
			if engine == "" || signature == "" || stringValue(evidence.Observations["scan_state"]) != "safe" {
				return nil, fmt.Errorf("T016 P09-T005 lacks real clean ClamAV authority")
			}
			details["p09_clean_engine_version"] = engine
			details["p09_clean_signature_version"] = signature
		}
		if number == 6 {
			if stringValue(evidence.Observations["scan_state"]) != "blocked" || stringValue(evidence.Observations["scan_status"]) != "infected" {
				return nil, fmt.Errorf("T016 P09-T006 lacks real blocked ClamAV authority")
			}
		}
		if number >= 7 {
			if stringValue(evidence.Observations["scan_state"]) != "scan_error" {
				failClosed = false
			}
		}
	}
	if !failClosed {
		return nil, fmt.Errorf("T016 P09 fail-closed predecessor authority is incomplete")
	}
	details["p09_clamav_preflight_bound"] = true
	details["p09_fail_closed_preflight"] = true
	return details, nil
}

func uploadFile(ctx context.Context, apiBase, workspaceID, cookie, origin, csrf, correlation string) (httpResult, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("change_reason", "P20 T016 real file upload"); err != nil {
		return httpResult{}, err
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="p20-t016.txt"`)
	header.Set("Content-Type", "text/plain")
	part, err := writer.CreatePart(header)
	if err != nil {
		return httpResult{}, err
	}
	if _, err := io.WriteString(part, fixture); err != nil {
		return httpResult{}, err
	}
	if err := writer.Close(); err != nil {
		return httpResult{}, err
	}
	headers := unsafeHeaders(cookie, origin, csrf, correlation)
	headers["Content-Type"] = writer.FormDataContentType()
	return requestRaw(ctx, apiBase, http.MethodPost, "/api/workspaces/"+workspaceID+"/files", body.Bytes(), headers)
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

func storagePath(root, kind, key string) string {
	return filepath.Join(root, kind, key[:2], key[2:4], key)
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var value int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
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

func boolValue(value any) bool {
	boolean, _ := value.(bool)
	return boolean
}

func uint64Value(value any) uint64 {
	typed, ok := value.(float64)
	if !ok || typed <= 0 || typed != float64(uint64(typed)) {
		return 0
	}
	return uint64(typed)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
