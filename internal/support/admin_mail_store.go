package support

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"strings"
	"time"
)

type AdminMailQueueItem struct {
	ID              string     `json:"id"`
	TemplateKey     string     `json:"template_key"`
	TemplateVersion uint64     `json:"template_version"`
	RecipientKind   string     `json:"recipient_kind"`
	ResourceType    string     `json:"resource_type"`
	ResourceID      string     `json:"resource_id"`
	Status          MailStatus `json:"status"`
	AttemptCount    uint32     `json:"attempt_count"`
	NextAttemptAt   *time.Time `json:"next_attempt_at,omitempty"`
	LastErrorCode   string     `json:"last_error_code,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type AdminMailTemplateView struct {
	Key               string    `json:"key"`
	Locale            string    `json:"locale"`
	Version           uint64    `json:"version"`
	SubjectTemplate   string    `json:"subject_template"`
	TextTemplate      string    `json:"text_template"`
	HTMLTemplate      string    `json:"html_template"`
	VariableAllowlist []string  `json:"variable_allowlist"`
	Enabled           bool      `json:"enabled"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type AdminMailSettings struct {
	Enabled   bool      `json:"enabled"`
	Version   uint64    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AdminMailTestInput struct {
	ActorID            string
	Recipient          string
	CorrelationID      string
	IdempotencyKeyHash [32]byte
}

func (s *Store) ListAdminMailQueue(ctx context.Context, limit int) ([]AdminMailQueueItem, error) {
	if s == nil || s.db == nil {
		return nil, ErrInvalidInput
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id,template_key,template_version,recipient_kind,resource_type,resource_id,status,
       attempt_count,next_attempt_at,last_error_code,created_at,updated_at
FROM mail_jobs ORDER BY created_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AdminMailQueueItem, 0)
	for rows.Next() {
		item, err := scanAdminMailQueueItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListAdminMailTemplates(ctx context.Context) ([]AdminMailTemplateView, error) {
	if s == nil || s.db == nil {
		return nil, ErrInvalidInput
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT template_key,locale,version,subject_template,text_template,html_template,
       variable_allowlist_json,enabled,updated_at
FROM mail_templates ORDER BY template_key,locale,version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AdminMailTemplateView, 0)
	for rows.Next() {
		var item AdminMailTemplateView
		var allowlist []byte
		var enabled bool
		if err := rows.Scan(&item.Key, &item.Locale, &item.Version, &item.SubjectTemplate, &item.TextTemplate,
			&item.HTMLTemplate, &allowlist, &enabled, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(allowlist, &item.VariableAllowlist); err != nil {
			return nil, ErrTemplateVariable
		}
		item.Enabled = enabled
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetAdminMailSettings(ctx context.Context) (AdminMailSettings, error) {
	if s == nil || s.db == nil {
		return AdminMailSettings{}, ErrInvalidInput
	}
	var settings AdminMailSettings
	err := s.db.QueryRowContext(ctx, `
SELECT enabled,version,updated_at FROM mail_settings WHERE settings_key='primary'`).
		Scan(&settings.Enabled, &settings.Version, &settings.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminMailSettings{}, ErrSupportNotFound
	}
	if err != nil {
		return AdminMailSettings{}, err
	}
	if settings.Version == 0 || settings.UpdatedAt.IsZero() {
		return AdminMailSettings{}, ErrMailState
	}
	return settings, nil
}

func (s *Store) UpdateAdminMailSettings(ctx context.Context, expectedVersion uint64, enabled bool) (AdminMailSettings, error) {
	if s == nil || s.db == nil || expectedVersion == 0 {
		return AdminMailSettings{}, ErrInvalidInput
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE mail_settings SET enabled=?,version=version+1
WHERE settings_key='primary' AND version=?`, enabled, expectedVersion)
	if err != nil {
		return AdminMailSettings{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return AdminMailSettings{}, err
	}
	if rows != 1 {
		return AdminMailSettings{}, ErrSupportConflict
	}
	return s.GetAdminMailSettings(ctx)
}

func (s *Store) EnqueueAdminTestMail(ctx context.Context, input AdminMailTestInput) (AdminMailQueueItem, bool, error) {
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.Recipient = strings.TrimSpace(input.Recipient)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	parsed, parseErr := mail.ParseAddress(input.Recipient)
	if s == nil || s.db == nil || input.ActorID == "" || input.Recipient == "" || input.CorrelationID == "" ||
		input.IdempotencyKeyHash == ([32]byte{}) || parseErr != nil || parsed.Address != input.Recipient || len(input.Recipient) > 320 {
		return AdminMailQueueItem{}, false, ErrInvalidInput
	}
	resourceID := "test_" + hex.EncodeToString(input.IdempotencyKeyHash[:16])
	logicalHash, err := MailLogicalIdempotencyHash("mail-test", 1, "admin_test", "mail_test", resourceID)
	if err != nil {
		return AdminMailQueueItem{}, false, err
	}
	jobID, err := newOpaqueID("mail")
	if err != nil {
		return AdminMailQueueItem{}, false, err
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
INSERT INTO mail_jobs
(id,template_key,template_locale,template_version,recipient_kind,recipient_value,resource_type,resource_id,
 status,attempt_count,next_attempt_at,idempotency_key_hash,claim_token_hash,claim_expires_at,last_error_code,created_at,updated_at)
VALUES (?,'mail-test','en',1,'admin_test',?,'mail_test',?,'queued',0,NULL,?,NULL,NULL,NULL,?,?)
ON DUPLICATE KEY UPDATE id=id`, jobID, input.Recipient, resourceID, logicalHash[:], now, now)
	if err != nil {
		return AdminMailQueueItem{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return AdminMailQueueItem{}, false, err
	}
	created := rows == 1
	var recipient string
	item, err := queryAdminMailQueueItem(s.db.QueryRowContext(ctx, `
SELECT id,template_key,template_version,recipient_kind,resource_type,resource_id,status,
       attempt_count,next_attempt_at,last_error_code,created_at,updated_at,recipient_value
FROM mail_jobs WHERE idempotency_key_hash=?`, logicalHash[:]), &recipient)
	if err != nil {
		return AdminMailQueueItem{}, false, err
	}
	if recipient != input.Recipient {
		return AdminMailQueueItem{}, false, ErrSupportConflict
	}
	return item, created, nil
}

// MailDispatchEnabled is the runtime gate consumed by MailWorker before it
// claims a job. No SMTP credential is consulted or persisted here.
func (s *MySQLMailStore) MailDispatchEnabled(ctx context.Context) (bool, error) {
	if s == nil || s.db == nil {
		return false, ErrInvalidInput
	}
	var enabled bool
	var version uint64
	err := s.db.QueryRowContext(ctx, `
SELECT enabled,version FROM mail_settings WHERE settings_key='primary'`).Scan(&enabled, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrMailState
	}
	if err != nil {
		return false, err
	}
	if version == 0 {
		return false, ErrMailState
	}
	return enabled, nil
}

func scanAdminMailQueueItem(row rowScanner) (AdminMailQueueItem, error) {
	return queryAdminMailQueueItem(row, nil)
}

func queryAdminMailQueueItem(row rowScanner, recipient *string) (AdminMailQueueItem, error) {
	var item AdminMailQueueItem
	var status string
	var nextAttempt sql.NullTime
	var lastError sql.NullString
	args := []any{&item.ID, &item.TemplateKey, &item.TemplateVersion, &item.RecipientKind, &item.ResourceType,
		&item.ResourceID, &status, &item.AttemptCount, &nextAttempt, &lastError, &item.CreatedAt, &item.UpdatedAt}
	if recipient != nil {
		args = append(args, recipient)
	}
	if err := row.Scan(args...); err != nil {
		return AdminMailQueueItem{}, err
	}
	item.Status = MailStatus(status)
	switch item.Status {
	case MailQueued, MailSending, MailSent, MailRetrying, MailFailed:
	default:
		return AdminMailQueueItem{}, ErrMailState
	}
	if nextAttempt.Valid {
		v := nextAttempt.Time.UTC()
		item.NextAttemptAt = &v
	}
	if lastError.Valid {
		item.LastErrorCode = lastError.String
	}
	return item, nil
}
