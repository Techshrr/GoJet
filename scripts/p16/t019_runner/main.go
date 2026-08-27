package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/scripts/p16/domainfixture"
	"github.com/Techshrr/GoJet/scripts/p16/runtimefixture"
)

type output struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	Fixture      string          `json:"fixture"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

type response struct {
	Status  int
	Headers http.Header
	Body    map[string]any
	Raw     string
}

func main() {
	out, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		out.Case = "P16-T019"
		out.Status = "FAIL"
		if out.Checks == nil {
			out.Checks = map[string]bool{"runner_completed": false}
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
	if err != nil || out.Status != "PASS" {
		os.Exit(1)
	}
}

func run() (output, error) {
	out := output{
		Case:         "P16-T019",
		Status:       "FAIL",
		Fixture:      "real MySQL/Redis/native platformapi public abuse-report endpoint with deterministic server-side Turnstile, idempotency and fail-closed rate authority",
		RecordCounts: map[string]int{},
		Checks:       map[string]bool{},
	}
	db, redisClient, err := runtimefixture.Open()
	if err != nil {
		return out, err
	}
	defer db.Close()
	defer redisClient.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return out, err
	}
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return out, err
	}
	if err := redisClient.FlushDB(ctx).Err(); err != nil {
		return out, err
	}
	out.MySQLVersion, err = domainfixture.MySQLVersion(ctx, db)
	if err != nil {
		return out, err
	}
	link, err := runtimefixture.CreateLink(ctx, db, "p16-t019-workspace", "go.example.test", "official", "t019-report", "https://safe.example/t019", nil, nil)
	if err != nil {
		return out, err
	}
	token := strings.TrimSpace(os.Getenv("GOJET_TEST_TRUST_TURNSTILE_TOKEN"))
	if token == "" {
		return out, fmt.Errorf("GOJET_TEST_TRUST_TURNSTILE_TOKEN is required")
	}
	validBody := map[string]any{
		"resource_type":   "short-link-risk",
		"hostname":        link.Hostname,
		"code":            link.Code,
		"category":        "phishing",
		"details":         "Suspicious redirect behavior",
		"turnstile_token": token,
	}
	created, err := request(ctx, "p16-t019-idempotency-0001", validBody)
	if err != nil {
		return out, err
	}
	replayed, err := request(ctx, "p16-t019-idempotency-0001", validBody)
	if err != nil {
		return out, err
	}
	conflictBody := clone(validBody)
	conflictBody["category"] = "spam"
	conflict, err := request(ctx, "p16-t019-idempotency-0001", conflictBody)
	if err != nil {
		return out, err
	}
	invalidBody := clone(validBody)
	invalidBody["resource_type"] = "workspace"
	invalidBody["turnstile_token"] = "not-used-for-invalid-input"
	invalid, err := request(ctx, "p16-t019-idempotency-invalid", invalidBody)
	if err != nil {
		return out, err
	}
	bad1 := clone(validBody)
	bad1["turnstile_token"] = "wrong-turnstile-token-1"
	verification, err := request(ctx, "p16-t019-idempotency-bad-1", bad1)
	if err != nil {
		return out, err
	}
	bad2 := clone(validBody)
	bad2["turnstile_token"] = "wrong-turnstile-token-2"
	_, err = request(ctx, "p16-t019-idempotency-bad-2", bad2)
	if err != nil {
		return out, err
	}
	bad3 := clone(validBody)
	bad3["turnstile_token"] = "wrong-turnstile-token-3"
	rateLimited, err := request(ctx, "p16-t019-idempotency-bad-3", bad3)
	if err != nil {
		return out, err
	}

	reports, err := runtimefixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM abuse_reports WHERE workspace_id=? AND resource_type='short-link-risk'`, link.WorkspaceID)
	if err != nil {
		return out, err
	}
	events, err := runtimefixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM abuse_report_events e JOIN abuse_reports r ON r.id=e.report_id WHERE r.workspace_id=? AND e.action='abuse.public-intake'`, link.WorkspaceID)
	if err != nil {
		return out, err
	}
	keys, err := redisClient.Keys(ctx, "p16:abuse:*").Result()
	if err != nil {
		return out, err
	}
	keyMaterial := strings.Join(keys, "\n")
	reportID, _ := created.Body["report_id"].(string)
	replayID, _ := replayed.Body["report_id"].(string)
	createdFlag, _ := created.Body["created"].(bool)
	replayFlag, _ := replayed.Body["created"].(bool)

	out.RecordCounts = map[string]int{
		"durable_abuse_reports":   reports,
		"immutable_intake_events": events,
		"abuse_redis_keys":        len(keys),
	}
	out.Checks = map[string]bool{
		"public_api_persists_safe_receipt":                        created.Status == http.StatusCreated && createdFlag && strings.HasPrefix(reportID, "abr_") && reports == 1 && events == 1,
		"idempotent_retry_returns_same_receipt":                   replayed.Status == http.StatusOK && !replayFlag && replayID == reportID && reports == 1 && events == 1,
		"idempotency_key_payload_mismatch_conflicts":              conflict.Status == http.StatusConflict && errorCode(conflict) == "idempotency_conflict",
		"unallowlisted_resource_type_is_rejected":                 invalid.Status == http.StatusBadRequest && errorCode(invalid) == "invalid_request",
		"turnstile_is_verified_server_side":                       verification.Status == http.StatusBadRequest && errorCode(verification) == "verification_failed",
		"rate_budget_fails_closed":                                rateLimited.Status == http.StatusTooManyRequests && errorCode(rateLimited) == "rate_limited",
		"public_response_is_no_store_noindex":                     strings.Contains(created.Headers.Get("Cache-Control"), "no-store") && strings.Contains(created.Headers.Get("X-Robots-Tag"), "noindex"),
		"public_receipt_discloses_no_internal_resource_state":     !strings.Contains(created.Raw, link.WorkspaceID) && !strings.Contains(created.Raw, link.Hostname) && !strings.Contains(created.Raw, link.Code) && !strings.Contains(created.Raw, link.Fingerprint),
		"redis_keys_contain_only_hashed_rate_and_replay_identity": !strings.Contains(keyMaterial, token) && !strings.Contains(keyMaterial, "127.0.0.1") && len(keys) >= 2,
	}
	if runtimefixture.AllTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func request(ctx context.Context, idempotencyKey string, body map[string]any) (response, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_PLATFORMAPI_URL")), "/")
	if base == "" {
		return response{}, fmt.Errorf("GOJET_PLATFORMAPI_URL is required")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return response{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/public/abuse-reports", bytes.NewReader(raw))
	if err != nil {
		return response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return response{}, err
	}
	defer resp.Body.Close()
	bodyRaw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return response{}, err
	}
	decoded := map[string]any{}
	if len(bodyRaw) > 0 {
		if err := json.Unmarshal(bodyRaw, &decoded); err != nil {
			return response{}, err
		}
	}
	return response{Status: resp.StatusCode, Headers: resp.Header.Clone(), Body: decoded, Raw: string(bodyRaw)}, nil
}

func clone(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func errorCode(resp response) string {
	errorValue, _ := resp.Body["error"].(map[string]any)
	code, _ := errorValue["code"].(string)
	return code
}
