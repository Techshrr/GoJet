package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/securetoken"
	"github.com/Techshrr/GoJet/internal/support"
	_ "github.com/go-sql-driver/mysql"
)

type runnerOutput struct {
	Errors  []string       `json:"errors"`
	Details map[string]any `json:"details"`
}

type captureSender struct {
	expectedRecipient string
	code              string
	deliveries        int
	captureError      string
}

var verificationCodePattern = regexp.MustCompile(`gvc_[A-Za-z0-9_-]{36,124}`)

func (s *captureSender) Send(_ context.Context, recipient string, rendered support.RenderedMail) support.MailDeliveryResult {
	if strings.TrimSpace(recipient) != s.expectedRecipient {
		s.captureError = "rendered verification mail recipient did not match the T009 account"
		return support.MailDeliveryResult{ErrorCode: "p20_capture_recipient_mismatch"}
	}
	if strings.TrimSpace(rendered.Subject) == "" || strings.TrimSpace(rendered.Text) == "" || strings.TrimSpace(rendered.HTML) == "" {
		s.captureError = "rendered verification mail was incomplete"
		return support.MailDeliveryResult{ErrorCode: "p20_capture_render_invalid"}
	}
	codes := uniqueCodes(rendered.Text + "\n" + rendered.HTML)
	if len(codes) != 1 {
		s.captureError = "rendered verification mail did not contain exactly one verification code"
		return support.MailDeliveryResult{ErrorCode: "p20_capture_code_invalid"}
	}
	s.code = codes[0]
	s.deliveries++
	return support.MailDeliveryResult{Success: true}
}

func uniqueCodes(value string) []string {
	matches := verificationCodePattern.FindAllString(value, -1)
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if _, ok := seen[match]; ok {
			continue
		}
		seen[match] = struct{}{}
		out = append(out, match)
	}
	return out
}

func emit(errors []string, details map[string]any) {
	if errors == nil {
		errors = []string{}
	}
	if details == nil {
		details = map[string]any{}
	}
	if _, ok := details["mock_authority"]; !ok {
		details["mock_authority"] = false
	}
	if _, ok := details["token_rule_bypass"]; !ok {
		details["token_rule_bypass"] = false
	}
	if _, ok := details["secret_material_recorded"]; !ok {
		details["secret_material_recorded"] = false
	}
	_ = json.NewEncoder(os.Stdout).Encode(runnerOutput{Errors: errors, Details: details})
}

func fail(label string, details map[string]any) {
	emit([]string{label}, details)
}

func count(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var value int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

func loadGrantKey() (securetoken.Key, error) {
	keyID := strings.TrimSpace(os.Getenv("GOJET_AUTH_GRANT_KEY_ID"))
	keyHex := strings.TrimSpace(os.Getenv("GOJET_AUTH_GRANT_KEY_HEX"))
	if keyID == "" || keyHex == "" {
		return securetoken.Key{}, fmt.Errorf("missing grant key configuration")
	}
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil || len(keyBytes) != 32 {
		return securetoken.Key{}, fmt.Errorf("invalid grant key configuration")
	}
	return securetoken.NewKey(keyID, keyBytes)
}

func postJSON(ctx context.Context, base, path string, payload map[string]string) (int, map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "p20-t010-runtime")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return 0, nil, err
	}
	decoded := map[string]any{}
	if len(body) != 0 {
		if err := json.Unmarshal(body, &decoded); err != nil {
			return resp.StatusCode, map[string]any{"_non_json": true}, nil
		}
	}
	return resp.StatusCode, decoded, nil
}

func bodyStatus(body map[string]any) string {
	value, _ := body["status"].(string)
	return value
}

func problemCode(body map[string]any) string {
	problem, _ := body["error"].(map[string]any)
	value, _ := problem["code"].(string)
	return value
}

