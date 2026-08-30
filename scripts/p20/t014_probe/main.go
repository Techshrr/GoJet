package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type t013Evidence struct {
	Status               string `json:"status"`
	ImplementationCommit string `json:"implementation_commit"`
	Details              struct {
		UserID       string `json:"user_id"`
		WorkspaceID  string `json:"workspace_id"`
		LinkID       uint64 `json:"link_id"`
		ClickEventID string `json:"click_event_id"`
		ClickSequence uint64 `json:"click_sequence"`
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
	Cookies []*http.Cookie
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	exactHead := strings.TrimSpace(os.Getenv("P20_EXACT_HEAD"))
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	apiBase := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_P20_API_BASE")), "/")
	workerBin := strings.TrimSpace(os.Getenv("GOJET_P20_ANALYTICS_WORKER_BIN"))
	reconcilerBin := strings.TrimSpace(os.Getenv("GOJET_P20_ANALYTICS_RECONCILER_BIN"))
	outPath := strings.TrimSpace(os.Getenv("GOJET_P20_T014_PROBE_OUT"))
	if outPath == "" {
		outPath = "artifacts/v10/P20/runtime/t014/probe.json"
	}

	result := probe{
		Status:               "FAIL",
		ImplementationCommit: exactHead,
		Errors:               []string{},
		Details: map[string]any{
			"real_mysql":                true,
			"real_redis_transport":      true,
			"real_analyticsworker":      true,
			"real_analyticsreconciler":  true,
			"real_platform_api":         true,
			"mock_authority":            false,
			"test_header_authority":     false,
			"secret_material_recorded":  false,
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

	if len(exactHead) < 12 || dsn == "" || apiBase == "" || workerBin == "" || reconcilerBin == "" {
		fail("T014 probe runtime configuration is incomplete")
		return
	}

	t013Raw, err := os.ReadFile("artifacts/v10/P20/p0/P20-T013.json")
	if err != nil {
		fail("T014 probe requires same-run T013 evidence")
		return
	}
	var predecessor t013Evidence
	if err := json.Unmarshal(t013Raw, &predecessor); err != nil || predecessor.Status != "PASS" || predecessor.ImplementationCommit != exactHead {
		fail("T014 probe predecessor T013 evidence is not exact-head PASS")
		return
	}
	if predecessor.Details.WorkspaceID == "" || predecessor.Details.LinkID == 0 || predecessor.Details.ClickEventID == "" || predecessor.Details.ClickSequence != 1 {
		fail("T014 probe predecessor click identity is incomplete")
		return
	}
	result.Details["workspace_id"] = predecessor.Details.WorkspaceID
	result.Details["link_id"] = predecessor.Details.LinkID
	result.Details["click_event_id"] = predecessor.Details.ClickEventID
	result.Details["click_sequence"] = predecessor.Details.ClickSequence
	result.Details["t013_click_correlation_preserved"] = true

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fail("T014 probe could not open MySQL")
		return
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		fail("T014 probe could not reach MySQL")
		return
	}

	var outWorkspace, publishedStreamID string
	var outLinkID, outSequence uint64
	var published int
	if err := db.QueryRowContext(ctx, `
		SELECT workspace_id,link_id,click_sequence,COALESCE(published_stream_id,''),published_at IS NOT NULL
		FROM analytics_outbox WHERE event_id=?`, predecessor.Details.ClickEventID,
	).Scan(&outWorkspace, &outLinkID, &outSequence, &publishedStreamID, &published); err != nil {
		fail("T014 probe could not bind to the T013 analytics outbox event")
		return
	}
	transportBound := outWorkspace == predecessor.Details.WorkspaceID && outLinkID == predecessor.Details.LinkID && outSequence == 1 && published == 1 && publishedStreamID != ""
	result.Details["t013_outbox_transport_bound"] = transportBound
	result.Details["published_stream_id_present"] = publishedStreamID != ""
	if !transportBound {
		fail("T014 probe T013 click is not bound to the published Redis transport identity")
		return
	}

	workerLogs, err := runBinary(ctx, workerBin, map[string]string{
		"GOJET_ANALYTICS_WORKER_CONSUMER":     "p20-t014-" + exactHead[:12],
		"GOJET_ANALYTICS_WORKER_MAX_MESSAGES": "1",
	})
	result.Details["analyticsworker_logged_processing"] = strings.Contains(workerLogs, "analytics event processed")
	if err != nil {
		fail("T014 probe native analyticsworker failed")
		return
	}

	var eventWorkspace, consumedStreamID string
	var eventLinkID, eventSequence uint64
	var occurredAt time.Time
	if err := db.QueryRowContext(ctx, `
		SELECT workspace_id,link_id,click_sequence,stream_id,occurred_at
		FROM analytics_events WHERE event_id=?`, predecessor.Details.ClickEventID,
	).Scan(&eventWorkspace, &eventLinkID, &eventSequence, &consumedStreamID, &occurredAt); err != nil {
		fail("T014 probe worker did not persist the same T013 click event")
		return
	}
	workerCorrelated := eventWorkspace == predecessor.Details.WorkspaceID && eventLinkID == predecessor.Details.LinkID && eventSequence == 1 && consumedStreamID == publishedStreamID
	result.Details["analyticsworker_event_correlated"] = workerCorrelated
	if !workerCorrelated {
		fail("T014 probe analyticsworker persistence lost T013 click correlation")
		return
	}

	aggregateBefore, err := scalarInt(ctx, db, `SELECT COALESCE(SUM(clicks),0) FROM analytics_hourly_aggregates WHERE workspace_id=? AND link_id=?`, predecessor.Details.WorkspaceID, predecessor.Details.LinkID)
	if err != nil || aggregateBefore != 1 {
		fail("T014 probe native worker aggregation is not exactly one correlated click")
		return
	}
	result.Details["mysql_reporting_aggregate_before_reconcile"] = aggregateBefore

	firstLogs, err := runBinary(ctx, reconcilerBin, map[string]string{
		"GOJET_ANALYTICS_RECONCILER_ONCE":    "1",
		"GOJET_ANALYTICS_RECONCILE_REPAIR":   "1",
	})
	if err != nil {
		fail("T014 probe first native reconciliation cycle failed")
		return
	}
	firstSource, firstBefore, firstAfter, firstRepaired, err := latestReconciliation(ctx, db)
	if err != nil || firstSource != 1 || firstBefore != 1 || firstAfter != 1 || firstRepaired {
		fail("T014 probe first reconciliation did not preserve the single-click authoritative total")
		return
	}
	result.Details["first_reconciliation_idempotent"] = !firstRepaired
	result.Details["first_reconciliation_logged_cycle"] = strings.Contains(firstLogs, "analytics reconciliation cycle")

	var workspaceState, stateReason string
	if err := db.QueryRowContext(ctx, `SELECT status,state_reason FROM analytics_workspace_state WHERE workspace_id=?`, predecessor.Details.WorkspaceID).Scan(&workspaceState, &stateReason); err != nil {
		fail("T014 probe reconciliation did not establish reporting completeness state")
		return
	}
	result.Details["workspace_reporting_state"] = workspaceState
	result.Details["workspace_reporting_state_reason"] = stateReason
	if workspaceState != "complete" || stateReason != "reconciled" {
		fail("T014 probe reporting state is not complete after reconciliation")
		return
	}

	secondLogs, err := runBinary(ctx, reconcilerBin, map[string]string{
		"GOJET_ANALYTICS_RECONCILER_ONCE":    "1",
		"GOJET_ANALYTICS_RECONCILE_REPAIR":   "1",
	})
	if err != nil {
		fail("T014 probe second native reconciliation cycle failed")
		return
	}
	secondSource, secondBefore, secondAfter, secondRepaired, err := latestReconciliation(ctx, db)
	if err != nil || secondSource != 1 || secondBefore != 1 || secondAfter != 1 || secondRepaired {
		fail("T014 probe reconciliation is not idempotent on a stable authoritative dataset")
		return
	}
	result.Details["second_reconciliation_idempotent"] = !secondRepaired
	result.Details["second_reconciliation_logged_cycle"] = strings.Contains(secondLogs, "analytics reconciliation cycle")
	result.Details["reconciliation_idempotent"] = true

	email := "p20-t009-" + strings.ToLower(exactHead[:12]) + "@example.test"
	login, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/auth/login", map[string]any{
		"email": email,
		"password": "P20-T009!Strong-Passphrase-2026",
		"correlation_id": "p20-t014-login-" + exactHead[:12],
	}, "")
	if err != nil {
		fail("T014 probe real P15 login request failed")
		return
	}
	result.Details["login_http_status"] = login.Status
	if login.Status != http.StatusOK || stringValue(login.Body["status"]) != "authenticated" {
		result.Details["login_error_code"] = nestedErrorCode(login.Body)
		fail("T014 probe could not establish a real authenticated reporting session")
		return
	}
	sessionCookie := findSessionCookie(login.Cookies)
	if sessionCookie == nil || strings.TrimSpace(sessionCookie.Value) == "" {
		fail("T014 probe real login did not issue a session cookie")
		return
	}
	cookieHeader := "__Host-gojet_session=" + sessionCookie.Value
	me, err := requestJSON(ctx, apiBase, http.MethodGet, "/api/me", nil, cookieHeader)
	if err != nil || me.Status != http.StatusOK {
		fail("T014 probe session is not accepted by the real authenticated API")
		return
	}
	result.Details["real_session_authenticated"] = true

	query := url.Values{}
	query.Set("from", occurredAt.UTC().Add(-time.Minute).Format(time.RFC3339Nano))
	query.Set("to", occurredAt.UTC().Add(time.Minute).Format(time.RFC3339Nano))
	query.Set("timezone", "UTC")
	query.Set("granularity", "hour")
	reportPath := fmt.Sprintf("/api/workspaces/%s/analytics/links/%d?%s", url.PathEscape(predecessor.Details.WorkspaceID), predecessor.Details.LinkID, query.Encode())
	report, err := requestJSON(ctx, apiBase, http.MethodGet, reportPath, nil, cookieHeader)
	if err != nil {
		fail("T014 probe real analytics report request failed")
		return
	}
	result.Details["report_http_status"] = report.Status
	result.Details["report_error_code"] = nestedErrorCode(report.Body)
	if report.Status != http.StatusOK {
		fail("real authenticated session is not accepted as Analytics API reporting authority")
		return
	}
	totalClicks := uint64Value(report.Body["total_clicks"])
	result.Details["report_total_clicks"] = totalClicks
	result.Details["analytics_report_correlated"] = totalClicks == 1
	if totalClicks != 1 {
		fail("T014 probe browser/API report did not correlate to the same T013 click")
		return
	}

	result.Details["real_analytics_flow"] = true
	finish()
}

