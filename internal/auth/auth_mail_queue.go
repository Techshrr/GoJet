package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/securetoken"
	"github.com/Techshrr/GoJet/internal/support"
)

// AuthMailQueue is a narrow P15 adapter over the signed P14 MySQL mail queue.
// Claim/retry/idempotency/dispatch remain P14-owned. P15 only resolves active
// auth grant variables from a server-held derivation key at delivery time and
// records auth-owned completion audit without routing auth resources through
// P14 support-ticket workspace audit semantics.
type AuthMailQueue struct {
	db       *sql.DB
	base     *support.MySQLMailStore
	grantKey securetoken.Key
}

func NewAuthMailQueue(db *sql.DB, grantKey securetoken.Key) (*AuthMailQueue, error) {
	if db == nil || strings.TrimSpace(grantKey.ID()) == "" {
		return nil, ErrInvalid
	}
	base, err := support.NewMySQLMailStore(db)
	if err != nil {
		return nil, err
	}
	return &AuthMailQueue{db: db, base: base, grantKey: grantKey}, nil
}

func (q *AuthMailQueue) ClaimNext(ctx context.Context, rawClaimToken string, now time.Time) (support.ClaimedMail, error) {
	if q == nil || q.base == nil {
		return support.ClaimedMail{}, support.ErrInvalidInput
	}
	return q.base.ClaimNext(ctx, rawClaimToken, now)
}

