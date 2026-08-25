package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	defaultPasswordSessionTTL = 30 * 24 * time.Hour
	passwordTimingDummyHash   = "pbkdf2-sha256$600000$R29KZXRQMTVEdW1teVNhbHQ$FE1V29Z-H9_2LCpHXlnJsBnhGH6b_q8BanyeJsSMHBI"
)

type PasswordLoginInput struct {
	Email         string
	Password      string
	CorrelationID string
}

type PasswordLoginService struct {
	db         *sql.DB
	sessionTTL time.Duration
}

func NewPasswordLoginService(db *sql.DB, sessionTTL time.Duration) (*PasswordLoginService, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	if sessionTTL == 0 {
		sessionTTL = defaultPasswordSessionTTL
	}
	if sessionTTL < 5*time.Minute || sessionTTL > 90*24*time.Hour {
		return nil, ErrInvalid
	}
	return &PasswordLoginService{db: db, sessionTTL: sessionTTL}, nil
}

func (s *PasswordLoginService) LoginPassword(ctx context.Context, input PasswordLoginInput) (SessionSecret, error) {
	if s == nil || s.db == nil || !validPassword(input.Password) || !validCorrelationID(strings.TrimSpace(input.CorrelationID)) {
		return SessionSecret{}, ErrInvalid
	}
	normalized, err := NormalizeEmail(input.Email)
	if err != nil {
		return SessionSecret{}, ErrUnauthorized
	}
	correlationID := strings.TrimSpace(input.CorrelationID)
	now := time.Now().UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return SessionSecret{}, err
	}
	defer tx.Rollback()

	var (
		user              User
		verifiedAt        sql.NullTime
		passwordChangedAt sql.NullTime
		passwordHash      string
		passwordAlgo      string
		passwordVersion   uint64
		failedAttempts    uint32
		lockedUntil       sql.NullTime
	)
	err = tx.QueryRowContext(ctx, `
SELECT u.id,u.email,u.email_normalized,u.display_name,u.status,u.email_verified_at,u.password_changed_at,u.version,u.created_at,u.updated_at,
       c.password_hash,c.password_algorithm,c.password_version,c.failed_attempts,c.locked_until
FROM auth_users u
JOIN auth_credentials c ON c.user_id=u.id
WHERE u.email_normalized=?
FOR UPDATE`, normalized).Scan(
		&user.ID,
		&user.Email,
		&user.EmailNormalized,
		&user.DisplayName,
		&user.Status,
		&verifiedAt,
		&passwordChangedAt,
		&user.Version,
		&user.CreatedAt,
		&user.UpdatedAt,
		&passwordHash,
		&passwordAlgo,
		&passwordVersion,
		&failedAttempts,
		&lockedUntil,
	)
	if errors.Is(err, sql.ErrNoRows) {
		_ = verifyPassword(passwordTimingDummyHash, input.Password)
		if auditErr := recordPasswordLoginAuditTx(ctx, tx, "", correlationID, "denied", "invalid_credentials", now); auditErr != nil {
			return SessionSecret{}, auditErr
		}
		if err := tx.Commit(); err != nil {
			return SessionSecret{}, err
		}
		return SessionSecret{}, ErrUnauthorized
	}
	if err != nil {
		return SessionSecret{}, err
	}
	if verifiedAt.Valid {
		v := verifiedAt.Time.UTC()
		user.EmailVerifiedAt = &v
	}
	if passwordChangedAt.Valid {
		v := passwordChangedAt.Time.UTC()
		user.PasswordChangedAt = &v
	}
	if passwordAlgo != passwordAlgorithm || passwordVersion != passwordAlgorithmVersion || !verifyPassword(passwordHash, input.Password) {
		if _, err := tx.ExecContext(ctx, `
UPDATE auth_credentials
SET failed_attempts=LEAST(failed_attempts+1,1000000),updated_at=?
WHERE user_id=?`, now, user.ID); err != nil {
			return SessionSecret{}, err
		}
		if err := recordPasswordLoginAuditTx(ctx, tx, user.ID, correlationID, "denied", "invalid_credentials", now); err != nil {
			return SessionSecret{}, err
		}
		if err := tx.Commit(); err != nil {
			return SessionSecret{}, err
		}
		return SessionSecret{}, ErrUnauthorized
	}

	if user.Status == UserStatusPendingVerification || user.EmailVerifiedAt == nil {
		if err := recordPasswordLoginAuditTx(ctx, tx, user.ID, correlationID, "denied", "verification_required", now); err != nil {
			return SessionSecret{}, err
		}
		if err := tx.Commit(); err != nil {
			return SessionSecret{}, err
		}
		return SessionSecret{}, ErrVerificationRequired
	}
	if user.Status == UserStatusLocked || (lockedUntil.Valid && lockedUntil.Time.After(now)) {
		if err := recordPasswordLoginAuditTx(ctx, tx, user.ID, correlationID, "denied", "account_locked", now); err != nil {
			return SessionSecret{}, err
		}
		if err := tx.Commit(); err != nil {
			return SessionSecret{}, err
		}
		return SessionSecret{}, ErrLocked
	}
	if user.Status == UserStatusDisabled {
		if err := recordPasswordLoginAuditTx(ctx, tx, user.ID, correlationID, "denied", "account_disabled", now); err != nil {
			return SessionSecret{}, err
		}
		if err := tx.Commit(); err != nil {
			return SessionSecret{}, err
		}
		return SessionSecret{}, ErrForbidden
	}
	if user.Status != UserStatusActive {
		return SessionSecret{}, ErrForbidden
	}

	sessionSecret, err := newPasswordSession(user.ID, s.sessionTTL, correlationID, now)
	if err != nil {
		return SessionSecret{}, err
	}
	tokenHash := HashOpaque(sessionSecret.Token)
	csrfHash := HashOpaque(sessionSecret.CSRFToken)
	_, err = tx.ExecContext(ctx, `
INSERT INTO auth_sessions
(id,user_id,token_hash,csrf_secret_hash,status,expires_at,last_seen_at,correlation_id,created_at,updated_at)
VALUES (?,?,?,?,'active',?,?,?,?,?)`,
		sessionSecret.Session.ID,
		sessionSecret.Session.UserID,
		tokenHash[:],
		csrfHash[:],
		sessionSecret.Session.ExpiresAt,
		sessionSecret.Session.LastSeenAt,
		sessionSecret.Session.CorrelationID,
		sessionSecret.Session.CreatedAt,
		sessionSecret.Session.UpdatedAt,
	)
	if err != nil {
		if mysqlDuplicate(err) {
			return SessionSecret{}, ErrConflict
		}
		return SessionSecret{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE auth_credentials SET failed_attempts=0,locked_until=NULL,updated_at=? WHERE user_id=?`, now, user.ID); err != nil {
		return SessionSecret{}, err
	}
	if err := recordPasswordLoginAuditTx(ctx, tx, user.ID, correlationID, "success", "password", now); err != nil {
		return SessionSecret{}, err
	}
	if err := tx.Commit(); err != nil {
		return SessionSecret{}, err
	}
	return sessionSecret, nil
}

func newPasswordSession(userID string, ttl time.Duration, correlationID string, now time.Time) (SessionSecret, error) {
	id, err := newOpaqueID("ses_", 18)
	if err != nil {
		return SessionSecret{}, err
	}
	token, err := NewOpaqueSecret("gst_", 32)
	if err != nil {
		return SessionSecret{}, err
	}
	csrf, err := NewOpaqueSecret("gcs_", 32)
	if err != nil {
		return SessionSecret{}, err
	}
	expiresAt := now.Add(ttl)
	return SessionSecret{
		Session: Session{
			ID:            id,
			UserID:        userID,
			Status:        SessionStatusActive,
			ExpiresAt:     expiresAt,
			LastSeenAt:    now,
			CorrelationID: correlationID,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		Token:     token.Value,
		CSRFToken: csrf.Value,
	}, nil
}

func recordPasswordLoginAuditTx(ctx context.Context, tx *sql.Tx, userID, correlationID, result, reason string, now time.Time) error {
	if tx == nil || !validCorrelationID(correlationID) || (result != "success" && result != "denied") {
		return ErrInvalid
	}
	if reason != "password" && reason != "invalid_credentials" && reason != "verification_required" && reason != "account_locked" && reason != "account_disabled" {
		return ErrInvalid
	}
	var userValue any
	resourceID := ""
	if strings.TrimSpace(userID) != "" {
		userValue = userID
		resourceID = userID
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('public','',?,'auth.login.password','auth_user',?,?,?,JSON_OBJECT('method','password','reason',?),?)`,
		userValue, resourceID, result, correlationID, reason, now)
	return err
}
