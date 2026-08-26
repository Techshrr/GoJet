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
	Status int
	Body   map[string]any
	Raw    string
}

func main() {
	out, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		out.Case = "P16-T020"
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
		Case:         "P16-T020",
		Status:       "FAIL",
		Fixture:      "real MySQL/Redis/native platformapi proving server-resolved abuse correlation plus reporter/provider-secret/PII minimization across durable intake, immutable audit, response and ordinary logs",
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
	link, err := runtimefixture.CreateLink(ctx, db, "p16-t020-workspace", "go.example.test", "official", "t020-report", "https://safe.example/t020", nil, nil)
	if err != nil {
		return out, err
	}
	token := strings.TrimSpace(os.Getenv("GOJET_TEST_TRUST_TURNSTILE_TOKEN"))
	if token == "" {
		return out, fmt.Errorf("GOJET_TEST_TRUST_TURNSTILE_TOKEN is required")
	}
	rawSecret := "p16-raw-secret-fixture"
	rawEmail := "victim@example.com"
	rawBearer := "Bearer p16bearercredential12345"
	rawJWT := "eyJabcdefghijk.abcdefghijklmnop.abcdefghijklmnop"
	details := "Inspect https://unsafe.example/path?token=" + rawSecret + " contact " + rawEmail + " Authorization: " + rawBearer + " password=" + rawSecret + " jwt=" + rawJWT
	idempotencyKey := "p16-t020-raw-idempotency-key"
	resp, err := request(ctx, idempotencyKey, map[string]any{
		"resource_type":   "short-link-risk",
		"hostname":        link.Hostname,
		"code":            link.Code,
		"category":        "malware",
		"details":         details,
		"turnstile_token": token,
	})
	if err != nil {
		return out, err
	}

	redactedDetails, err := domainfixture.ScalarString(ctx, db, `SELECT details_redacted FROM abuse_reports WHERE workspace_id=? AND resource_id=?`, link.WorkspaceID, fmt.Sprintf("%d", link.ID))
	if err != nil {
		return out, err
	}
	requestFingerprint, err := domainfixture.ScalarString(ctx, db, `SELECT request_fingerprint FROM abuse_reports WHERE workspace_id=? AND resource_id=?`, link.WorkspaceID, fmt.Sprintf("%d", link.ID))
	if err != nil {
		return out, err
	}
	idempotencyHash, err := domainfixture.ScalarString(ctx, db, `SELECT idempotency_key_hash FROM abuse_reports WHERE workspace_id=? AND resource_id=?`, link.WorkspaceID, fmt.Sprintf("%d", link.ID))
	if err != nil {
		return out, err
	}
	evidenceRef, err := domainfixture.ScalarString(ctx, db, `SELECT evidence_ref FROM abuse_reports WHERE workspace_id=? AND resource_id=?`, link.WorkspaceID, fmt.Sprintf("%d", link.ID))
	if err != nil {
		return out, err
	}
	metadata, err := domainfixture.ScalarString(ctx, db, `SELECT CAST(metadata_json AS CHAR) FROM abuse_report_events e JOIN abuse_reports r ON r.id=e.report_id WHERE r.workspace_id=? AND r.resource_id=? AND e.action='abuse.public-intake'`, link.WorkspaceID, fmt.Sprintf("%d", link.ID))
	if err != nil {
		return out, err
	}
	sensitiveColumns, err := runtimefixture.ScalarInt(ctx, db, `
SELECT COUNT(*) FROM information_schema.columns
WHERE table_schema=DATABASE() AND table_name IN ('abuse_reports','abuse_report_events')
  AND column_name IN ('email','name','remote_addr','ip_address','turnstile_token','provider_evidence','raw_evidence')`)
	if err != nil {
		return out, err
	}
	correlated, err := runtimefixture.ScalarInt(ctx, db, `
SELECT COUNT(*) FROM abuse_reports r JOIN abuse_report_events e ON e.report_id=r.id
WHERE r.workspace_id=? AND r.resource_type='short-link-risk' AND r.resource_id=?
  AND r.hostname_ascii=? AND r.safe_code=? AND r.destination_fingerprint=?
  AND r.correlation_id=e.correlation_id AND r.evidence_ref LIKE 'abuse-report:abr_%'`,
		link.WorkspaceID, fmt.Sprintf("%d", link.ID), link.Hostname, link.Code, link.Fingerprint)
	if err != nil {
		return out, err
	}

	logSafe := true
	if logPath := strings.TrimSpace(os.Getenv("GOJET_PLATFORMAPI_LOG")); logPath != "" {
		raw, readErr := os.ReadFile(logPath)
		if readErr != nil {
			return out, readErr
		}
		logs := string(raw)
		for _, secret := range []string{rawSecret, rawEmail, rawBearer, rawJWT, token, idempotencyKey} {
			if strings.Contains(logs, secret) {
				logSafe = false
			}
		}
	}

	allDurable := redactedDetails + "\n" + metadata
	publicRaw := resp.Raw
	out.RecordCounts = map[string]int{
		"correlated_report_event_pairs": correlated,
		"sensitive_schema_columns":      sensitiveColumns,
	}
	out.Checks = map[string]bool{
		"safe_resource_authority_is_server_resolved_and_correlated": resp.Status == http.StatusCreated && correlated == 1,
		"reporter_details_are_redacted_before_durable_storage": strings.Contains(redactedDetails, "[redacted-url]") && strings.Contains(redactedDetails, "[redacted-email]") && strings.Contains(redactedDetails, "[redacted]") && !strings.Contains(allDurable, rawSecret) && !strings.Contains(allDurable, rawEmail) && !strings.Contains(allDurable, rawBearer) && !strings.Contains(allDurable, rawJWT),
		"turnstile_and_remote_identity_have_no_durable_columns": sensitiveColumns == 0,
		"idempotency_and_request_authority_are_hash_only": len(requestFingerprint) == 64 && len(idempotencyHash) == 64 && idempotencyHash != idempotencyKey && requestFingerprint != details,
		"evidence_reference_is_opaque_not_provider_evidence": strings.HasPrefix(evidenceRef, "abuse-report:abr_") && !strings.Contains(evidenceRef, "http") && !strings.Contains(evidenceRef, rawSecret),
		"immutable_event_metadata_is_minimized": strings.Contains(metadata, "resource_type") && strings.Contains(metadata, "details_present") && !strings.Contains(metadata, link.Hostname) && !strings.Contains(metadata, link.Code) && !strings.Contains(metadata, link.Fingerprint),
		"public_receipt_exposes_no_resource_or_reporter_context": !strings.Contains(publicRaw, link.WorkspaceID) && !strings.Contains(publicRaw, link.Hostname) && !strings.Contains(publicRaw, link.Code) && !strings.Contains(publicRaw, link.Fingerprint) && !strings.Contains(publicRaw, rawSecret) && !strings.Contains(publicRaw, rawEmail),
		"ordinary_platformapi_log_contains_no_reporter_secret_or_pii": logSafe,
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
	return response{Status: resp.StatusCode, Body: decoded, Raw: string(bodyRaw)}, nil
}