func (q *AuthMailQueue) Complete(ctx context.Context, claimed support.ClaimedMail, rawClaimToken string, delivery support.MailDeliveryResult, now time.Time) (support.MailJob, error) {
	if q == nil || q.db == nil || q.base == nil {
		return support.MailJob{}, support.ErrInvalidInput
	}
	if claimed.Job.ResourceType != "auth_one_time_grant" {
		return q.base.Complete(ctx, claimed, rawClaimToken, delivery, now)
	}

	next, err := support.CompleteMailJob(claimed.Job, rawClaimToken, delivery, now.UTC())
	if err != nil {
		return support.MailJob{}, err
	}
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return support.MailJob{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
UPDATE mail_jobs
SET status=?,next_attempt_at=?,claim_token_hash=NULL,claim_expires_at=NULL,last_error_code=?,updated_at=?
WHERE id=? AND status='sending' AND claim_token_hash=? AND attempt_count=?`,
		string(next.Status), next.NextAttemptAt, nullableAuthMailString(next.LastErrorCode), next.UpdatedAt,
		next.ID, claimed.Job.ClaimTokenHash[:], claimed.Job.AttemptCount)
	if err != nil {
		return support.MailJob{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return support.MailJob{}, err
	}
	if rows != 1 {
		return support.MailJob{}, support.ErrMailClaim
	}

	attemptStatus := "terminal_failure"
	auditResult := "failed"
	if delivery.Success {
		attemptStatus = "sent"
		auditResult = "success"
	} else if delivery.Transient && next.Status == support.MailRetrying {
		attemptStatus = "transient_failure"
	}
	attemptResult, err := tx.ExecContext(ctx, `
UPDATE mail_attempts
SET status=?,error_code=?,completed_at=?
WHERE mail_job_id=? AND attempt_number=? AND status='sending'`,
		attemptStatus, nullableAuthMailString(next.LastErrorCode), next.UpdatedAt, next.ID, next.AttemptCount)
	if err != nil {
		return support.MailJob{}, err
	}
	attemptRows, err := attemptResult.RowsAffected()
	if err != nil {
		return support.MailJob{}, err
	}
	if attemptRows != 1 {
		return support.MailJob{}, support.ErrMailState
	}

	var userID sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM auth_one_time_grants WHERE id=?`, next.ResourceID).Scan(&userID); errors.Is(err, sql.ErrNoRows) {
		return support.MailJob{}, support.ErrInvalidInput
	} else if err != nil {
		return support.MailJob{}, err
	}
	var userValue any
	if userID.Valid && strings.TrimSpace(userID.String) != "" {
		userValue = userID.String
	}
	metadata := map[string]any{
		"template_key":   next.TemplateKey,
		"attempt_number": next.AttemptCount,
		"status":         string(next.Status),
	}
	if next.LastErrorCode != "" {
		metadata["error_code"] = next.LastErrorCode
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return support.MailJob{}, err
	}
	correlationID := fmt.Sprintf("mail:%s:%d", next.ID, next.AttemptCount)
	if !validCorrelationID(correlationID) {
		return support.MailJob{}, support.ErrInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('system','',?,?, 'mail_job',?,?,?, ?,?)`,
		userValue, "auth.mail.attempt."+attemptStatus, next.ID, auditResult, correlationID, metadataJSON, next.UpdatedAt); err != nil {
		return support.MailJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return support.MailJob{}, err
	}
	return next, nil
}

func (q *AuthMailQueue) MailDispatchEnabled(ctx context.Context) (bool, error) {
	if q == nil || q.base == nil {
		return false, support.ErrInvalidInput
	}
	return q.base.MailDispatchEnabled(ctx)
}

func (q *AuthMailQueue) LoadDelivery(ctx context.Context, claimed support.ClaimedMail) (support.MailDeliveryPayload, error) {
	if q == nil || q.db == nil || q.base == nil {
		return support.MailDeliveryPayload{}, support.ErrInvalidInput
	}
	if claimed.Job.ResourceType != "auth_one_time_grant" {
		return q.base.LoadDelivery(ctx, claimed)
	}
	if claimed.Job.Status != support.MailSending || strings.TrimSpace(claimed.Locale) == "" || strings.TrimSpace(claimed.Job.ResourceID) == "" {
		return support.MailDeliveryPayload{}, support.ErrMailState
	}

	var (
		template      support.MailTemplate
		allowlistJSON []byte
		internalOnly  bool
	)
	err := q.db.QueryRowContext(ctx, `
SELECT template_key,version,subject_template,text_template,html_template,variable_allowlist_json,internal_only
FROM mail_templates
WHERE template_key=? AND locale=? AND version=? AND enabled=1`,
		claimed.Job.TemplateKey, claimed.Locale, claimed.Job.TemplateVersion).
		Scan(&template.Key, &template.Version, &template.SubjectTemplate, &template.TextTemplate,
			&template.HTMLTemplate, &allowlistJSON, &internalOnly)
	if errors.Is(err, sql.ErrNoRows) {
		return support.MailDeliveryPayload{}, support.ErrTemplateVariable
	}
	if err != nil {
		return support.MailDeliveryPayload{}, err
	}
	template.InternalOnlyTemplate = internalOnly
	if err := json.Unmarshal(allowlistJSON, &template.VariableAllowlist); err != nil {
		return support.MailDeliveryPayload{}, support.ErrTemplateVariable
	}

	var (
		purpose, grantEmail string
		tokenHash           []byte
		tokenKeyID          sql.NullString
		expiresAt           time.Time
		consumedAt          sql.NullTime
		invalidatedAt       sql.NullTime
	)
	err = q.db.QueryRowContext(ctx, `
SELECT purpose,COALESCE(email_normalized,''),token_hash,token_key_id,expires_at,consumed_at,invalidated_at
FROM auth_one_time_grants WHERE id=?`, claimed.Job.ResourceID).
		Scan(&purpose, &grantEmail, &tokenHash, &tokenKeyID, &expiresAt, &consumedAt, &invalidatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return support.MailDeliveryPayload{}, support.ErrInvalidInput
	}
	if err != nil {
		return support.MailDeliveryPayload{}, err
	}
	if len(tokenHash) != 32 || !tokenKeyID.Valid || tokenKeyID.String != q.grantKey.ID() || consumedAt.Valid || invalidatedAt.Valid || !expiresAt.After(time.Now().UTC()) {
		return support.MailDeliveryPayload{}, support.ErrMailState
	}
	recipientNormalized, err := NormalizeEmail(claimed.Job.RecipientValue)
	if err != nil || grantEmail == "" || recipientNormalized != grantEmail {
		return support.MailDeliveryPayload{}, support.ErrInvalidInput
	}

	var prefix, variable string
	switch purpose {
	case "email_verification":
		prefix, variable = "gvc_", "verification_code"
	case "login_email_code":
		prefix, variable = "glc_", "login_code"
	case "password_reset":
		prefix, variable = "grp_", "reset_token"
	case "social_email_verification":
		prefix, variable = "gsv_", "verification_code"
	default:
		return support.MailDeliveryPayload{}, support.ErrInvalidInput
	}
	code, err := q.grantKey.Derive(prefix, purpose, claimed.Job.ResourceID)
	if err != nil {
		return support.MailDeliveryPayload{}, err
	}
	expected := securetoken.Hash(code)
	var stored [32]byte
	copy(stored[:], tokenHash)
	if stored != expected {
		return support.MailDeliveryPayload{}, support.ErrMailState
	}
	return support.MailDeliveryPayload{
		Template:  template,
		Values:    map[string]string{variable: code},
		Recipient: claimed.Job.RecipientValue,
	}, nil
}

func nullableAuthMailString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
