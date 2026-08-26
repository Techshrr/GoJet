package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return ErrInvalid
	}
	return s.db.PingContext(ctx)
}

func (s *Store) CreateUser(ctx context.Context, in CreateUserInput) (User, error) {
	if s == nil || s.db == nil {
		return User{}, ErrInvalid
	}
	email := strings.TrimSpace(in.Email)
	normalized, err := NormalizeEmail(email)
	if err != nil || len(strings.TrimSpace(in.DisplayName)) > 255 {
		return User{}, ErrInvalid
	}
	id, err := newOpaqueID("usr_", 18)
	if err != nil {
		return User{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err = s.db.ExecContext(ctx, `
INSERT INTO auth_users
(id,email,email_normalized,display_name,status,version,created_at,updated_at)
VALUES (?,?,?,?,'pending_verification',1,?,?)`,
		id, email, normalized, strings.TrimSpace(in.DisplayName), now, now)
	if err != nil {
		if mysqlDuplicate(err) {
			return User{}, ErrConflict
		}
		return User{}, err
	}
	return User{
		ID:              id,
		Email:           email,
		EmailNormalized: normalized,
		DisplayName:     strings.TrimSpace(in.DisplayName),
		Status:          UserStatusPendingVerification,
		Version:         1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (User, error) {
	if s == nil || s.db == nil || strings.TrimSpace(userID) == "" {
		return User{}, ErrInvalid
	}
	return scanUser(s.db.QueryRowContext(ctx, `
SELECT id,email,email_normalized,display_name,status,email_verified_at,password_changed_at,version,created_at,updated_at
FROM auth_users WHERE id=?`, userID))
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	if s == nil || s.db == nil {
		return User{}, ErrInvalid
	}
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return User{}, ErrInvalid
	}
	return scanUser(s.db.QueryRowContext(ctx, `
SELECT id,email,email_normalized,display_name,status,email_verified_at,password_changed_at,version,created_at,updated_at
FROM auth_users WHERE email_normalized=?`, normalized))
}

func (s *Store) CreateSession(ctx context.Context, userID string, ttl time.Duration, correlationID string) (SessionSecret, error) {
	if s == nil || s.db == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(correlationID) == "" || ttl < 5*time.Minute || ttl > 90*24*time.Hour {
		return SessionSecret{}, ErrInvalid
	}
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
	now := time.Now().UTC().Truncate(time.Microsecond)
	expires := now.Add(ttl)
	_, err = s.db.ExecContext(ctx, `
INSERT INTO auth_sessions
(id,user_id,token_hash,csrf_secret_hash,status,expires_at,last_seen_at,correlation_id,created_at,updated_at)
VALUES (?,?,?,?,'active',?,?,?,?,?)`,
		id, userID, token.Hash[:], csrf.Hash[:], expires, now, strings.TrimSpace(correlationID), now, now)
	if err != nil {
		if mysqlDuplicate(err) {
			return SessionSecret{}, ErrConflict
		}
		return SessionSecret{}, err
	}
	session := Session{
		ID:            id,
		UserID:        userID,
		Status:        SessionStatusActive,
		ExpiresAt:     expires,
		LastSeenAt:    now,
		CorrelationID: strings.TrimSpace(correlationID),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	return SessionSecret{Session: session, Token: token.Value, CSRFToken: csrf.Value}, nil
}

func (s *Store) GetSessionByToken(ctx context.Context, rawToken string, now time.Time) (Session, error) {
	if s == nil || s.db == nil || strings.TrimSpace(rawToken) == "" {
		return Session{}, ErrUnauthorized
	}
	hash := HashOpaque(rawToken)
	session, err := scanSession(s.db.QueryRowContext(ctx, `
SELECT id,user_id,status,expires_at,revoked_at,last_seen_at,correlation_id,created_at,updated_at
FROM auth_sessions WHERE token_hash=?`, hash[:]))
	if errors.Is(err, ErrNotFound) {
		return Session{}, ErrUnauthorized
	}
	if err != nil {
		return Session{}, err
	}
	if session.Status == SessionStatusRevoked {
		return Session{}, ErrRevoked
	}
	if session.Status == SessionStatusExpired || !session.ExpiresAt.After(now.UTC()) {
		return Session{}, ErrExpired
	}
	if session.Status != SessionStatusActive {
		return Session{}, ErrUnauthorized
	}
	return session, nil
}

func (s *Store) RevokeOwnedSession(ctx context.Context, userID, sessionID string, now time.Time) error {
	if s == nil || s.db == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return ErrInvalid
	}
	when := now.UTC().Truncate(time.Microsecond)
	res, err := s.db.ExecContext(ctx, `
UPDATE auth_sessions
SET status='revoked',revoked_at=?,updated_at=?
WHERE id=? AND user_id=? AND status='active'`, when, when, sessionID, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) BindOAuthIdentity(ctx context.Context, in BindOAuthIdentityInput) (OAuthIdentity, error) {
	if s == nil || s.db == nil || strings.TrimSpace(in.UserID) == "" || !ValidProvider(in.Provider) || strings.TrimSpace(in.ProviderSubject) == "" || len(strings.TrimSpace(in.DisplayName)) > 255 {
		return OAuthIdentity{}, ErrInvalid
	}
	providerEmail := ""
	if strings.TrimSpace(in.ProviderEmail) != "" {
		normalized, err := NormalizeEmail(in.ProviderEmail)
		if err != nil {
			return OAuthIdentity{}, ErrInvalid
		}
		providerEmail = normalized
	}
	id, err := newOpaqueID("oid_", 18)
	if err != nil {
		return OAuthIdentity{}, err
	}
	subjectHash := HashOpaque(in.Provider + "\x00" + in.ProviderSubject)
	now := time.Now().UTC().Truncate(time.Microsecond)
	var emailValue any
	if providerEmail == "" {
		emailValue = nil
	} else {
		emailValue = providerEmail
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO oauth_identities
(id,user_id,provider,provider_subject_hash,provider_email_normalized,provider_email_verified,display_name,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,?)`,
		id, in.UserID, in.Provider, subjectHash[:], emailValue, in.ProviderEmailVerified, strings.TrimSpace(in.DisplayName), now, now)
	if err != nil {
		if mysqlDuplicate(err) {
			return OAuthIdentity{}, ErrConflict
		}
		return OAuthIdentity{}, err
	}
	return OAuthIdentity{
		ID:                    id,
		UserID:                in.UserID,
		Provider:              in.Provider,
		ProviderEmail:         providerEmail,
		ProviderEmailVerified: in.ProviderEmailVerified,
		DisplayName:           strings.TrimSpace(in.DisplayName),
		CreatedAt:             now,
		UpdatedAt:             now,
	}, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (User, error) {
	var user User
	var verifiedAt, passwordChangedAt sql.NullTime
	err := row.Scan(
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
	)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	if verifiedAt.Valid {
		value := verifiedAt.Time
		user.EmailVerifiedAt = &value
	}
	if passwordChangedAt.Valid {
		value := passwordChangedAt.Time
		user.PasswordChangedAt = &value
	}
	return user, nil
}

func scanSession(row rowScanner) (Session, error) {
	var session Session
	var revokedAt sql.NullTime
	err := row.Scan(
		&session.ID,
		&session.UserID,
		&session.Status,
		&session.ExpiresAt,
		&revokedAt,
		&session.LastSeenAt,
		&session.CorrelationID,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	if revokedAt.Valid {
		value := revokedAt.Time
		session.RevokedAt = &value
	}
	return session, nil
}

func mysqlDuplicate(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "duplicate entry") || strings.Contains(text, "error 1062")
}
