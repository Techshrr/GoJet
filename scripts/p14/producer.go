package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	filecore "github.com/Techshrr/GoJet/internal/files"
	"github.com/Techshrr/GoJet/internal/links"
	"github.com/Techshrr/GoJet/internal/support"
)

type output map[string]any

type attachmentComponents struct {
	store    *support.Store
	storage  *filecore.NativeStorage
	policy   *filecore.TypePolicy
	scanner  *filecore.ClamAVClient
	maxBytes int64
}

func main() {
	if len(os.Args) < 2 {
		fail("usage")
	}
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	if dsn == "" {
		fail("mysql_config")
	}
	db, err := links.OpenMySQL(dsn)
	if err != nil {
		fail("mysql_open")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		fail("mysql_unavailable")
	}

	var result output
	switch os.Args[1] {
	case "attachment-intake":
		result, err = attachmentIntake(ctx, db, os.Args[2:])
	case "attachment-scan":
		result, err = attachmentScan(ctx, db, os.Args[2:])
	case "attachment-download-check":
		result, err = attachmentDownloadCheck(ctx, db, os.Args[2:])
	case "claim-mail":
		result, err = claimMail(ctx, db)
	case "render-template":
		result, err = renderTemplate(ctx, db, os.Args[2:])
	default:
		fail("unknown_command")
	}
	if err != nil {
		if result == nil {
			result = output{}
		}
		result["ok"] = false
		result["error"] = safeErrorCode(err)
		write(result)
		os.Exit(2)
	}
	result["ok"] = true
	write(result)
}

func buildAttachmentComponents(db *sql.DB) (attachmentComponents, error) {
	root := strings.TrimSpace(os.Getenv("GOJET_TEST_P14_ATTACHMENT_ROOT"))
	if root == "" {
		return attachmentComponents{}, support.ErrInvalidInput
	}
	storage, err := filecore.NewNativeStorage(root)
	if err != nil {
		return attachmentComponents{}, err
	}
	policyRaw := strings.TrimSpace(os.Getenv("GOJET_TEST_P14_ATTACHMENT_POLICY"))
	if policyRaw == "" {
		policyRaw = "txt=text/plain"
	}
	policy, err := filecore.ParseTypePolicy(policyRaw)
	if err != nil {
		return attachmentComponents{}, err
	}
	network := strings.TrimSpace(os.Getenv("GOJET_TEST_P14_CLAMAV_NETWORK"))
	if network == "" {
		network = "tcp"
	}
	address := strings.TrimSpace(os.Getenv("GOJET_TEST_P14_CLAMAV_ADDRESS"))
	if address == "" {
		return attachmentComponents{}, support.ErrInvalidInput
	}
	dialTimeout, err := durationEnv("GOJET_TEST_P14_CLAMAV_DIAL_TIMEOUT", 2*time.Second)
	if err != nil {
		return attachmentComponents{}, err
	}
	scanTimeout, err := durationEnv("GOJET_TEST_P14_CLAMAV_SCAN_TIMEOUT", 5*time.Second)
	if err != nil {
		return attachmentComponents{}, err
	}
	maxAge, err := durationEnv("GOJET_TEST_P14_CLAMAV_MAX_SIGNATURE_AGE", 48*time.Hour)
	if err != nil {
		return attachmentComponents{}, err
	}
	clamav, err := filecore.NewClamAVClient(network, address, dialTimeout, scanTimeout, maxAge)
	if err != nil {
		return attachmentComponents{}, err
	}
	store, err := support.NewStore(db)
	if err != nil {
		return attachmentComponents{}, err
	}
	maxBytes := int64(65536)
	if raw := strings.TrimSpace(os.Getenv("GOJET_TEST_P14_ATTACHMENT_MAX_BYTES")); raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || parsed <= 0 {
			return attachmentComponents{}, support.ErrInvalidInput
		}
		maxBytes = parsed
	}
	return attachmentComponents{store: store, storage: storage, policy: policy, scanner: clamav, maxBytes: maxBytes}, nil
}

