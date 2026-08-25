package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/securetoken"
	"github.com/Techshrr/GoJet/internal/support"
)

// AuthMailQueue is a narrow P15 adapter over the signed P14 MySQL mail queue.
// Claim/retry/idempotency/dispatch remain P14-owned. P15 only resolves active
// auth grant variables from a server-held derivation key at delivery time.
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
	if q == nil || q.base == nil {
		return support.MailJob{}, support.ErrInvalidInput
	}
	return q.base.Complete(ctx, claimed, rawClaimToken, delivery, now)
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
