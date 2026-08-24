package support

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrNoMailAvailable = errors.New("no mail job available")

type ClaimedMail struct {
	Job    MailJob
	Locale string
}

type MailDeliveryPayload struct {
	Template  MailTemplate
	Values    map[string]string
	Recipient string
}

type MySQLMailStore struct {
	db *sql.DB
}

func NewMySQLMailStore(db *sql.DB) (*MySQLMailStore, error) {
	if db == nil {
		return nil, ErrInvalidInput
	}
	return &MySQLMailStore{db: db}, nil
}

func (s *MySQLMailStore) ClaimNext(ctx context.Context, rawClaimToken string, now time.Time) (ClaimedMail, error) {
	if s == nil || s.db == nil || strings.TrimSpace(rawClaimToken) == "" || now.IsZero() {
		return ClaimedMail{}, ErrInvalidInput
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ClaimedMail{}, err
	}
	defer tx.Rollback()

	var (
		claimed                       ClaimedMail
		status                        string
		nextAttempt, claimExpires     sql.NullTime
		lastError                     sql.NullString
		idempotencyHash, claimHashRaw []byte
	)
	err = tx.QueryRowContext(ctx, `
SELECT id,template_key,template_locale,template_version,recipient_kind,recipient_value,
       resource_type,resource_id,status,attempt_count,next_attempt_at,idempotency_key_hash,
       claim_token_hash,claim_expires_at,last_error_code,created_at,updated_at
FROM mail_jobs
WHERE status='queued' OR (status='retrying' AND next_attempt_at<=?)
ORDER BY created_at,id
LIMIT 1
FOR UPDATE SKIP LOCKED`, now).
		Scan(&claimed.Job.ID, &claimed.Job.TemplateKey, &claimed.Locale, &claimed.Job.TemplateVersion,
			&claimed.Job.RecipientKind, &claimed.Job.RecipientValue, &claimed.Job.ResourceType,
			&claimed.Job.ResourceID, &status, &claimed.Job.AttemptCount, &nextAttempt,
			&idempotencyHash, &claimHashRaw, &claimExpires, &lastError,
			&claimed.Job.CreatedAt, &claimed.Job.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ClaimedMail{}, ErrNoMailAvailable
	}
	if err != nil {
		return ClaimedMail{}, err
	}
	claimed.Job.Status = MailStatus(status)
	claimed.Locale = strings.TrimSpace(claimed.Locale)
	if claimed.Locale == "" || len(idempotencyHash) != 32 {
		return ClaimedMail{}, ErrMailState
	}
	copy(claimed.Job.IdempotencyKeyHash[:], idempotencyHash)
	if nextAttempt.Valid {
		v := nextAttempt.Time.UTC()
		claimed.Job.NextAttemptAt = &v
	}
	if len(claimHashRaw) != 0 {
		if len(claimHashRaw) != 32 {
			return ClaimedMail{}, ErrMailState
		}
		copy(claimed.Job.ClaimTokenHash[:], claimHashRaw)
	}
	if claimExpires.Valid {
		v := claimExpires.Time.UTC()
		claimed.Job.ClaimExpiresAt = &v
	}
	if lastError.Valid {
		claimed.Job.LastErrorCode = lastError.String
	}

	next, err := ClaimMailJob(claimed.Job, rawClaimToken, now)
	if err != nil {
		return ClaimedMail{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE mail_jobs
SET status=?,attempt_count=?,next_attempt_at=NULL,claim_token_hash=?,claim_expires_at=?,updated_at=?
WHERE id=? AND status=? AND attempt_count=?`,
		string(next.Status), next.AttemptCount, next.ClaimTokenHash[:], next.ClaimExpiresAt, next.UpdatedAt,
		claimed.Job.ID, string(claimed.Job.Status), claimed.Job.AttemptCount)
	if err != nil {
		return ClaimedMail{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		if err != nil {
			return ClaimedMail{}, err
		}
		return ClaimedMail{}, ErrMailClaim
	}
	correlationID := fmt.Sprintf("mail:%s:%d", next.ID, next.AttemptCount)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO mail_attempts
(mail_job_id,attempt_number,status,started_at,correlation_id)
VALUES (?,?, 'sending', ?, ?)`, next.ID, next.AttemptCount, now, correlationID); err != nil {
		return ClaimedMail{}, err
	}
	if err := tx.Commit(); err != nil {
		return ClaimedMail{}, err
	}
	claimed.Job = next
	return claimed, nil
}

func (s *MySQLMailStore) LoadDelivery(ctx context.Context, claimed ClaimedMail) (MailDeliveryPayload, error) {
	if s == nil || s.db == nil || strings.TrimSpace(claimed.Locale) == "" || claimed.Job.Status != MailSending {
		return MailDeliveryPayload{}, ErrMailState
	}
	var (
		template      MailTemplate
		allowlistJSON []byte
		internalOnly  bool
	)
	err := s.db.QueryRowContext(ctx, `
SELECT template_key,version,subject_template,text_template,html_template,variable_allowlist_json,internal_only
FROM mail_templates
WHERE template_key=? AND locale=? AND version=? AND enabled=1`,
		claimed.Job.TemplateKey, claimed.Locale, claimed.Job.TemplateVersion).
		Scan(&template.Key, &template.Version, &template.SubjectTemplate, &template.TextTemplate,
			&template.HTMLTemplate, &allowlistJSON, &internalOnly)
	if errors.Is(err, sql.ErrNoRows) {
		return MailDeliveryPayload{}, ErrTemplateVariable
	}
	if err != nil {
		return MailDeliveryPayload{}, err
	}
	template.InternalOnlyTemplate = internalOnly
	if err := json.Unmarshal(allowlistJSON, &template.VariableAllowlist); err != nil {
		return MailDeliveryPayload{}, ErrTemplateVariable
	}
	values, err := s.resolveMailValues(ctx, claimed.Job)
	if err != nil {
		return MailDeliveryPayload{}, err
	}
	return MailDeliveryPayload{Template: template, Values: values, Recipient: claimed.Job.RecipientValue}, nil
}

func (s *MySQLMailStore) resolveMailValues(ctx context.Context, job MailJob) (map[string]string, error) {
	switch job.ResourceType {
	case "ticket":
		var id, subject, status, displayName string
		err := s.db.QueryRowContext(ctx, `
SELECT t.id,t.subject,t.status,
       COALESCE(NULLIF(wm.display_name,''),NULLIF(pc.name,''),'Customer')
FROM support_tickets t
LEFT JOIN workspace_memberships wm ON wm.workspace_id=t.workspace_id AND wm.user_id=t.requester_user_id
LEFT JOIN support_public_contacts pc ON pc.id=t.public_contact_id
WHERE t.id=?`, job.ResourceID).Scan(&id, &subject, &status, &displayName)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidInput
		}
		if err != nil {
			return nil, err
		}
		return map[string]string{"ticket_id": id, "subject": subject, "status": status, "display_name": displayName}, nil
	case "ticket_message":
		var ticketID, subject, status, displayName, body string
		err := s.db.QueryRowContext(ctx, `
SELECT t.id,t.subject,t.status,
       COALESCE(NULLIF(wm.display_name,''),NULLIF(pc.name,''),'Customer'),m.body
FROM support_ticket_messages m
JOIN support_tickets t ON t.id=m.ticket_id
LEFT JOIN workspace_memberships wm ON wm.workspace_id=t.workspace_id AND wm.user_id=t.requester_user_id
LEFT JOIN support_public_contacts pc ON pc.id=t.public_contact_id
WHERE m.id=?`, job.ResourceID).Scan(&ticketID, &subject, &status, &displayName, &body)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidInput
		}
		if err != nil {
			return nil, err
		}
		return map[string]string{"ticket_id": ticketID, "subject": subject, "status": status, "display_name": displayName, "message_body": body}, nil
	case "public_contact":
		var name, subject, message string
		err := s.db.QueryRowContext(ctx, `
SELECT name,subject,message FROM support_public_contacts WHERE id=?`, job.ResourceID).
			Scan(&name, &subject, &message)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidInput
		}
		if err != nil {
			return nil, err
		}
		return map[string]string{"contact_name": name, "contact_subject": subject, "contact_message": message}, nil
	case "mail_test":
		return map[string]string{}, nil
	default:
		return nil, ErrInvalidInput
	}
}