func attachmentIntake(ctx context.Context, db *sql.DB, args []string) (output, error) {
	if len(args) != 5 {
		return nil, support.ErrInvalidInput
	}
	file, err := os.Open(args[2])
	if err != nil {
		return nil, err
	}
	defer file.Close()
	components, err := buildAttachmentComponents(db)
	if err != nil {
		return nil, err
	}
	runtime, err := support.NewAttachmentRuntime(components.store, components.storage, components.policy, components.scanner, components.maxBytes)
	if err != nil {
		return nil, err
	}
	attachment, err := runtime.Intake(ctx, args[0], args[1], args[3], args[4], file, time.Now().UTC())
	if err != nil {
		return output{"created": false}, err
	}
	return output{
		"created":          true,
		"attachment":       attachment,
		"download_allowed": support.AttachmentDownloadAllowed(attachment),
	}, nil
}

func attachmentScan(ctx context.Context, db *sql.DB, args []string) (output, error) {
	if len(args) != 1 {
		return nil, support.ErrInvalidInput
	}
	components, err := buildAttachmentComponents(db)
	if err != nil {
		return nil, err
	}
	current, err := components.store.GetAttachment(ctx, args[0])
	if err != nil {
		return nil, err
	}
	startedAt := time.Now().UTC()
	scanning, err := support.BeginAttachmentScan(current, startedAt)
	if err != nil {
		return nil, err
	}
	if err := components.store.TransitionAttachment(ctx, current.ID, current.ScanStatus, scanning); err != nil {
		return nil, err
	}

	file, err := components.storage.OpenQuarantine(current.StorageKey)
	if err != nil {
		completedAt := time.Now().UTC()
		final, stateErr := support.CompleteAttachmentScan(scanning, support.AttachmentScanError, completedAt)
		if stateErr == nil {
			stateErr = components.store.TransitionAttachment(ctx, current.ID, support.AttachmentScanning, final)
		}
		if stateErr != nil {
			return nil, stateErr
		}
		return scanOutput(final, filecore.ScanResult{Verdict: filecore.VerdictError, ErrorCode: "quarantine_unavailable"}, true), err
	}
	result, scanErr := components.scanner.Scan(ctx, file)
	_ = file.Close()
	completedAt := time.Now().UTC()
	final, stateErr := support.ApplyP09ScanOutcome(scanning, result, scanErr, completedAt)
	if stateErr != nil {
		return nil, stateErr
	}
	if final.ScanStatus == support.AttachmentClean {
		if err := components.storage.Publish(current.StorageKey); err != nil {
			failed, failStateErr := support.CompleteAttachmentScan(scanning, support.AttachmentScanError, time.Now().UTC())
			if failStateErr == nil {
				failStateErr = components.store.TransitionAttachment(ctx, current.ID, support.AttachmentScanning, failed)
			}
			if failStateErr != nil {
				return nil, failStateErr
			}
			return scanOutput(failed, result, true), err
		}
		if err := components.store.TransitionAttachment(ctx, current.ID, support.AttachmentScanning, final); err != nil {
			_ = components.storage.ReturnToQuarantine(current.StorageKey)
			return nil, err
		}
	} else if err := components.store.TransitionAttachment(ctx, current.ID, support.AttachmentScanning, final); err != nil {
		return nil, err
	}
	if scanErr != nil {
		return scanOutput(final, result, true), scanErr
	}
	if result.Verdict != filecore.VerdictClean && result.Verdict != filecore.VerdictInfected {
		return scanOutput(final, result, true), filecore.ErrScanIndeterminate
	}
	return scanOutput(final, result, false), nil
}

func scanOutput(attachment support.Attachment, result filecore.ScanResult, failedClosed bool) output {
	return output{
		"attachment":         attachment,
		"verdict":            string(result.Verdict),
		"verdict_code":       result.VerdictCode,
		"scan_error_code":    result.ErrorCode,
		"download_allowed":   support.AttachmentDownloadAllowed(attachment),
		"scan_failed_closed": failedClosed,
	}
}