func runBinary(ctx context.Context, binary string, overrides map[string]string) (string, error) {
	cmd := exec.CommandContext(ctx, binary)
	env := os.Environ()
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	cmd.Env = env
	raw, err := cmd.CombinedOutput()
	return string(raw), err
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var value int
	err := db.QueryRowContext(ctx, query, args...).Scan(&value)
	return value, err
}

func latestReconciliation(ctx context.Context, db *sql.DB) (source, before, after int, repaired bool, err error) {
	var repairedInt int
	err = db.QueryRowContext(ctx, `
		SELECT source_event_total,aggregate_total_before,aggregate_total_after,repaired
		FROM analytics_reconciliation_runs ORDER BY id DESC LIMIT 1`,
	).Scan(&source, &before, &after, &repairedInt)
	return source, before, after, repairedInt == 1, err
}

func requestJSON(ctx context.Context, base, method, path string, payload map[string]any, cookie string) (httpResult, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return httpResult{}, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return httpResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
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
	return httpResult{Status: resp.StatusCode, Body: decoded, Cookies: resp.Cookies()}, nil
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
	raw, ok := body["error"].(map[string]any)
	if !ok {
		return ""
	}
	return stringValue(raw["code"])
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func uint64Value(value any) uint64 {
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return uint64(typed)
		}
	case json.Number:
		parsed, _ := strconv.ParseUint(string(typed), 10, 64)
		return parsed
	case string:
		parsed, _ := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		return parsed
	}
	return 0
}
