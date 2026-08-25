package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/securetoken"
	"github.com/Techshrr/GoJet/internal/support"
)

const (
	PasswordResetTTL         = 30 * time.Minute
	passwordResetIssueWindow = 60 * time.Second
)

type PasswordRecoveryService struct {
	db       *sql.DB
	grantKey securetoken.Key
}

func NewPasswordRecoveryService(db *sql.DB, grantKey securetoken.Key) (*PasswordRecoveryService, error) {
	if db == nil || strings.TrimSpace(grantKey.ID()) == "" {
		return nil, ErrInvalid
	}
	return &PasswordRecoveryService{db: db, grantKey: grantKey}, nil
}

// RequestReset deliberately returns the same nil result for existing, missing,
// ineligible and recently-requested accounts after syntactic validation. The
// caller therefore has no user-enumeration authority.
func (s *PasswordRecoveryService) RequestReset(ctx context.Context, email, correlationID string) error {
	if s == nil || s.db == nil || !validCorrelationID(strings.TrimSpace(correlationID)) {
		return ErrInvalid
	}
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return ErrInvalid
	}
	correlationID = strings.TrimSpace(correlationID)
	now := time.Now().UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var userID, storedEmail, status string
	var verifiedAt sql.NullTime
	var credentialCount int
	err = tx.QueryRowContext(ctx, `
SELECT u.id,u.email,u.status,u.email_verified_at,
       (SELECT COUNT(*) FROM auth_credentials c WHERE c.user_id=u.id)
FROM auth_users u
WHERE u.email_normalized=?
FOR UPDATE`, normalized).Scan(&userID, &storedEmail, &status, &verifiedAt, &credentialCount)
	if errors.Is(err, sql.ErrNoRows) {
		if err := recordResetRequestAuditTx(ctx, tx, nil, "", correlationID, now); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	if status != UserStatusActive || !verifiedAt.Valid || credentialCount != 1 {
		if err := recordResetRequestAuditTx(ctx, tx, userID, "", correlationID, now); err != nil {
			return err
		}
		return tx.Commit()
	}

	var recent int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM auth_one_time_grants
WHERE user_id=? AND purpose='password_reset'
  AND consumed_at IS NULL AND invalidated_at IS NULL AND created_at>=?`,
		userID, now.Add(-passwordResetIssueWindow)).Scan(&recent); err != nil {
		return err
	}
	if recent > 0 {
		if err := recordResetRequestAuditTx(ctx, tx, userID, "", correlationID, now); err != nil {
			return err
		}
		return tx.Commit()
	}

	grantID, err := newOpaqueID("grt_", 18)
	if err != nil {
		return err
	}
	token, err := s.grantKey.Derive("grp_", "password_reset", grantID)
	if err != nil {
		return err
	}
	hash := securetoken.Hash(token)
	expiresAt := now.Add(PasswordResetTTL)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_one_time_grants
(id,purpose,user_id,email_normalized,token_hash,token_key_id,attempt_count,max_attempts,expires_at,consumed_at,invalidated_at,correlation_id,created_at)
VALUES (?,'password_reset',?,?,?,?,0,8,?,NULL,NULL,?,?)`,
		grantID, userID, normalized, hash[:], s.grantKey.ID(), expiresAt, correlationID, now); err != nil {
		return err
	}
	if err := support.EnqueueMailTx(ctx, tx, support.MailEnqueueInput{
		TemplateKey:    "auth-password-reset",
		Locale:         "en",
		RecipientKind:  "auth_user",
		RecipientValue: storedEmail,
		ResourceType:   "auth_one_time_grant",
		ResourceID:     grantID,
	}, now); err != nil {
		return err
	}
	if err := recordResetRequestAuditTx(ctx, tx, userID, grantID, correlationID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PasswordRecoveryService) ResetPassword(ctx context.Context, rawToken, newPassword, correlationID string) error {
	if s == nil || s.db == nil || strings.TrimSpace(rawToken) == "" || !validPassword(newPassword) || !validCorrelationID(strings.TrimSpace(correlationID)) {
		return ErrInvalid
	}
	encoded, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	correlationID = strings.TrimSpace(correlationID)
	now := time.Now().UTC().Truncate(time.Microsecond)
	hash := securetoken.Hash(strings.TrimSpace(rawToken))
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var grantID, userID string
	var expiresAt time.Time
	var consumedAt, invalidatedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
SELECT id,user_id,expires_at,consumed_at,invalidated_at
FROM auth_one_time_grants
WHERE token_hash=? AND purpose='password_reset'
FOR UPDATE`, hash[:]).Scan(&grantID, &userID, &expiresAt, &consumedAt, &invalidatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return err
	}
	if consumedAt.Valid {
		return ErrReplay
	}
	if invalidatedAt.Valid {
		return ErrRevoked
	}
	if !expiresAt.After(now) {
		if _, err := tx.ExecContext(ctx, `UPDATE auth_one_time_grants SET invalidated_at=? WHERE id=? AND invalidated_at IS NULL`, now, grantID); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return ErrExpired
	}

	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM auth_users WHERE id=? FOR UPDATE`, userID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUnauthorized
		}
		return err
	}
	if status == UserStatusDisabled {
		return ErrForbidden
	}

	result, err := tx.ExecContext(ctx, `
UPDATE auth_credentials
SET password_hash=?,password_algorithm=?,password_version=?,failed_attempts=0,locked_until=NULL,updated_at=?
WHERE user_id=?`, encoded, passwordAlgorithm, passwordAlgorithmVersion, now, userID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE auth_users SET password_changed_at=?,version=version+1,updated_at=? WHERE id=?`, now, now, userID); err != nil {
		return err
	}
	revokeResult, err := tx.ExecContext(ctx, `
UPDATE auth_sessions
SET status='revoked',revoked_at=?,updated_at=?
WHERE user_id=? AND status='active'`, now, now, userID)
	if err != nil {
		return err
	}
	revokedSessions, err := revokeResult.RowsAffected()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE auth_one_time_grants SET consumed_at=? WHERE id=?`, now, grantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('public','',?,'auth.password.reset','auth_one_time_grant',?,'success',?,JSON_OBJECT('sessions_revoked',?,'algorithm',?),?)`,
		userID, grantID, correlationID, revokedSessions, passwordAlgorithm, now); err != nil {
		return err
	}
	return tx.Commit()
}

func recordResetRequestAuditTx(ctx context.Context, tx *sql.Tx, userID any, resourceID, correlationID string, now time.Time) error {
	if tx == nil || !validCorrelationID(correlationID) {
		return ErrInvalid
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('public','',?,'auth.password_reset.requested','auth_one_time_grant',?,'success',?,JSON_OBJECT('response','neutral'),?)`,
		userID, resourceID, correlationID, now)
	return err
}