func main() {
	details := map[string]any{
		"real_platform_api":         false,
		"real_auth_mail_queue":      false,
		"real_p14_mail_worker":      false,
		"mock_authority":            false,
		"token_rule_bypass":         false,
		"secret_material_recorded":  false,
		"verification_code_storage": "memory_only",
	}

	head := strings.TrimSpace(os.Getenv("P20_EXACT_HEAD"))
	if len(head) != 40 {
		fail("T010 exact-head binding is missing", details)
		return
	}
	suffix := strings.ToLower(head[:12])
	email := "p20-t009-" + suffix + "@example.test"
	registerCorrelation := "p20-t009-" + suffix
	verifyCorrelation := "p20-t010-" + suffix
	replayCorrelation := verifyCorrelation + "-replay"
	apiBase := strings.TrimSpace(os.Getenv("GOJET_P20_API_BASE"))
	if apiBase == "" {
		fail("T010 real platform API endpoint is missing", details)
		return
	}

	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	if dsn == "" {
		fail("T010 MySQL authority is not configured", details)
		return
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fail("T010 could not initialize MySQL authority", details)
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		fail("T010 could not reach MySQL authority", details)
		return
	}

	userCount, err := count(ctx, db, `SELECT COUNT(*) FROM auth_users WHERE email_normalized=?`, email)
	if err != nil || userCount != 1 {
		fail("T010 did not find exactly one T009 account", details)
		return
	}
	var userID, userStatus string
	if err := db.QueryRowContext(ctx, `SELECT id,status FROM auth_users WHERE email_normalized=?`, email).Scan(&userID, &userStatus); err != nil {
		fail("T010 could not load the T009 account", details)
		return
	}
	if userStatus != auth.UserStatusPendingVerification {
		fail("T010 T009 account was not pending verification", details)
		return
	}
	details["user_id"] = userID
	details["t009_account_status_before"] = userStatus

	workspaceCount, err := count(ctx, db, `SELECT COUNT(*) FROM workspace_memberships WHERE user_id=?`, userID)
	if err != nil || workspaceCount != 1 {
		fail("T010 T009 account did not retain exactly one Workspace membership", details)
		return
	}
	var workspaceID, workspaceRole string
	if err := db.QueryRowContext(ctx, `SELECT workspace_id,role FROM workspace_memberships WHERE user_id=?`, userID).Scan(&workspaceID, &workspaceRole); err != nil {
		fail("T010 could not load the T009 Workspace correlation", details)
		return
	}
	if workspaceRole != "owner" {
		fail("T010 T009 Workspace correlation was not owner authority", details)
		return
	}
	details["workspace_id"] = workspaceID
	details["workspace_role"] = workspaceRole

	grantCount, err := count(ctx, db, `SELECT COUNT(*) FROM auth_one_time_grants WHERE user_id=? AND purpose='email_verification'`, userID)
	if err != nil || grantCount != 1 {
		fail("T010 did not find exactly one T009 email-verification grant", details)
		return
	}
	var (
		grantID          string
		grantCorrelation string
		tokenHash        []byte
		tokenKeyID       sql.NullString
		grantConsumed    sql.NullTime
		grantInvalidated sql.NullTime
	)
	if err := db.QueryRowContext(ctx, `
SELECT id,correlation_id,token_hash,token_key_id,consumed_at,invalidated_at
FROM auth_one_time_grants
WHERE user_id=? AND purpose='email_verification'`, userID).Scan(
		&grantID, &grantCorrelation, &tokenHash, &tokenKeyID, &grantConsumed, &grantInvalidated,
	); err != nil {
		fail("T010 could not load the T009 verification grant", details)
		return
	}
	details["verification_grant_id"] = grantID
	details["grant_correlated_to_t009"] = grantCorrelation == registerCorrelation
	if grantCorrelation != registerCorrelation || len(tokenHash) != 32 || !tokenKeyID.Valid || grantConsumed.Valid || grantInvalidated.Valid {
		fail("T010 T009 verification grant was not live and correctly correlated", details)
		return
	}

	mailCount, err := count(ctx, db, `
SELECT COUNT(*) FROM mail_jobs
WHERE template_key='auth-email-verification' AND resource_type='auth_one_time_grant' AND resource_id=?`, grantID)
	if err != nil || mailCount != 1 {
		fail("T010 did not find exactly one queued T009 verification mail job", details)
		return
	}
	var mailJobID, mailStatus, mailRecipient string
	var mailAttemptCount uint32
	if err := db.QueryRowContext(ctx, `
SELECT id,status,attempt_count,recipient_value FROM mail_jobs
WHERE template_key='auth-email-verification' AND resource_type='auth_one_time_grant' AND resource_id=?`, grantID).
		Scan(&mailJobID, &mailStatus, &mailAttemptCount, &mailRecipient); err != nil {
		fail("T010 could not load the queued T009 verification mail job", details)
		return
	}
	details["verification_mail_job_id"] = mailJobID
	details["queued_mail_observed"] = mailStatus == string(support.MailQueued) && mailAttemptCount == 0
	if mailStatus != string(support.MailQueued) || mailAttemptCount != 0 || mailRecipient != email {
		fail("T010 verification mail was not in the expected queued T009 state", details)
		return
	}

	grantKey, err := loadGrantKey()
	if err != nil || !tokenKeyID.Valid || tokenKeyID.String != grantKey.ID() {
		fail("T010 verification grant key identity did not match runtime authority", details)
		return
	}
	queue, err := auth.NewAuthMailQueue(db, grantKey)
	if err != nil {
		fail("T010 could not initialize the real AuthMailQueue", details)
		return
	}
	sender := &captureSender{expectedRecipient: email}
	worker, err := support.NewMailWorker(queue, sender)
	if err != nil {
		fail("T010 could not initialize the inherited P14 MailWorker", details)
		return
	}
	details["real_auth_mail_queue"] = true
	details["real_p14_mail_worker"] = true
	worked, err := worker.RunOnce(ctx)
	if err != nil || !worked {
		fail("T010 real MailWorker did not deliver the queued verification mail", details)
		return
	}
	if sender.captureError != "" || sender.deliveries != 1 || sender.code == "" {
		label := sender.captureError
		if label == "" {
			label = "T010 verification mail capture did not observe exactly one rendered delivery"
		}
		fail(label, details)
		return
	}
	details["rendered_mail_delivery_count"] = sender.deliveries
	details["verification_code_source"] = "rendered_mail_memory_only"

	derivedHash := securetoken.Hash(sender.code)
	var storedHash [32]byte
	copy(storedHash[:], tokenHash)
	tokenHashMatch := len(tokenHash) == 32 && derivedHash == storedHash
	details["rendered_code_matches_stored_hash"] = tokenHashMatch
	if !tokenHashMatch {
		fail("T010 rendered verification code did not match the durable grant hash", details)
		return
	}

	secondWorked, err := worker.RunOnce(ctx)
	if err != nil || secondWorked {
		fail("T010 verification mail queue was not drained after one logical delivery", details)
		return
	}
	details["mail_queue_drained_after_delivery"] = true

	var mailStatusAfter string
	var mailAttemptsAfter uint32
	if err := db.QueryRowContext(ctx, `SELECT status,attempt_count FROM mail_jobs WHERE id=?`, mailJobID).Scan(&mailStatusAfter, &mailAttemptsAfter); err != nil {
		fail("T010 could not read delivered mail state", details)
		return
	}
	mailAttemptRows, err := count(ctx, db, `SELECT COUNT(*) FROM mail_attempts WHERE mail_job_id=? AND attempt_number=1 AND status='sent'`, mailJobID)
	if err != nil {
		fail("T010 could not read mail attempt authority", details)
		return
	}
	mailAuditRows, err := count(ctx, db, `
SELECT COUNT(*) FROM auth_audit_events
WHERE user_id=? AND action='auth.mail.attempt.sent' AND resource_type='mail_job' AND resource_id=? AND result='success'`, userID, mailJobID)
	if err != nil {
		fail("T010 could not read auth mail audit authority", details)
		return
	}
	var grantConsumedAfterMail, grantInvalidatedAfterMail int
	if err := db.QueryRowContext(ctx, `
SELECT IF(consumed_at IS NULL,0,1),IF(invalidated_at IS NULL,0,1)
FROM auth_one_time_grants WHERE id=?`, grantID).Scan(&grantConsumedAfterMail, &grantInvalidatedAfterMail); err != nil {
		fail("T010 could not confirm grant state after mail delivery", details)
		return
	}
	details["mail_status_after_delivery"] = mailStatusAfter
	details["mail_attempt_count"] = mailAttemptsAfter
	details["mail_attempt_rows"] = mailAttemptRows
	details["mail_audit_rows"] = mailAuditRows
	details["grant_unconsumed_after_mail_delivery"] = grantConsumedAfterMail == 0 && grantInvalidatedAfterMail == 0
	if mailStatusAfter != string(support.MailSent) || mailAttemptsAfter != 1 || mailAttemptRows != 1 || mailAuditRows != 1 || grantConsumedAfterMail != 0 || grantInvalidatedAfterMail != 0 {
		fail("T010 mail delivery lifecycle did not preserve an unconsumed live verification grant", details)
		return
	}

	verifyStatus, verifyBody, err := postJSON(ctx, apiBase, "/api/auth/verifyemail", map[string]string{
		"code":           sender.code,
		"correlation_id": verifyCorrelation,
	})
	if err != nil {
		fail("T010 could not execute the real email-verification API", details)
		return
	}
	details["real_platform_api"] = true
	details["verification_http_status"] = verifyStatus
	details["verification_response_status"] = bodyStatus(verifyBody)
	if verifyStatus != http.StatusOK || bodyStatus(verifyBody) != "verified" {
		fail("T010 real email-verification API did not complete the verified transition", details)
		return
	}

	var accountStatusAfter string
	var emailVerifiedAfter int
	var accountVersionAfter uint64
	if err := db.QueryRowContext(ctx, `SELECT status,IF(email_verified_at IS NULL,0,1),version FROM auth_users WHERE id=?`, userID).
		Scan(&accountStatusAfter, &emailVerifiedAfter, &accountVersionAfter); err != nil {
		fail("T010 could not read verified account state", details)
		return
	}
	var grantConsumedFlag, grantInvalidatedFlag int
	var grantAttemptCount uint32
	var consumedStamp string
	if err := db.QueryRowContext(ctx, `
SELECT IF(consumed_at IS NULL,0,1),IF(invalidated_at IS NULL,0,1),attempt_count,
       COALESCE(DATE_FORMAT(consumed_at,'%Y-%m-%dT%H:%i:%s.%fZ'),'')
FROM auth_one_time_grants WHERE id=?`, grantID).
		Scan(&grantConsumedFlag, &grantInvalidatedFlag, &grantAttemptCount, &consumedStamp); err != nil {
		fail("T010 could not read consumed verification grant state", details)
		return
	}
	verificationAuditRows, err := count(ctx, db, `
SELECT COUNT(*) FROM auth_audit_events
WHERE user_id=? AND action='auth.email_verification.completed' AND resource_type='auth_one_time_grant'
  AND resource_id=? AND request_correlation_id=? AND result='success'`, userID, grantID, verifyCorrelation)
	if err != nil {
		fail("T010 could not read verification completion audit authority", details)
		return
	}
	details["account_status_after_verification"] = accountStatusAfter
	details["email_verified"] = emailVerifiedAfter == 1
	details["verification_grant_consumed"] = grantConsumedFlag == 1
	details["verification_grant_invalidated"] = grantInvalidatedFlag == 1
	details["verification_grant_attempt_count"] = grantAttemptCount
	details["verification_audit_rows"] = verificationAuditRows
	if accountStatusAfter != auth.UserStatusActive || emailVerifiedAfter != 1 || grantConsumedFlag != 1 || grantInvalidatedFlag != 0 || grantAttemptCount != 1 || consumedStamp == "" || verificationAuditRows != 1 {
		fail("T010 durable verified-account or consumed-grant transition was incomplete", details)
		return
	}

	workspaceCountAfter, err := count(ctx, db, `SELECT COUNT(*) FROM workspace_memberships WHERE user_id=? AND workspace_id=? AND role='owner'`, userID, workspaceID)
	if err != nil || workspaceCountAfter != 1 {
		fail("T010 verification transition lost the T009 owner Workspace correlation", details)
		return
	}
	details["workspace_correlation_preserved"] = true

	replayStatus, replayBody, err := postJSON(ctx, apiBase, "/api/auth/verifyemail", map[string]string{
		"code":           sender.code,
		"correlation_id": replayCorrelation,
	})
	if err != nil {
		fail("T010 could not execute the verification replay probe", details)
		return
	}
	replayCode := problemCode(replayBody)
	details["replay_http_status"] = replayStatus
	details["replay_error_code"] = replayCode
	if replayStatus != http.StatusConflict || replayCode != "reused_token" {
		fail("T010 reused verification code did not fail closed as replay", details)
		return
	}

	var accountStatusReplay string
	var emailVerifiedReplay int
	var accountVersionReplay uint64
	if err := db.QueryRowContext(ctx, `SELECT status,IF(email_verified_at IS NULL,0,1),version FROM auth_users WHERE id=?`, userID).
		Scan(&accountStatusReplay, &emailVerifiedReplay, &accountVersionReplay); err != nil {
		fail("T010 could not read account state after replay", details)
		return
	}
	var grantConsumedReplay, grantInvalidatedReplay int
	var grantAttemptReplay uint32
	var consumedStampReplay string
	if err := db.QueryRowContext(ctx, `
SELECT IF(consumed_at IS NULL,0,1),IF(invalidated_at IS NULL,0,1),attempt_count,
       COALESCE(DATE_FORMAT(consumed_at,'%Y-%m-%dT%H:%i:%s.%fZ'),'')
FROM auth_one_time_grants WHERE id=?`, grantID).
		Scan(&grantConsumedReplay, &grantInvalidatedReplay, &grantAttemptReplay, &consumedStampReplay); err != nil {
		fail("T010 could not read grant state after replay", details)
		return
	}
	verificationAuditRowsReplay, err := count(ctx, db, `
SELECT COUNT(*) FROM auth_audit_events
WHERE user_id=? AND action='auth.email_verification.completed' AND resource_type='auth_one_time_grant'
  AND resource_id=? AND result='success'`, userID, grantID)
	if err != nil {
		fail("T010 could not read verification audit state after replay", details)
		return
	}
	var mailStatusReplay string
	var mailAttemptsReplay uint32
	if err := db.QueryRowContext(ctx, `SELECT status,attempt_count FROM mail_jobs WHERE id=?`, mailJobID).Scan(&mailStatusReplay, &mailAttemptsReplay); err != nil {
		fail("T010 could not read mail state after replay", details)
		return
	}
	terminalUnchanged := accountStatusReplay == accountStatusAfter &&
		emailVerifiedReplay == emailVerifiedAfter &&
		accountVersionReplay == accountVersionAfter &&
		grantConsumedReplay == grantConsumedFlag &&
		grantInvalidatedReplay == grantInvalidatedFlag &&
		grantAttemptReplay == grantAttemptCount &&
		consumedStampReplay == consumedStamp &&
		verificationAuditRowsReplay == verificationAuditRows &&
		mailStatusReplay == mailStatusAfter &&
		mailAttemptsReplay == mailAttemptsAfter
	details["replay_terminal_state_unchanged"] = terminalUnchanged
	if !terminalUnchanged {
		fail("T010 replay mutated verified account, grant, audit or mail terminal state", details)
		return
	}

	details["next_case"] = "P20-T011"
	emit([]string{}, details)
}