func (s *MySQLMailStore) Complete(ctx context.Context, claimed ClaimedMail, rawClaimToken string, delivery MailDeliveryResult, now time.Time) (MailJob, error) {
	if s == nil || s.db == nil || claimed.Job.Status != MailSending || now.IsZero() {
		return MailJob{}, ErrMailState
	}
	next, err := CompleteMailJob(claimed.Job, rawClaimToken, delivery, now.UTC())
	if err != nil {
		return MailJob{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MailJob{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
UPDATE mail_jobs
SET status=?,next_attempt_at=?,claim_token_hash=NULL,claim_expires_at=NULL,last_error_code=?,updated_at=?
WHERE id=? AND status='sending' AND claim_token_hash=? AND attempt_count=?`,
		string(next.Status), next.NextAttemptAt, nullableString(next.LastErrorCode), next.UpdatedAt,
		next.ID, claimed.Job.ClaimTokenHash[:], claimed.Job.AttemptCount)
	if err != nil {
		return MailJob{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		if err != nil {
			return MailJob{}, err
		}
		return MailJob{}, ErrMailClaim
	}
	attemptStatus := "terminal_failure"
	if delivery.Success {
		attemptStatus = "sent"
	} else if delivery.Transient && next.Status == MailRetrying {
		attemptStatus = "transient_failure"
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE mail_attempts
SET status=?,error_code=?,completed_at=?
WHERE mail_job_id=? AND attempt_number=? AND status='sending'`,
		attemptStatus, nullableString(next.LastErrorCode), next.UpdatedAt, next.ID, next.AttemptCount); err != nil {
		return MailJob{}, err
	}
	workspaceID, err := mailAuditWorkspaceTx(ctx, tx, next)
	if err != nil {
		return MailJob{}, err
	}
	metadata := map[string]string{
		"template_key":   next.TemplateKey,
		"attempt_number": strconv.FormatUint(uint64(next.AttemptCount), 10),
		"status":         string(next.Status),
	}
	if next.LastErrorCode != "" {
		metadata["error_code"] = next.LastErrorCode
	}
	if err := recordSupportAuditExecer(ctx, tx, SupportAuditInput{
		WorkspaceID: workspaceID, ActorID: "mailworker", Action: "mail_attempt_" + attemptStatus,
		ResourceType: "mail_job", ResourceID: next.ID,
		CorrelationID: fmt.Sprintf("mail:%s:%d", next.ID, next.AttemptCount), Result: AuditSuccess, Metadata: metadata,
	}); err != nil {
		return MailJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return MailJob{}, err
	}
	return next, nil
}

func mailAuditWorkspaceTx(ctx context.Context, tx *sql.Tx, job MailJob) (string, error) {
	var workspaceID sql.NullString
	switch job.ResourceType {
	case "ticket":
		err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM support_tickets WHERE id=?`, job.ResourceID).Scan(&workspaceID)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrInvalidInput
		}
		if err != nil {
			return "", err
		}
	case "ticket_message":
		err := tx.QueryRowContext(ctx, `
SELECT t.workspace_id FROM support_ticket_messages m JOIN support_tickets t ON t.id=m.ticket_id WHERE m.id=?`, job.ResourceID).Scan(&workspaceID)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrInvalidInput
		}
		if err != nil {
			return "", err
		}
	case "public_contact", "mail_test":
		return "", nil
	default:
		return "", ErrInvalidInput
	}
	if workspaceID.Valid {
		return workspaceID.String, nil
	}
	return "", nil
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
