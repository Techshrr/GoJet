package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
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
	Status               string   `json:"status"`
	ImplementationCommit string   `json:"implementation_commit"`
	CaseRange            string   `json:"case_range"`
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
		"turnstile_token": "XXXX.DUMMY.TOKEN.XXXX",
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

	ticketID := nestedString(created.Body, "ticket", "id")
	result.Details["ticket_create_identity_bound"] = nestedString(created.Body, "ticket", "workspace_id") == workspaceID
	result.Details["ticket_id_present"] = ticketID != ""
	if result.Details["ticket_create_identity_bound"] != true || ticketID == "" {
		fail("T020 requester ticket create did not preserve correlated Workspace identity")
		return
	}

	// P15 CSRF is single-use across Account and Workspace/Support unsafe routes.
	// Obtain a fresh token from the same authenticated customer session before
	// proving requester reply continuity.
	replyCSRF, _, err := issueCSRF(ctx, apiBase, cookieHeader, userID)
	if err != nil {
		fail("T020 could not renew real customer CSRF authority for requester reply")
		return
	}
	requesterRepliesBefore, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM support_ticket_messages WHERE ticket_id=? AND kind='requester_reply' AND actor_id=?`, ticketID, userID)
	if err != nil {
		fail("T020 could not inspect requester reply durable state")
		return
	}
	requesterCorrelation := "p20-t020-requester-reply-" + suffix
	requesterReply, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/support/tickets/"+ticketID+"/replies", map[string]any{
		"message": "P20 T020 real requester follow-up.",
	}, mergeHeaders(unsafeHeaders(cookieHeader, origin, replyCSRF, requesterCorrelation), map[string]string{
		"Idempotency-Key": requesterCorrelation,
	}))
	if err != nil {
		fail("T020 real requester reply request failed")
		return
	}
	requesterRepliesAfter, _ := scalarInt(ctx, db, `SELECT COUNT(*) FROM support_ticket_messages WHERE ticket_id=? AND kind='requester_reply' AND actor_id=?`, ticketID, userID)
	result.Details["requester_reply_http_status"] = requesterReply.Status
	result.Details["requester_reply_error_code"] = nestedErrorCode(requesterReply.Body)
	result.Details["requester_reply_row_delta"] = requesterRepliesAfter - requesterRepliesBefore
	if requesterReply.Status != http.StatusCreated || requesterRepliesAfter-requesterRepliesBefore != 1 {
		fail("real authenticated requester session could not persist the correlated Support reply")
		return
	}
	result.Details["requester_reply_persisted"] = true

	// Establish independent P17 administrator authority on the same real MySQL
	// and Redis state, then prove that the production Admin HTTP surface accepts
	// that session before attempting the P14 Support Admin reply boundary.
	adminAuthority, err := establishRealP17AdminAuthority(ctx, apiBase, suffix)
	if err != nil {
		result.Details["p17_admin_authority_error"] = err.Error()
		fail("T020 could not establish real P17 Admin session/permission authority")
		return
	}
	result.Details["p17_admin_session_authenticated"] = true
	result.Details["p17_tickets_manage"] = true
	result.Details["p17_mail_manage"] = true
	result.Details["p17_admin_id_present"] = adminAuthority.AdministratorID != ""

	adminRepliesBefore, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM support_ticket_messages WHERE ticket_id=? AND kind='support_reply'`, ticketID)
	if err != nil {
		fail("T020 could not inspect pre-Admin-reply durable state")
		return
	}
	adminCorrelation := "p20-t020-admin-reply-" + suffix
	adminReply, err := requestJSON(ctx, apiBase, http.MethodPost, "/api/admin/support/tickets/"+ticketID+"/replies", map[string]any{
		"kind":    "support_reply",
		"message": "P20 T020 real administrator reply.",
	}, mergeHeaders(adminUnsafeHeaders(adminAuthority, adminCorrelation), map[string]string{
		"Idempotency-Key": adminCorrelation,
	}))
	if err != nil {
		fail("T020 real P17 Admin Support reply request failed")
		return
	}
	adminRepliesAfter, _ := scalarInt(ctx, db, `SELECT COUNT(*) FROM support_ticket_messages WHERE ticket_id=? AND kind='support_reply'`, ticketID)
	result.Details["admin_reply_http_status"] = adminReply.Status
	result.Details["admin_reply_error_code"] = nestedErrorCode(adminReply.Body)
	result.Details["admin_reply_row_delta"] = adminRepliesAfter - adminRepliesBefore
	if adminReply.Status != http.StatusCreated || adminRepliesAfter-adminRepliesBefore != 1 {
		result.Details["admin_reply_failed_without_write"] = adminRepliesAfter == adminRepliesBefore
		fail("real P17 Admin authority is not accepted by the Support Admin reply surface")
		return
	}
	result.Details["admin_reply_persisted"] = true
	adminMessageID := nestedString(adminReply.Body, "message", "id")
	result.Details["admin_reply_message_id_present"] = adminMessageID != ""
	if adminMessageID == "" {
		fail("T020 real Admin reply did not expose its durable message identity")
		return
	}

	if err := proveT020MailAndAttachment(ctx, db, ticketID, adminMessageID, suffix, &result); err != nil {
		result.Details["full_lifecycle_error"] = err.Error()
		fail("T020 real Support lifecycle did not preserve mail/attachment authority")
		return
	}
	result.Details["t020_full_lifecycle_proven"] = true
	finish()
}

