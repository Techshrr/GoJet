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
	LoginEmailCodeTTL         = 10 * time.Minute
	loginEmailCodeIssueWindow = 60 * time.Second
)

type EmailCodeService struct {
	db         *sql.DB
	grantKey   securetoken.Key
	sessionTTL time.Duration
}

func NewEmailCodeService(db *sql.DB, grantKey securetoken.Key, sessionTTL time.Duration) (*EmailCodeService, error) {
	if db == nil || strings.TrimSpace(grantKey.ID()) == "" {
		return nil, ErrInvalid
	}
	if sessionTTL == 0 {
		sessionTTL = defaultPasswordSessionTTL
	}
	if sessionTTL < 5*time.Minute || sessionTTL > 90*24*time.Hour {
		return nil, ErrInvalid
	}
	return &EmailCodeService{db: db, grantKey: grantKey, sessionTTL: sessionTTL}, nil
}

func (s *EmailCodeService) RequestLoginCode(ctx context.Context, email, correlationID string) error {
	if s == nil || s.db == nil || !validCorrelationID(strings.TrimSpace(correlationID)) {
		return ErrInvalid
	}
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return ErrUnauthorized
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
	err = tx.QueryRowContext(ctx, `
SELECT id,email,status,email_verified_at
FROM auth_users
WHERE email_normalized=?
FOR UPDATE`, normalized).Scan(&userID, &storedEmail, &status, &verifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return err
	}
	if status == UserStatusPendingVerification || !verifiedAt.Valid {
		return ErrVerificationRequired
	}
	if status == UserStatusLocked {
		return ErrLocked
	}
	if status != UserStatusActive {
		return ErrForbidden
	}

	var recent int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM auth_one_time_grants
WHERE user_id=? AND purpose='login_email_code'
  AND consumed_at IS NULL AND invalidated_at IS NULL AND created_at>=?`,
		userID, now.Add(-loginEmailCodeIssueWindow)).Scan(&recent); err != nil {
		return err
	}
	if recent > 0 {
		return ErrRateLimited
	}

	grantID, err := newOpaqueID("grt_", 18)
	if err != nil {
		return err
	}
	code, err := s.grantKey.Derive("glc_", "login_email_code", grantID)
	if err != nil {
		return err
	}
	hash := securetoken.Hash(code)
	expiresAt := now.Add(LoginEmailCodeTTL)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_one_time_grants
(id,purpose,user_id,email_normalized,token_hash,token_key_id,attempt_count,max_attempts,expires_at,consumed_at,invalidated_at,correlation_id,created_at)
VALUES (?,'login_email_code',?,?,?,?,0,8,?,NULL,NULL,?,?)`,
		grantID, userID, normalized, hash[:], s.grantKey.ID(), expiresAt, correlationID, now); err != nil {
		return err
	}
	if err := support.EnqueueMailTx(ctx, tx, support.MailEnqueueInput{
		TemplateKey:    "auth-login-email-code",
		Locale:         "en",
		RecipientKind:  "auth_user",
		RecipientValue: storedEmail,
		ResourceType:   "auth_one_time_grant",
		ResourceID:     grantID,
	}, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('public','',?,'auth.login.email_code.issued','auth_one_time_grant',?,'success',?,JSON_OBJECT('purpose','login_email_code'),?)`,
		userID, grantID, correlationID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *EmailCodeService) ConsumeLoginCode(ctx context.Context, rawCode, correlationID string) (SessionSecret, error) {
	if s == nil || s.db == nil || strings.TrimSpace(rawCode) == "" || !validCorrelationID(strings.TrimSpace(correlationID)) {
		return SessionSecret{}, ErrInvalid
	}
	correlationID = strings.TrimSpace(correlationID)
	now := time.Now().UTC().Truncate(time.Microsecond)
	hash := securetoken.Hash(strings.TrimSpace(rawCode))
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return SessionSecret{}, err
	}
	defer tx.Rollback()

	var grantID, userID string
	var expiresAt time.Time
	var consumedAt, invalidatedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
SELECT id,user_id,expires_at,consumed_at,invalidated_at
FROM auth_one_time_grants
WHERE token_hash=? AND purpose='login_email_code'
FOR UPDATE`, hash[:]).Scan(&grantID, &userID, &expiresAt, &consumedAt, &invalidatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionSecret{}, ErrUnauthorized
	}
	if err != nil {
		return SessionSecret{}, err
	}
	if consumedAt.Valid {
		return SessionSecret{}, ErrReplay
	}
	if invalidatedAt.Valid {
		return SessionSecret{}, ErrRevoked
	}
	if !expiresAt.After(now) {
		if _, err := tx.ExecContext(ctx, `UPDATE auth_one_time_grants SET invalidated_at=? WHERE id=? AND invalidated_at IS NULL`, now, grantID); err != nil {
			return SessionSecret{}, err
		}
		if err := tx.Commit(); err != nil {
			return SessionSecret{}, err
		}
		return SessionSecret{}, ErrExpired
	}

	var status string
	var verifiedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT status,email_verified_at FROM auth_users WHERE id=? FOR UPDATE`, userID).Scan(&status, &verifiedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionSecret{}, ErrUnauthorized
		}
		return SessionSecret{}, err
	}
	if status == UserStatusLocked {
		return SessionSecret{}, ErrLocked
	}
	if status != UserStatusActive || !verifiedAt.Valid {
		return SessionSecret{}, ErrForbidden
	}

	sessionSecret, err := newPasswordSession(userID, s.sessionTTL, correlationID, now)
	if err != nil {
		return SessionSecret{}, err
	}
	tokenHash := HashOpaque(sessionSecret.Token)
	csrfHash := HashOpaque(sessionSecret.CSRFToken)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_sessions
(id,user_id,token_hash,csrf_secret_hash,status,expires_at,last_seen_at,correlation_id,created_at,updated_at)
VALUES (?,?,?,?,'active',?,?,?,?,?)`,
		sessionSecret.Session.ID, userID, tokenHash[:], csrfHash[:], sessionSecret.Session.ExpiresAt,
		sessionSecret.Session.LastSeenAt, correlationID, now, now); err != nil {
		return SessionSecret{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE auth_one_time_grants SET consumed_at=? WHERE id=? AND consumed_at IS NULL AND invalidated_at IS NULL`, now, grantID); err != nil {
		return SessionSecret{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('public','',?,'auth.login.email_code','auth_one_time_grant',?,'success',?,JSON_OBJECT('method','email_code'),?)`,
		userID, grantID, correlationID, now); err != nil {
		return SessionSecret{}, err
	}
	if err := tx.Commit(); err != nil {
		return SessionSecret{}, err
	}
	return sessionSecret, nil
}
