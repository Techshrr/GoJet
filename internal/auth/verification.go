package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/securetoken"
)

type EmailVerificationInput struct {
	Code          string
	CorrelationID string
}

type EmailVerificationResult struct {
	User       User
	GrantID    string
	VerifiedAt time.Time
}

type VerificationService struct {
	db *sql.DB
}

func NewVerificationService(db *sql.DB) (*VerificationService, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &VerificationService{db: db}, nil
}

func (s *VerificationService) VerifyEmail(ctx context.Context, input EmailVerificationInput) (EmailVerificationResult, error) {
	if s == nil || s.db == nil {
		return EmailVerificationResult{}, ErrInvalid
	}
	code := strings.TrimSpace(input.Code)
	correlationID := strings.TrimSpace(input.CorrelationID)
	if !validVerificationCode(code) || !validCorrelationID(correlationID) {
		return EmailVerificationResult{}, ErrInvalid
	}

	tokenHash := securetoken.Hash(code)
	now := time.Now().UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return EmailVerificationResult{}, err
	}
	defer tx.Rollback()

	var (
		grantID        string
		grantUserID    sql.NullString
		grantEmail     sql.NullString
		attemptCount   uint32
		maxAttempts    uint32
		expiresAt      time.Time
		consumedAt     sql.NullTime
		invalidatedAt  sql.NullTime
		grantCreatedAt time.Time
	)
	err = tx.QueryRowContext(ctx, `
SELECT id,user_id,email_normalized,attempt_count,max_attempts,expires_at,consumed_at,invalidated_at,created_at
FROM auth_one_time_grants
WHERE token_hash=? AND purpose='email_verification'
LIMIT 1
FOR UPDATE`, tokenHash[:]).Scan(
		&grantID,
		&grantUserID,
		&grantEmail,
		&attemptCount,
		&maxAttempts,
		&expiresAt,
		&consumedAt,
		&invalidatedAt,
		&grantCreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return EmailVerificationResult{}, ErrInvalid
	}
	if err != nil {
		return EmailVerificationResult{}, err
	}
	if !grantUserID.Valid || strings.TrimSpace(grantUserID.String) == "" || !grantEmail.Valid || strings.TrimSpace(grantEmail.String) == "" || grantID == "" || maxAttempts == 0 || expiresAt.IsZero() || grantCreatedAt.IsZero() {
		return EmailVerificationResult{}, ErrConflict
	}
	if consumedAt.Valid {
		return EmailVerificationResult{}, ErrReplay
	}
	if invalidatedAt.Valid {
		return EmailVerificationResult{}, ErrRevoked
	}
	if attemptCount >= maxAttempts {
		if err := invalidateVerificationGrant(ctx, tx, grantID, grantUserID.String, correlationID, now, "attempt_limit"); err != nil {
			return EmailVerificationResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return EmailVerificationResult{}, err
		}
		return EmailVerificationResult{}, ErrRevoked
	}
	if !expiresAt.After(now) {
		if err := invalidateVerificationGrant(ctx, tx, grantID, grantUserID.String, correlationID, now, "expired"); err != nil {
			return EmailVerificationResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return EmailVerificationResult{}, err
		}
		return EmailVerificationResult{}, ErrExpired
	}

	user, err := scanUser(tx.QueryRowContext(ctx, `
SELECT id,email,email_normalized,display_name,status,email_verified_at,password_changed_at,version,created_at,updated_at
FROM auth_users WHERE id=? FOR UPDATE`, grantUserID.String))
	if err != nil {
		return EmailVerificationResult{}, err
	}
	if user.EmailNormalized != grantEmail.String || user.Status != UserStatusPendingVerification || user.EmailVerifiedAt != nil {
		return EmailVerificationResult{}, ErrConflict
	}

	result, err := tx.ExecContext(ctx, `
UPDATE auth_one_time_grants
SET consumed_at=?,attempt_count=attempt_count+1
WHERE id=? AND consumed_at IS NULL AND invalidated_at IS NULL AND attempt_count=?`,
		now, grantID, attemptCount)
	if err != nil {
		return EmailVerificationResult{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return EmailVerificationResult{}, err
	}
	if rows != 1 {
		return EmailVerificationResult{}, ErrConflict
	}

	result, err = tx.ExecContext(ctx, `
UPDATE auth_users
SET status='active',email_verified_at=?,version=version+1,updated_at=?
WHERE id=? AND status='pending_verification' AND email_verified_at IS NULL AND version=?`,
		now, now, user.ID, user.Version)
	if err != nil {
		return EmailVerificationResult{}, err
	}
	rows, err = result.RowsAffected()
	if err != nil {
		return EmailVerificationResult{}, err
	}
	if rows != 1 {
		return EmailVerificationResult{}, ErrConflict
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('public','',?,'auth.email_verification.completed','auth_one_time_grant',?,'success',?,JSON_OBJECT('purpose','email_verification'),?)`,
		user.ID, grantID, correlationID, now); err != nil {
		return EmailVerificationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return EmailVerificationResult{}, err
	}

	user.Status = UserStatusActive
	verifiedAt := now
	user.EmailVerifiedAt = &verifiedAt
	user.Version++
	user.UpdatedAt = now
	return EmailVerificationResult{User: user, GrantID: grantID, VerifiedAt: now}, nil
}

func invalidateVerificationGrant(ctx context.Context, tx *sql.Tx, grantID, userID, correlationID string, now time.Time, reason string) error {
	if tx == nil || strings.TrimSpace(grantID) == "" || strings.TrimSpace(userID) == "" || !validCorrelationID(correlationID) || (reason != "expired" && reason != "attempt_limit") {
		return ErrInvalid
	}
	result, err := tx.ExecContext(ctx, `
UPDATE auth_one_time_grants
SET invalidated_at=?,attempt_count=LEAST(attempt_count+1,max_attempts)
WHERE id=? AND consumed_at IS NULL AND invalidated_at IS NULL`, now, grantID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrConflict
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('public','',?,'auth.email_verification.denied','auth_one_time_grant',?,'denied',?,JSON_OBJECT('purpose','email_verification','reason',?),?)`,
		userID, grantID, correlationID, reason, now)
	return err
}

func validVerificationCode(code string) bool {
	if len(code) < 40 || len(code) > 128 || !strings.HasPrefix(code, "gvc_") {
		return false
	}
	for _, r := range code[4:] {
		if !(r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
