package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/links"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

type predecessorEvidence struct {
	Status               string `json:"status"`
	ImplementationCommit string `json:"implementation_commit"`
	Details              struct {
		UserID      string `json:"user_id"`
		WorkspaceID string `json:"workspace_id"`
		LinkID      uint64 `json:"link_id"`
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

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exactHead := strings.TrimSpace(os.Getenv("P20_EXACT_HEAD"))
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	apiBase := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_P20_API_BASE")), "/")
	origin := strings.TrimSpace(os.Getenv("GOJET_AUTH_ALLOWED_ORIGIN"))
	outPath := strings.TrimSpace(os.Getenv("GOJET_P20_T015_PROBE_OUT"))
	if outPath == "" {
		outPath = "artifacts/v10/P20/runtime/t015/probe.json"
	}
	artifactDir := filepath.Dir(outPath)

	result := probe{
		Status:               "FAIL",
		ImplementationCommit: exactHead,
		Errors:               []string{},
		Details: map[string]any{
			"real_mysql":                  true,
			"real_redis":                  true,
			"real_platform_api":           true,
			"real_qr_renderer":            false,
			"independent_decoder":         "zbarimg",
			"independent_decoder_invoked": false,
			"mock_authority":              false,
			"test_header_authority":       false,
			"secret_material_recorded":    false,
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

	if len(exactHead) != 40 || dsn == "" || redisAddr == "" || apiBase == "" || origin == "" {
		fail("T015 probe runtime configuration is incomplete")
		return
	}

	t014, err := readPredecessor("artifacts/v10/P20/p0/P20-T014.json", exactHead)
	if err != nil {
		fail("T015 probe requires same-run exact-head P20-T014 PASS evidence")
		return
	}
	t013, err := readPredecessor("artifacts/v10/P20/p0/P20-T013.json", exactHead)
	if err != nil || t013.Details.UserID == "" || t013.Details.WorkspaceID == "" || t013.Details.LinkID == 0 {
		fail("T015 probe requires same-run exact-head P20-T013 identity evidence")
		return
	}
	result.Details["t014_evidence_bound"] = t014.Status == "PASS"
	result.Details["t013_identity_bound"] = true
	result.Details["user_id"] = t013.Details.UserID
	result.Details["workspace_id"] = t013.Details.WorkspaceID
	result.Details["source_link_id"] = t013.Details.LinkID

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fail("T015 probe could not open MySQL")
		return
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		fail("T015 probe could not reach MySQL")
		return
	}

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		fail("T015 probe could not reach Redis")
		return
	}

	var userStatus, workspaceRole string
	if err := db.QueryRowContext(ctx, `
		SELECT u.status,m.role
		FROM auth_users u JOIN workspace_memberships m ON m.user_id=u.id
		WHERE u.id=? AND m.workspace_id=?`, t013.Details.UserID, t013.Details.WorkspaceID,
	).Scan(&userStatus, &workspaceRole); err != nil || userStatus != "active" || workspaceRole != "owner" {
		fail("T015 predecessor identity is not an active owner Workspace authority")
		return
	}
	result.Details["workspace_role_before_qr"] = workspaceRole

	var linkWorkspace, hostname, code, fingerprint, linkStatus string
	if err := db.QueryRowContext(ctx, `
		SELECT workspace_id,hostname,code,risk_fingerprint,status
		FROM links WHERE id=?`, t013.Details.LinkID,
	).Scan(&linkWorkspace, &hostname, &code, &fingerprint, &linkStatus); err != nil {
		fail("T015 probe could not bind to the T013 source Link")
		return
	}
	if linkWorkspace != t013.Details.WorkspaceID || hostname == "" || code == "" || fingerprint == "" || linkStatus != "active" {
		fail("T015 source Link identity/current state is invalid")
		return
	}
	riskStore := links.NewRedisRiskStore(redisClient)
	_, riskState, err := riskStore.Resolve(ctx, t013.Details.LinkID, fingerprint, time.Now().UTC())
	if err != nil || riskState != links.RiskAllow {
		result.Details["source_risk_state"] = string(riskState)
		fail("T015 source Link exact-current risk authority is not allow")
		return
	}
	expectedURL := "https://" + hostname + "/" + code
	result.Details["source_risk_allow"] = true
	result.Details["source_risk_state"] = string(riskState)
	result.Details["source_fingerprint_present"] = len(fingerprint) == 64
	result.Details["expected_public_url"] = expectedURL

	rowsBefore, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM qr_codes WHERE workspace_id=? AND deleted_at IS NULL`, t013.Details.WorkspaceID)
	if err != nil {
		fail("T015 probe could not inspect pre-create QR row count")
		return
	}
	result.Details["qr_rows_before"] = rowsBefore

	suffix := strings.ToLower(exactHead[:12])
	email := "p20-t009-" + suffix + "@example.test"
	login, err := request(ctx, apiBase, http.MethodPost, "/api/auth/login", map[string]any{
		"email":          email,
		"password":       "P20-T009!Strong-Passphrase-2026",
		"correlation_id": "p20-t015-login-" + suffix,
	}, nil)
	if err != nil {
		fail("T015 probe real P15 login request failed")
		return
	}
	result.Details["login_http_status"] = login.Status
	if login.Status != http.StatusOK || stringValue(login.Body["status"]) != "authenticated" {
		result.Details["login_error_code"] = nestedErrorCode(login.Body)
		fail("T015 probe could not establish the real authenticated QR session")
		return
	}
	sessionCookie := findSessionCookie(login.Cookies)
	if sessionCookie == nil || strings.TrimSpace(sessionCookie.Value) == "" {
		fail("T015 real login did not issue the signed session cookie")
		return
	}
	cookieHeader := "__Host-gojet_session=" + sessionCookie.Value
	csrf, sessionID, err := issueCSRF(ctx, apiBase, cookieHeader, t013.Details.UserID)
	if err != nil {
		fail("T015 real session is not accepted by /api/me")
		return
	}
	result.Details["real_session_authenticated"] = true
	result.Details["csrf_authority_issued"] = csrf != ""
	result.Details["session_id_present"] = sessionID != ""

	create, err := request(ctx, apiBase, http.MethodPost, "/api/workspaces/"+t013.Details.WorkspaceID+"/qr-codes", map[string]any{
		"source_link_id": t013.Details.LinkID,
		"label":          "P20 T015 QR",
		"change_reason":  "P20 T015 create",
	}, unsafeHeaders(cookieHeader, origin, csrf, "p20-t015-create-"+suffix))
	if err != nil {
		fail("T015 real QR create HTTP request failed")
		return
	}
	result.Details["qr_create_http_status"] = create.Status
	result.Details["qr_create_error_code"] = nestedErrorCode(create.Body)
	rowsAfter, _ := scalarInt(ctx, db, `SELECT COUNT(*) FROM qr_codes WHERE workspace_id=? AND deleted_at IS NULL`, t013.Details.WorkspaceID)
	result.Details["qr_rows_after_create"] = rowsAfter
	result.Details["qr_create_row_delta"] = rowsAfter - rowsBefore
	if create.Status != http.StatusCreated {
		result.Details["qr_create_failed_without_write"] = rowsAfter == rowsBefore
		fail("real authenticated session is not accepted as QR API mutation authority")
		return
	}

	qrID := uint64Value(create.Body["id"])
	createWorkspace := stringValue(create.Body["workspace_id"])
	createSourceLink := uint64Value(create.Body["source_link_id"])
	createState := stringValue(create.Body["state"])
	createPublicURL := nestedString(create.Body, "source", "public_url")
	createRiskState := nestedString(create.Body, "source", "risk_state")
	result.Details["qr_id"] = qrID
	result.Details["qr_create_state"] = createState
	result.Details["qr_create_public_url"] = createPublicURL
	if qrID == 0 || createWorkspace != t013.Details.WorkspaceID || createSourceLink != t013.Details.LinkID || createPublicURL != expectedURL || createRiskState != "allow" {
		fail("T015 QR creation response lost Workspace/source/public-URL/risk identity continuity")
		return
	}

	var dbWorkspace, createdBy string
	var dbSource uint64
	var notDeleted int
	if err := db.QueryRowContext(ctx, `
		SELECT workspace_id,source_link_id,created_by,deleted_at IS NULL
		FROM qr_codes WHERE id=?`, qrID,
	).Scan(&dbWorkspace, &dbSource, &createdBy, &notDeleted); err != nil {
		fail("T015 could not inspect created QR resource in MySQL")
		return
	}
	mysqlBound := dbWorkspace == t013.Details.WorkspaceID && dbSource == t013.Details.LinkID && createdBy == t013.Details.UserID && notDeleted == 1
	result.Details["mysql_qr_identity_bound"] = mysqlBound
	if !mysqlBound {
		fail("T015 MySQL QR identity does not match the real-session create authority")
		return
	}

	detail, err := request(ctx, apiBase, http.MethodGet, fmt.Sprintf("/api/workspaces/%s/qr-codes/%d", t013.Details.WorkspaceID, qrID), nil, map[string]string{"Cookie": cookieHeader})
	if err != nil || detail.Status != http.StatusOK {
		fail("T015 real session could not read the created QR resource")
		return
	}
	detailBound := uint64Value(detail.Body["id"]) == qrID && uint64Value(detail.Body["source_link_id"]) == t013.Details.LinkID && nestedString(detail.Body, "source", "public_url") == expectedURL
	result.Details["detail_http_status"] = detail.Status
	result.Details["detail_identity_bound"] = detailBound
	if !detailBound {
		fail("T015 QR detail lost source/public-URL identifier continuity")
		return
	}

	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		fail("T015 could not create runtime artifact directory")
		return
	}
	png, err := request(ctx, apiBase, http.MethodGet, fmt.Sprintf("/api/workspaces/%s/qr-codes/%d/download?format=png", t013.Details.WorkspaceID, qrID), nil, map[string]string{"Cookie": cookieHeader})
	if err != nil || png.Status != http.StatusOK || !bytes.HasPrefix(png.Raw, []byte{'\x89', 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		fail("T015 PNG download is not a real valid QR asset")
		return
	}
	svg, err := request(ctx, apiBase, http.MethodGet, fmt.Sprintf("/api/workspaces/%s/qr-codes/%d/download?format=svg", t013.Details.WorkspaceID, qrID), nil, map[string]string{"Cookie": cookieHeader})
	if err != nil || svg.Status != http.StatusOK || !bytes.Contains(bytes.ToLower(svg.Raw), []byte("<svg")) {
		fail("T015 SVG download is not a real QR asset")
		return
	}
	preview, err := request(ctx, apiBase, http.MethodGet, fmt.Sprintf("/api/workspaces/%s/qr-codes/%d/preview?format=png", t013.Details.WorkspaceID, qrID), nil, map[string]string{"Cookie": cookieHeader})
	if err != nil || preview.Status != http.StatusOK || !bytes.HasPrefix(preview.Raw, []byte{'\x89', 'P', 'N', 'G'}) {
		fail("T015 PNG preview is not a real QR asset")
		return
	}

	pngPath := filepath.Join(artifactDir, "download.png")
	svgPath := filepath.Join(artifactDir, "download.svg")
	svgRasterPath := filepath.Join(artifactDir, "download-svg-raster.png")
	if err := os.WriteFile(pngPath, png.Raw, 0o644); err != nil {
		fail("T015 could not persist PNG runtime evidence")
		return
	}
	if err := os.WriteFile(svgPath, svg.Raw, 0o644); err != nil {
		fail("T015 could not persist SVG runtime evidence")
		return
	}
	pngDigest := digest(png.Raw)
	svgDigest := digest(svg.Raw)
	result.Details["real_qr_renderer"] = true
	result.Details["preview_png_status"] = preview.Status
	result.Details["download_png_status"] = png.Status
	result.Details["download_svg_status"] = svg.Status
	result.Details["png_sha256"] = pngDigest
	result.Details["svg_sha256"] = svgDigest
	result.Details["png_digest_header_matches"] = strings.TrimSpace(png.Headers.Get("X-GoJet-Artifact-SHA256")) == pngDigest
	result.Details["svg_digest_header_matches"] = strings.TrimSpace(svg.Headers.Get("X-GoJet-Artifact-SHA256")) == svgDigest
	if result.Details["png_digest_header_matches"] != true || result.Details["svg_digest_header_matches"] != true {
		fail("T015 artifact SHA headers do not match downloaded bytes")
		return
	}

	pngDecoded, err := decodeZBar(ctx, pngPath)
	if err != nil {
		fail("T015 independent decoder could not decode downloaded PNG")
		return
	}
	cmd := exec.CommandContext(ctx, "rsvg-convert", "-o", svgRasterPath, svgPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		result.Details["svg_rasterizer_error"] = strings.TrimSpace(string(output))
		fail("T015 independent SVG rasterizer failed")
		return
	}
	svgDecoded, err := decodeZBar(ctx, svgRasterPath)
	if err != nil {
		fail("T015 independent decoder could not decode downloaded SVG")
		return
	}
	result.Details["independent_decoder_invoked"] = true
	result.Details["png_decoded"] = pngDecoded
	result.Details["svg_decoded"] = svgDecoded
	decodeBound := pngDecoded == expectedURL && svgDecoded == expectedURL
	result.Details["independent_decode_correlated"] = decodeBound
	if !decodeBound {
		fail("T015 independently decoded QR payload does not equal the authoritative source Link URL")
		return
	}

	if _, err := db.ExecContext(ctx, `UPDATE workspace_memberships SET role='viewer' WHERE workspace_id=? AND user_id=?`, t013.Details.WorkspaceID, t013.Details.UserID); err != nil {
		fail("T015 could not arrange viewer RBAC verification")
		return
	}
	viewerRestored := false
	defer func() {
		if !viewerRestored {
			_, _ = db.ExecContext(context.Background(), `UPDATE workspace_memberships SET role='owner' WHERE workspace_id=? AND user_id=?`, t013.Details.WorkspaceID, t013.Details.UserID)
		}
	}()
	viewerDetail, viewerDetailErr := request(ctx, apiBase, http.MethodGet, fmt.Sprintf("/api/workspaces/%s/qr-codes/%d", t013.Details.WorkspaceID, qrID), nil, map[string]string{"Cookie": cookieHeader})
	viewerDownload, viewerDownloadErr := request(ctx, apiBase, http.MethodGet, fmt.Sprintf("/api/workspaces/%s/qr-codes/%d/download?format=png", t013.Details.WorkspaceID, qrID), nil, map[string]string{"Cookie": cookieHeader})
	viewerCSRF, _, viewerCSRFErr := issueCSRF(ctx, apiBase, cookieHeader, t013.Details.UserID)
	viewerCreate := httpResult{}
	var viewerCreateErr error
	if viewerCSRFErr == nil {
		viewerCreate, viewerCreateErr = request(ctx, apiBase, http.MethodPost, "/api/workspaces/"+t013.Details.WorkspaceID+"/qr-codes", map[string]any{
			"source_link_id": t013.Details.LinkID,
			"label":          "P20 T015 viewer denied",
			"change_reason":  "P20 T015 viewer denial",
		}, unsafeHeaders(cookieHeader, origin, viewerCSRF, "p20-t015-viewer-"+suffix))
	}
	restoreErr := error(nil)
	if _, err := db.ExecContext(ctx, `UPDATE workspace_memberships SET role='owner' WHERE workspace_id=? AND user_id=?`, t013.Details.WorkspaceID, t013.Details.UserID); err != nil {
		restoreErr = err
	} else {
		viewerRestored = true
	}
	result.Details["workspace_role_restored"] = viewerRestored
	result.Details["viewer_detail_http_status"] = viewerDetail.Status
	result.Details["viewer_download_http_status"] = viewerDownload.Status
	result.Details["viewer_create_http_status"] = viewerCreate.Status
	result.Details["viewer_create_error_code"] = nestedErrorCode(viewerCreate.Body)
	if restoreErr != nil {
		fail("T015 could not restore owner membership after viewer verification")
		return
	}
	if viewerDetailErr != nil || viewerDownloadErr != nil || viewerCSRFErr != nil || viewerCreateErr != nil || viewerDetail.Status != http.StatusOK || viewerDownload.Status != http.StatusOK || viewerCreate.Status != http.StatusForbidden || nestedErrorCode(viewerCreate.Body) != "read_only" {
		fail("T015 QR RBAC continuity does not preserve viewer read-only semantics")
		return
	}
	result.Details["permission_continuity"] = true

	finish()
}

func readPredecessor(path, exactHead string) (predecessorEvidence, error) {
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

func request(ctx context.Context, base, method, path string, payload map[string]any, extra map[string]string) (httpResult, error) {
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
	me, err := request(ctx, apiBase, http.MethodGet, "/api/me", nil, map[string]string{"Cookie": cookie})
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

func decodeZBar(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "zbarimg", "--quiet", "--raw", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("zbarimg: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
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

func uint64Value(value any) uint64 {
	typed, ok := value.(float64)
	if !ok || typed <= 0 || typed != float64(uint64(typed)) {
		return 0
	}
	return uint64(typed)
}