func proveT020MailAndAttachment(ctx context.Context, db *sql.DB, ticketID, adminMessageID, suffix string, result *probe) error {
	if db == nil || result == nil || strings.TrimSpace(ticketID) == "" || strings.TrimSpace(adminMessageID) == "" {
		return fmt.Errorf("invalid T020 lifecycle authority")
	}

	var mailJobID, mailStatus string
	err := db.QueryRowContext(ctx, `
SELECT id,status FROM mail_jobs
WHERE template_key='support-ticket-reply' AND resource_type='ticket_message' AND resource_id=?
ORDER BY created_at DESC,id DESC LIMIT 1`, adminMessageID).Scan(&mailJobID, &mailStatus)
	if err != nil || strings.TrimSpace(mailJobID) == "" {
		return fmt.Errorf("Admin reply mail job correlation missing: %w", err)
	}
	result.Details["admin_reply_mail_job_bound"] = true
	result.Details["admin_reply_mail_job_id_present"] = true
	result.Details["admin_reply_mail_initial_status"] = mailStatus

	smtpCmd, smtpLog, err := startT020SMTPSink(ctx)
	if err != nil {
		return err
	}
	defer stopT020Process(smtpCmd, smtpLog)

	mailCmd, mailLog, err := startT020Mailworker(ctx)
	if err != nil {
		return err
	}
	defer stopT020Process(mailCmd, mailLog)

	mailStatus, err = waitT020MailStatus(ctx, db, mailJobID, "sent", 25*time.Second)
	if err != nil {
		return err
	}
	result.Details["admin_reply_mail_final_status"] = mailStatus
	result.Details["native_mailworker"] = true

	var attemptStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM mail_attempts WHERE mail_job_id=? ORDER BY attempt_number DESC LIMIT 1`, mailJobID).Scan(&attemptStatus); err != nil || attemptStatus != "sent" {
		return fmt.Errorf("Admin reply mail attempt not durably sent: status=%q err=%v", attemptStatus, err)
	}
	result.Details["admin_reply_mail_attempt_status"] = attemptStatus

	smtpStatePath := strings.TrimSpace(os.Getenv("GOJET_TEST_P14_SMTP_STATE"))
	var smtpState map[string]any
	if smtpStatePath == "" || readJSON(smtpStatePath, &smtpState) != nil {
		return fmt.Errorf("T020 SMTP sink state unavailable")
	}
	deliveries := numericInt(smtpState["deliveries"])
	messageDigest := strings.TrimSpace(stringValue(smtpState["last_message_sha256"]))
	if deliveries < 1 || len(messageDigest) != 64 {
		return fmt.Errorf("T020 SMTP evidence incomplete deliveries=%d digest_length=%d", deliveries, len(messageDigest))
	}
	result.Details["smtp_deliveries"] = deliveries
	result.Details["smtp_message_sha256_length"] = len(messageDigest)
	result.Details["real_smtp_delivery"] = true

	producerPath := strings.TrimSpace(os.Getenv("GOJET_TEST_P14_PRODUCER"))
	if producerPath == "" {
		return fmt.Errorf("P14 attachment producer path missing")
	}
	eicarPath := filepath.Join(os.TempDir(), "gojet-p20", "t020-eicar-"+suffix+".txt")
	if err := os.MkdirAll(filepath.Dir(eicarPath), 0o755); err != nil {
		return err
	}
	eicar := []byte("X5O!P%@AP[4\\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*\n")
	if err := os.WriteFile(eicarPath, eicar, 0o600); err != nil {
		return err
	}
	defer os.Remove(eicarPath)

	intakeRC, intake, err := runT020Producer(ctx, producerPath, "attachment-intake", ticketID, adminMessageID, eicarPath, "t020-eicar.txt", "text/plain")
	if err != nil || intakeRC != 0 {
		return fmt.Errorf("T020 EICAR attachment intake failed rc=%d err=%v", intakeRC, err)
	}
	attachmentID := nestedString(intake, "attachment", "id")
	if attachmentID == "" {
		return fmt.Errorf("T020 EICAR attachment identity missing")
	}
	result.Details["attachment_id_present"] = true

	scanRC, scan, err := runT020Producer(ctx, producerPath, "attachment-scan", attachmentID)
	if err != nil || scanRC != 0 || nestedString(scan, "attachment", "scan_status") != "infected" {
		return fmt.Errorf("T020 EICAR scan did not persist infected rc=%d err=%v", scanRC, err)
	}
	var durableScan string
	if err := db.QueryRowContext(ctx, `SELECT scan_status FROM support_ticket_attachments WHERE id=? AND ticket_id=? AND message_id=?`, attachmentID, ticketID, adminMessageID).Scan(&durableScan); err != nil || durableScan != "infected" {
		return fmt.Errorf("T020 infected attachment durable linkage mismatch status=%q err=%v", durableScan, err)
	}
	result.Details["attachment_bound_to_admin_reply"] = true
	result.Details["attachment_scan_status"] = durableScan
	result.Details["real_clamav_infected"] = true

	downloadRC, download, err := runT020Producer(ctx, producerPath, "attachment-download-check", attachmentID)
	if err != nil {
		return err
	}
	allowed, _ := download["allowed"].(bool)
	if downloadRC == 0 || allowed {
		return fmt.Errorf("T020 infected attachment was downloadable rc=%d allowed=%v", downloadRC, allowed)
	}
	result.Details["infected_attachment_download_blocked"] = true
	return nil
}

func startT020SMTPSink(ctx context.Context) (*exec.Cmd, *os.File, error) {
	statePath := strings.TrimSpace(os.Getenv("GOJET_TEST_P14_SMTP_STATE"))
	modePath := strings.TrimSpace(os.Getenv("GOJET_TEST_P14_SMTP_MODE"))
	addr := strings.TrimSpace(os.Getenv("GOJET_SMTP_ADDR"))
	if statePath == "" || modePath == "" || addr == "" {
		return nil, nil, fmt.Errorf("T020 SMTP fixture configuration missing")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, nil, err
	}
	_ = os.Remove(statePath)
	if err := os.WriteFile(modePath, []byte("success\n"), 0o644); err != nil {
		return nil, nil, err
	}
	logPath := filepath.Join(os.TempDir(), "gojet-p20", "t020-smtp.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return nil, nil, err
	}
	log, err := os.Create(logPath)
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.CommandContext(ctx, "python3", "scripts/p14/smtp_sink.py", "--host", host, "--port", port, "--state", statePath, "--mode-file", modePath)
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		log.Close()
		return nil, nil, err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if dialErr == nil {
			_ = conn.SetReadDeadline(time.Now().Add(time.Second))
			buf := make([]byte, 256)
			n, _ := conn.Read(buf)
			_ = conn.Close()
			if strings.HasPrefix(string(buf[:n]), "220 ") {
				return cmd, log, nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	stopT020Process(cmd, log)
	return nil, nil, fmt.Errorf("T020 SMTP sink did not become ready")
}

func startT020Mailworker(ctx context.Context) (*exec.Cmd, *os.File, error) {
	path := strings.TrimSpace(os.Getenv("GOJET_TEST_P14_MAILWORKER"))
	if path == "" {
		return nil, nil, fmt.Errorf("T020 mailworker path missing")
	}
	logPath := filepath.Join(os.TempDir(), "gojet-p20", "t020-mailworker.log")
	log, err := os.Create(logPath)
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.CommandContext(ctx, path)
	cmd.Env = os.Environ()
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		log.Close()
		return nil, nil, err
	}
	return cmd, log, nil
}

func stopT020Process(cmd *exec.Cmd, log *os.File) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	if log != nil {
		_ = log.Close()
	}
}

func waitT020MailStatus(ctx context.Context, db *sql.DB, jobID, wanted string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	last := ""
	for time.Now().Before(deadline) {
		if err := db.QueryRowContext(ctx, `SELECT status FROM mail_jobs WHERE id=?`, jobID).Scan(&last); err == nil {
			if last == wanted {
				return last, nil
			}
			if last == "failed" {
				return last, fmt.Errorf("mail job failed before %s", wanted)
			}
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return last, fmt.Errorf("mail job did not reach %s; last=%s", wanted, last)
}

func runT020Producer(ctx context.Context, path string, args ...string) (int, map[string]any, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = os.Environ()
	raw, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return -1, nil, err
		}
		exitCode = exitErr.ExitCode()
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	if len(lines) == 0 || len(lines[len(lines)-1]) == 0 {
		return exitCode, nil, fmt.Errorf("producer emitted no JSON")
	}
	data := map[string]any{}
	if err := json.Unmarshal(lines[len(lines)-1], &data); err != nil {
		return exitCode, nil, fmt.Errorf("producer JSON decode: %w", err)
	}
	return exitCode, data, nil
}

func numericInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	default:
		return 0
	}
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