func attachmentDownloadCheck(ctx context.Context, db *sql.DB, args []string) (output, error) {
	if len(args) != 1 {
		return nil, support.ErrInvalidInput
	}
	components, err := buildAttachmentComponents(db)
	if err != nil {
		return nil, err
	}
	runtime, err := support.NewAttachmentRuntime(components.store, components.storage, components.policy, components.scanner, components.maxBytes)
	if err != nil {
		return nil, err
	}
	attachment, file, err := runtime.OpenDownload(ctx, args[0])
	if err != nil {
		return output{"allowed": false}, err
	}
	defer file.Close()
	return output{"allowed": true, "attachment": attachment}, nil
}

func claimMail(ctx context.Context, db *sql.DB) (output, error) {
	store, err := support.NewMySQLMailStore(db)
	if err != nil {
		return nil, err
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	claimed, err := store.ClaimNext(ctx, hex.EncodeToString(raw[:]), time.Now().UTC())
	if errors.Is(err, support.ErrNoMailAvailable) {
		return output{"claimed": false}, nil
	}
	if err != nil {
		return nil, err
	}
	return output{
		"claimed":       true,
		"job_id":        claimed.Job.ID,
		"status":        claimed.Job.Status,
		"attempt_count": claimed.Job.AttemptCount,
	}, nil
}

func renderTemplate(ctx context.Context, db *sql.DB, args []string) (output, error) {
	if len(args) != 3 {
		return nil, support.ErrInvalidInput
	}
	key := strings.TrimSpace(args[0])
	locale := strings.TrimSpace(args[1])
	var values map[string]string
	if err := json.Unmarshal([]byte(args[2]), &values); err != nil {
		return nil, support.ErrTemplateVariable
	}
	var template support.MailTemplate
	var allowlist []byte
	var internalOnly bool
	err := db.QueryRowContext(ctx, `
SELECT template_key,version,subject_template,text_template,html_template,variable_allowlist_json,internal_only
FROM mail_templates WHERE template_key=? AND locale=? AND enabled=1 ORDER BY version DESC LIMIT 1`, key, locale).
		Scan(&template.Key, &template.Version, &template.SubjectTemplate, &template.TextTemplate, &template.HTMLTemplate, &allowlist, &internalOnly)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, support.ErrTemplateVariable
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(allowlist, &template.VariableAllowlist); err != nil {
		return nil, support.ErrTemplateVariable
	}
	template.InternalOnlyTemplate = internalOnly
	rendered, err := support.RenderMailTemplate(template, values)
	if err != nil {
		return output{"template_key": key, "version": template.Version}, err
	}
	return output{
		"template_key":     key,
		"version":          template.Version,
		"subject_nonempty": strings.TrimSpace(rendered.Subject) != "",
		"text_nonempty":    strings.TrimSpace(rendered.Text) != "",
		"html_nonempty":    strings.TrimSpace(rendered.HTML) != "",
	}, nil
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, support.ErrInvalidInput
	}
	return value, nil
}

func safeErrorCode(err error) string {
	switch {
	case errors.Is(err, support.ErrAttachmentBlocked):
		return "attachment_blocked"
	case errors.Is(err, support.ErrSupportNotFound):
		return "not_found"
	case errors.Is(err, support.ErrSupportConflict):
		return "conflict"
	case errors.Is(err, support.ErrInvalidInput):
		return "invalid_input"
	case errors.Is(err, support.ErrTemplateVariable), errors.Is(err, support.ErrSensitiveVariable):
		return "template_rejected"
	case errors.Is(err, filecore.ErrSignatureStale):
		return "signature_stale"
	case errors.Is(err, filecore.ErrScanIndeterminate):
		return "scan_indeterminate"
	default:
		return "operation_failed"
	}
}

func write(value output) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(true)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, "encode failed")
		os.Exit(3)
	}
}

func fail(code string) {
	write(output{"ok": false, "error": code})
	os.Exit(2)
}
