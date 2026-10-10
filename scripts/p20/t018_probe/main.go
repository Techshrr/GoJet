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
	"github.com/redis/go-redis/v9"
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

type riskDecision struct {
	SchemaVersion int    `json:"schema_version"`
	Decision      string `json:"decision"`
	Fingerprint   string `json:"fingerprint"`
	CheckedAt     string `json:"checked_at"`
	ValidUntil    string `json:"valid_until"`
	PolicyVersion string `json:"policy_version"`
}

const (
	bioURLV1 = "https://example.com/p20-t018-a"
	bioURLV2 = "https://example.com/p20-t018-b"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	exactHead := strings.TrimSpace(os.Getenv("P20_EXACT_HEAD"))
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	apiBase := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_P20_API_BASE")), "/")
	origin := strings.TrimSpace(os.Getenv("GOJET_AUTH_ALLOWED_ORIGIN"))
	outPath := strings.TrimSpace(os.Getenv("GOJET_P20_T018_PROBE_OUT"))
	if outPath == "" {
		outPath = "artifacts/v10/P20/runtime/t018/probe.json"
	}

	result := probe{
		Status:               "FAIL",
		ImplementationCommit: exactHead,
		Errors:               []string{},
		Details: map[string]any{
			"real_mysql":                 true,
			"real_redis":                 true,
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

	if len(exactHead) != 40 || dsn == "" || redisAddr == "" || apiBase == "" || origin == "" {
		fail("T018 probe runtime configuration is incomplete")
		return
	}

	t017, err := readT017("artifacts/v10/P20/p0/P20-T017.json", exactHead)
	if err != nil || t017.Details.UserID == "" || t017.Details.WorkspaceID == "" || t017.Details.NextCase != "P20-T018" {
		fail("T018 probe requires same-run exact-head formal P20-T017 PASS evidence with next_case=P20-T018")
		return
	}
	result.Details["t017_evidence_bound"] = true
	result.Details["user_id"] = t017.Details.UserID
	result.Details["workspace_id"] = t017.Details.WorkspaceID

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fail("T018 probe could not open MySQL")
		return
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		fail("T018 probe could not reach MySQL")
		return
	}

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		fail("T018 probe could not reach Redis")
		return
	}

	var userStatus, workspaceRole string
	if err := db.QueryRowContext(ctx, `
		SELECT u.status,m.role
		FROM auth_users u JOIN workspace_memberships m ON m.user_id=u.id
		WHERE u.id=? AND m.workspace_id=?`, t017.Details.UserID, t017.Details.WorkspaceID,
	).Scan(&userStatus, &workspaceRole); err != nil || userStatus != "active" || workspaceRole != "owner" {
		fail("T018 predecessor identity is not an active owner Workspace authority")
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
		fail("T018 probe timed out before the real authentication rate window elapsed")
		return
	case <-time.After(time.Duration(windowSeconds+1) * time.Second):
	}

	suffix := strings.ToLower(exactHead[:12])
	email := "p20-t009-" + suffix + "@example.test"
	login, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/auth/login", map[string]any{
		"email":          email,
		"password":       "P20-T009!Strong-Passphrase-2026",
		"correlation_id": "p20-t018-login-" + suffix,
	}, nil)
	if err != nil {
		fail("T018 probe real P15 login request failed")
		return
	}
	result.Details["login_http_status"] = login.Status
	if login.Status != http.StatusOK || stringValue(login.Body["status"]) != "authenticated" {
		result.Details["login_error_code"] = nestedErrorCode(login.Body)
		fail("T018 probe could not establish the real authenticated Bio session")
		return
	}
	sessionCookie := findSessionCookie(login.Cookies)
	if sessionCookie == nil || strings.TrimSpace(sessionCookie.Value) == "" {
		fail("T018 real login did not issue the signed session cookie")
		return
	}
	cookieHeader := "__Host-gojet_session=" + sessionCookie.Value
	csrf, sessionID, err := issueCSRF(ctx, apiBase, cookieHeader, t017.Details.UserID)
	if err != nil {
		fail("T018 real session is not accepted by /api/me")
		return
	}
	result.Details["real_session_authenticated"] = true
	result.Details["csrf_authority_issued"] = csrf != ""
	result.Details["session_id_present"] = sessionID != ""

	rowsBefore, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM bio_pages WHERE workspace_id=? AND deleted_at IS NULL`, t017.Details.WorkspaceID)
	if err != nil {
		fail("T018 probe could not inspect pre-create Bio row count")
		return
	}
	result.Details["bio_rows_before"] = rowsBefore

	created, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/workspaces/"+t017.Details.WorkspaceID+"/bio-pages", map[string]any{
		"title": "P20 T018 real Bio",
		"bio":   "GoJet P20 T018 real Bio lifecycle fixture.",
		"links": []map[string]any{{
			"position":        0,
			"label":           "Primary",
			"destination_url": bioURLV1,
		}},
		"change_reason": "P20 T018 real Bio create",
	}, unsafeHeaders(cookieHeader, origin, csrf, "p20-t018-create-"+suffix))
	if err != nil {
		fail("T018 real Bio create request failed")
		return
	}
	rowsAfter, _ := scalarInt(ctx, db, `SELECT COUNT(*) FROM bio_pages WHERE workspace_id=? AND deleted_at IS NULL`, t017.Details.WorkspaceID)
	result.Details["bio_create_http_status"] = created.Status
	result.Details["bio_create_error_code"] = nestedErrorCode(created.Body)
	result.Details["bio_create_row_delta"] = rowsAfter - rowsBefore
	if created.Status != http.StatusCreated {
		result.Details["bio_create_failed_without_write"] = rowsAfter == rowsBefore
		fail("real authenticated session is not accepted as Bio API mutation authority")
		return
	}

	pageID := uint64Value(created.Body["id"])
	slug := stringValue(created.Body["slug"])
	version := uint64Value(created.Body["version"])
	child := firstMap(created.Body["links"])
	childID := uint64Value(child["id"])
	fingerprintV1 := stringValue(child["destination_fingerprint"])
	if pageID == 0 || slug == "" || version != 1 || childID == 0 || fingerprintV1 == "" || stringValue(created.Body["workspace_id"]) != t017.Details.WorkspaceID {
		fail("T018 create did not return the expected Bio and child identity")
		return
	}
	result.Details["bio_id"] = pageID
	result.Details["bio_slug_present"] = true
	result.Details["bio_child_id"] = childID
	result.Details["bio_create_identity_bound"] = true

	prePublishCSRF, _, err := issueCSRF(ctx, apiBase, cookieHeader, t017.Details.UserID)
	if err != nil {
		fail("T018 could not issue one-time CSRF for unresolved-risk publish")
		return
	}
	prePublish, err := requestJSON(ctx, apiBase, http.MethodPost, fmt.Sprintf("/api/workspaces/%s/bio-pages/%d/publish", t017.Details.WorkspaceID, pageID), map[string]any{
		"expected_version": version,
		"change_reason":    "P20 T018 unresolved risk must fail closed",
	}, unsafeHeaders(cookieHeader, origin, prePublishCSRF, "p20-t018-prepublish-"+suffix))
	if err != nil {
		fail("T018 unresolved-risk publish request failed")
		return
	}
	result.Details["unresolved_publish_http_status"] = prePublish.Status
	result.Details["unresolved_publish_error_code"] = nestedErrorCode(prePublish.Body)
	if prePublish.Status != http.StatusConflict || nestedErrorCode(prePublish.Body) != "child_link_risk_unresolved" {
		fail("T018 unresolved Bio child risk did not fail closed before publish")
		return
	}

	workspaceRead, err := requestRaw(ctx, apiBase, http.MethodGet, fmt.Sprintf("/api/workspaces/%s/bio-pages/%d", t017.Details.WorkspaceID, pageID), nil, map[string]string{"Cookie": cookieHeader})
	if err != nil || workspaceRead.Status != http.StatusOK || stringValue(workspaceRead.Body["status"]) != "draft" || uint64Value(workspaceRead.Body["version"]) != version {
		fail("T018 failed publish changed authoritative Bio lifecycle state")
		return
	}
	result.Details["unresolved_publish_state_unchanged"] = true

	if err := seedRisk(ctx, redisClient, childID, fingerprintV1, "allow"); err != nil {
		fail("T018 could not seed the predecessor-compatible allow risk decision")
		return
	}
	result.Details["risk_allow_seeded"] = true

	publishCSRF, _, err := issueCSRF(ctx, apiBase, cookieHeader, t017.Details.UserID)
	if err != nil {
		fail("T018 could not issue one-time CSRF for Bio publish")
		return
	}
	published, err := requestJSON(ctx, apiBase, http.MethodPost, fmt.Sprintf("/api/workspaces/%s/bio-pages/%d/publish", t017.Details.WorkspaceID, pageID), map[string]any{
		"expected_version": version,
		"change_reason":    "P20 T018 publish after allow risk",
	}, unsafeHeaders(cookieHeader, origin, publishCSRF, "p20-t018-publish-"+suffix))
	if err != nil || published.Status != http.StatusOK || stringValue(published.Body["status"]) != "published" {
		fail("T018 Bio publish did not succeed after authoritative allow risk")
		return
	}
	publishedVersion := uint64Value(published.Body["version"])
	if publishedVersion <= version {
		fail("T018 Bio publish did not advance version authority")
		return
	}
	result.Details["publish_http_status"] = published.Status
	result.Details["publish_version"] = publishedVersion

	publicHTML, err := requestRaw(ctx, apiBase, http.MethodGet, "/p/"+slug, nil, nil)
	if err != nil || publicHTML.Status != http.StatusOK {
		fail("T018 published Bio HTML is not publicly readable")
		return
	}
	publicAPI, err := requestRaw(ctx, apiBase, http.MethodGet, "/api/public/bio/"+slug, nil, nil)
	if err != nil || publicAPI.Status != http.StatusOK {
		fail("T018 published Bio public API is not readable")
		return
	}
	bodyLower := strings.ToLower(string(publicHTML.Raw))
	robots := strings.ToLower(publicHTML.Headers.Get("X-Robots-Tag"))
	noindex := strings.Contains(robots, "noindex") && strings.Contains(bodyLower, `<meta name="robots" content="noindex,nofollow">`)
	outboundAllowed := bytes.Contains(publicHTML.Raw, []byte(`href="`+bioURLV1+`"`))
	ugcAttributes := strings.Contains(bodyLower, `rel="ugc nofollow"`)
	apiChild := firstMap(publicAPI.Body["links"])
	apiURL := stringValue(apiChild["url"])
	result.Details["public_html_http_status"] = publicHTML.Status
	result.Details["public_api_http_status"] = publicAPI.Status
	result.Details["permanent_noindex"] = noindex
	result.Details["outbound_allow_visible"] = outboundAllowed
	result.Details["outbound_ugc_nofollow"] = ugcAttributes
	result.Details["public_api_allow_url_matches"] = apiURL == bioURLV1
	if !noindex || !outboundAllowed || !ugcAttributes || apiURL != bioURLV1 {
		fail("T018 published Bio did not preserve outbound UGC and permanent noindex authority")
		return
	}

	hits, err := sitemapBioHits()
	if err != nil {
		fail("T018 could not inspect sitemap exclusion")
		return
	}
	result.Details["sitemap_bio_hits"] = hits
	result.Details["sitemap_excluded"] = len(hits) == 0
	if len(hits) != 0 {
		fail("T018 Bio UGC appeared in sitemap authority")
		return
	}

	updateCSRF, _, err := issueCSRF(ctx, apiBase, cookieHeader, t017.Details.UserID)
	if err != nil {
		fail("T018 could not issue one-time CSRF for Bio child mutation")
		return
	}
	updated, err := requestJSON(ctx, apiBase, http.MethodPatch, fmt.Sprintf("/api/workspaces/%s/bio-pages/%d", t017.Details.WorkspaceID, pageID), map[string]any{
		"expected_version": publishedVersion,
		"links": []map[string]any{{
			"id":              childID,
			"position":        0,
			"label":           "Primary",
			"destination_url": bioURLV2,
		}},
		"change_reason": "P20 T018 mutate child destination",
	}, unsafeHeaders(cookieHeader, origin, updateCSRF, "p20-t018-update-"+suffix))
	if err != nil || updated.Status != http.StatusOK {
		fail("T018 Bio child destination mutation did not commit")
		return
	}
	updatedChild := firstMap(updated.Body["links"])
	fingerprintV2 := stringValue(updatedChild["destination_fingerprint"])
	riskAfterUpdate := stringValue(updatedChild["risk_status"])
	result.Details["bio_update_http_status"] = updated.Status
	result.Details["destination_fingerprint_changed"] = fingerprintV2 != "" && fingerprintV2 != fingerprintV1
	result.Details["risk_invalidated_to_review"] = riskAfterUpdate == "review"
	if fingerprintV2 == "" || fingerprintV2 == fingerprintV1 || riskAfterUpdate != "review" {
		fail("T018 child destination mutation did not invalidate prior allow risk")
		return
	}

	publicAfterUpdate, err := requestRaw(ctx, apiBase, http.MethodGet, "/p/"+slug, nil, nil)
	if err != nil || publicAfterUpdate.Status != http.StatusOK {
		fail("T018 published Bio became unavailable after child risk invalidation")
		return
	}
	apiAfterUpdate, err := requestRaw(ctx, apiBase, http.MethodGet, "/api/public/bio/"+slug, nil, nil)
	if err != nil || apiAfterUpdate.Status != http.StatusOK {
		fail("T018 public Bio API became unavailable after child risk invalidation")
		return
	}
	updatedHTML := string(publicAfterUpdate.Raw)
	updatedAPIChild := firstMap(apiAfterUpdate.Body["links"])
	newURLHidden := !strings.Contains(updatedHTML, `href="`+bioURLV2+`"`) && stringValue(updatedAPIChild["url"]) == ""
	oldURLHidden := !strings.Contains(updatedHTML, `href="`+bioURLV1+`"`)
	result.Details["invalidated_new_url_hidden"] = newURLHidden
	result.Details["stale_old_url_hidden"] = oldURLHidden
	if !newURLHidden || !oldURLHidden {
		fail("T018 invalidated Bio child destination did not fail closed publicly")
		return
	}

	result.Details["next_case"] = "P20-T019"
	finish()
}

func readT017(path, exactHead string) (predecessorEvidence, error) {
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

func seedRisk(ctx context.Context, client *redis.Client, childID uint64, fingerprint, decision string) error {
	now := time.Now().UTC()
	payload := riskDecision{
		SchemaVersion: 1,
		Decision:      decision,
		Fingerprint:   fingerprint,
		CheckedAt:     now.Format(time.RFC3339Nano),
		ValidUntil:    now.Add(30 * time.Minute).Format(time.RFC3339Nano),
		PolicyVersion: "p20-t018-live-v1",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("risk:bio-child:%d:%s", childID, fingerprint)
	return client.Set(ctx, key, string(raw), 30*time.Minute).Err()
}

func sitemapBioHits() ([]string, error) {
	hits := []string{}
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path == ".git" || strings.HasPrefix(path, ".git"+string(os.PathSeparator)) || strings.HasPrefix(filepath.ToSlash(path), "artifacts/v10/P11/sitemap") {
				if path == ".git" || strings.HasPrefix(filepath.ToSlash(path), "artifacts/v10/P11/sitemap") {
					return filepath.SkipDir
				}
			}
			return nil
		}
		base := strings.ToLower(filepath.Base(path))
		ext := strings.ToLower(filepath.Ext(path))
		if !strings.Contains(base, "sitemap") || (ext != ".xml" && ext != ".txt" && ext != ".json") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(raw, []byte("/p/")) {
			hits = append(hits, filepath.ToSlash(path))
		}
		return nil
	})
	return hits, err
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

func firstMap(value any) map[string]any {
	items, _ := value.([]any)
	if len(items) == 0 {
		return map[string]any{}
	}
	result, _ := items[0].(map[string]any)
	if result == nil {
		return map[string]any{}
	}
	return result
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
