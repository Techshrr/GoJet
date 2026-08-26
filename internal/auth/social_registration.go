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

const SocialEmailVerificationTTL = 15 * time.Minute

type SocialRegistrationState struct {
	Provider                  string
	Email                     string
	ProviderEmailVerified     bool
	RequiresEmailVerification bool
	DisplayName               string
	ExpiresAt                 time.Time
}

type SocialEmailVerificationGrant struct {
	ID              string
	HandoffID       string
	EmailNormalized string
	TokenKeyID      string
	ExpiresAt       time.Time
}

type SocialRegistrationService struct {
	db         *sql.DB
	grantKey   securetoken.Key
	sessionTTL time.Duration
}

func NewSocialRegistrationService(db *sql.DB, grantKey securetoken.Key, sessionTTL time.Duration) (*SocialRegistrationService, error) {
	if db == nil || strings.TrimSpace(grantKey.ID()) == "" {
		return nil, ErrInvalid
	}
	if sessionTTL == 0 {
		sessionTTL = defaultPasswordSessionTTL
	}
	if sessionTTL < 5*time.Minute || sessionTTL > 90*24*time.Hour {
		return nil, ErrInvalid
	}
	return &SocialRegistrationService{db: db, grantKey: grantKey, sessionTTL: sessionTTL}, nil
}

func (s *SocialRegistrationService) GetState(ctx context.Context, rawSocialCode string, now time.Time) (SocialRegistrationState, error) {
	rawSocialCode = strings.TrimSpace(rawSocialCode)
	if s == nil || s.db == nil || !strings.HasPrefix(rawSocialCode, "gsr_") {
		return SocialRegistrationState{}, ErrInvalid
	}
	hash := HashOpaque(rawSocialCode)
	var provider, displayName string
	var subjectHash []byte
	var email sql.NullString
	var providerVerified bool
	var expiresAt time.Time
	var consumedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
SELECT provider,provider_subject_hash,email_normalized,provider_email_verified,display_name,expires_at,consumed_at
FROM oauth_handoffs
WHERE handoff_kind=? AND code_hash=?
LIMIT 1`, OAuthHandoffKindSocial, hash[:]).Scan(
		&provider, &subjectHash, &email, &providerVerified, &displayName, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SocialRegistrationState{}, ErrUnauthorized
	}
	if err != nil {
		return SocialRegistrationState{}, err
	}
	when := now.UTC()
	if consumedAt.Valid {
		return SocialRegistrationState{}, ErrReplay
	}
	if !expiresAt.After(when) {
		return SocialRegistrationState{}, ErrExpired
	}
	if !ValidProvider(provider) || len(subjectHash) != 32 {
		return SocialRegistrationState{}, ErrForbidden
	}
	var existing int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_identities WHERE provider=? AND provider_subject_hash=?`, provider, subjectHash).Scan(&existing); err != nil {
		return SocialRegistrationState{}, err
	}
	if existing != 0 {
		return SocialRegistrationState{}, ErrConflict
	}
	state := SocialRegistrationState{Provider: provider, ProviderEmailVerified: providerVerified, DisplayName: displayName, ExpiresAt: expiresAt}
	if email.Valid {
		state.Email = email.String
	}
	state.RequiresEmailVerification = !providerVerified || !email.Valid || strings.TrimSpace(email.String) == ""
	return state, nil
}

// RequestEmailVerification binds a user-supplied email to an active social
// continuation and queues delivery through the inherited P14 mail authority.
// The raw verification code is deterministic from the server-held key and grant
// id, but is never returned by this service or persisted in plaintext.
func (s *SocialRegistrationService) RequestEmailVerification(ctx context.Context, rawSocialCode, email, correlationID string, now time.Time) (SocialEmailVerificationGrant, error) {
	rawSocialCode = strings.TrimSpace(rawSocialCode)
	correlationID = strings.TrimSpace(correlationID)
	normalized, err := NormalizeEmail(email)
	if s == nil || s.db == nil || !strings.HasPrefix(rawSocialCode, "gsr_") || err != nil || !validCorrelationID(correlationID) {
		return SocialEmailVerificationGrant{}, ErrInvalid
	}
	when := now.UTC().Truncate(time.Microsecond)
	hash := HashOpaque(rawSocialCode)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return SocialEmailVerificationGrant{}, err
	}
	defer tx.Rollback()

	var handoffID, provider string
	var subjectHash []byte
	var expiresAt time.Time
	var consumedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
SELECT id,provider,provider_subject_hash,expires_at,consumed_at
FROM oauth_handoffs
WHERE handoff_kind=? AND code_hash=?
LIMIT 1 FOR UPDATE`, OAuthHandoffKindSocial, hash[:]).Scan(
		&handoffID, &provider, &subjectHash, &expiresAt, &consumedAt); errors.Is(err, sql.ErrNoRows) {
		return SocialEmailVerificationGrant{}, ErrUnauthorized
	} else if err != nil {
		return SocialEmailVerificationGrant{}, err
	}
	if consumedAt.Valid {
		return SocialEmailVerificationGrant{}, ErrReplay
	}
	if !expiresAt.After(when) {
		return SocialEmailVerificationGrant{}, ErrExpired
	}
	if !ValidProvider(provider) || len(subjectHash) != 32 {
		return SocialEmailVerificationGrant{}, ErrForbidden
	}
	var existingIdentity int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_identities WHERE provider=? AND provider_subject_hash=?`, provider, subjectHash).Scan(&existingIdentity); err != nil {
		return SocialEmailVerificationGrant{}, err
	}
	if existingIdentity != 0 {
		return SocialEmailVerificationGrant{}, ErrConflict
	}
	var existingUser int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_users WHERE email_normalized=?`, normalized).Scan(&existingUser); err != nil {
		return SocialEmailVerificationGrant{}, err
	}
	if existingUser != 0 {
		return SocialEmailVerificationGrant{}, ErrConflict
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE auth_one_time_grants
SET invalidated_at=?
WHERE oauth_handoff_id=? AND purpose='social_email_verification'
  AND consumed_at IS NULL AND invalidated_at IS NULL`, when, handoffID); err != nil {
		return SocialEmailVerificationGrant{}, err
	}
	grantID, err := newOpaqueID("grt_", 18)
	if err != nil {
		return SocialEmailVerificationGrant{}, err
	}
	code, err := s.grantKey.Derive("gsv_", "social_email_verification", grantID)
	if err != nil {
		return SocialEmailVerificationGrant{}, err
	}
	codeHash := securetoken.Hash(code)
	grantExpires := when.Add(SocialEmailVerificationTTL)
	if grantExpires.After(expiresAt) {
		grantExpires = expiresAt
	}
	if !grantExpires.After(when) {
		return SocialEmailVerificationGrant{}, ErrExpired
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_one_time_grants
(id,purpose,user_id,oauth_handoff_id,email_normalized,token_hash,token_key_id,attempt_count,max_attempts,expires_at,consumed_at,invalidated_at,correlation_id,created_at)
VALUES (?,'social_email_verification',NULL,?,?,?,?,0,8,?,NULL,NULL,?,?)`,
		grantID, handoffID, normalized, codeHash[:], s.grantKey.ID(), grantExpires, correlationID, when); err != nil {
		return SocialEmailVerificationGrant{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE oauth_handoffs
SET email_normalized=?,provider_email_verified=0
WHERE id=? AND consumed_at IS NULL`, normalized, handoffID); err != nil {
		return SocialEmailVerificationGrant{}, err
	}
	if err := support.EnqueueMailTx(ctx, tx, support.MailEnqueueInput{
		TemplateKey:    "auth-social-email-verification",
		Locale:         "en",
		RecipientKind:  "auth_social",
		RecipientValue: normalized,
		ResourceType:   "auth_one_time_grant",
		ResourceID:     grantID,
	}, when); err != nil {
		return SocialEmailVerificationGrant{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('public','',NULL,'auth.oauth.social.email_verification.issued','auth_one_time_grant',?,'success',?,JSON_OBJECT('provider',?,'purpose','social_email_verification'),?)`,
		grantID, correlationID, provider, when); err != nil {
		return SocialEmailVerificationGrant{}, err
	}
	if err := tx.Commit(); err != nil {
		return SocialEmailVerificationGrant{}, err
	}
	return SocialEmailVerificationGrant{ID: grantID, HandoffID: handoffID, EmailNormalized: normalized, TokenKeyID: s.grantKey.ID(), ExpiresAt: grantExpires}, nil
}

type CompleteSocialRegistrationInput struct {
	SocialCode       string
	VerificationCode string
	CorrelationID    string
}

func (s *SocialRegistrationService) Complete(ctx context.Context, input CompleteSocialRegistrationInput, now time.Time) (SessionSecret, error) {
	input.SocialCode = strings.TrimSpace(input.SocialCode)
	input.VerificationCode = strings.TrimSpace(input.VerificationCode)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if s == nil || s.db == nil || !strings.HasPrefix(input.SocialCode, "gsr_") || !validCorrelationID(input.CorrelationID) {
		return SessionSecret{}, ErrInvalid
	}
	when := now.UTC().Truncate(time.Microsecond)
	handoffHash := HashOpaque(input.SocialCode)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return SessionSecret{}, err
	}
	defer tx.Rollback()

	var handoffID, provider, displayName string
	var subjectHash []byte
	var email sql.NullString
	var providerVerified bool
	var expiresAt time.Time
	var consumedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
SELECT id,provider,provider_subject_hash,email_normalized,provider_email_verified,display_name,expires_at,consumed_at
FROM oauth_handoffs
WHERE handoff_kind=? AND code_hash=?
LIMIT 1 FOR UPDATE`, OAuthHandoffKindSocial, handoffHash[:]).Scan(
		&handoffID, &provider, &subjectHash, &email, &providerVerified, &displayName, &expiresAt, &consumedAt); errors.Is(err, sql.ErrNoRows) {
		return SessionSecret{}, ErrUnauthorized
	} else if err != nil {
		return SessionSecret{}, err
	}
	if consumedAt.Valid {
		return SessionSecret{}, ErrReplay
	}
	if !expiresAt.After(when) {
		return SessionSecret{}, ErrExpired
	}
	if !ValidProvider(provider) || len(subjectHash) != 32 || !email.Valid || strings.TrimSpace(email.String) == "" {
		return SessionSecret{}, ErrVerificationRequired
	}
	normalized, err := NormalizeEmail(email.String)
	if err != nil {
		return SessionSecret{}, ErrForbidden
	}

	var existingIdentity int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_identities WHERE provider=? AND provider_subject_hash=?`, provider, subjectHash).Scan(&existingIdentity); err != nil {
		return SessionSecret{}, err
	}
	if existingIdentity != 0 {
		return SessionSecret{}, ErrConflict
	}
	var existingUser int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_users WHERE email_normalized=?`, normalized).Scan(&existingUser); err != nil {
		return SessionSecret{}, err
	}
	if existingUser != 0 {
		return SessionSecret{}, ErrConflict
	}

	if !providerVerified {
		if input.VerificationCode == "" {
			return SessionSecret{}, ErrVerificationRequired
		}
		verificationHash := securetoken.Hash(input.VerificationCode)
		var grantID, grantEmail string
		var grantExpires time.Time
		var grantConsumed, grantInvalidated sql.NullTime
		err := tx.QueryRowContext(ctx, `
SELECT id,email_normalized,expires_at,consumed_at,invalidated_at
FROM auth_one_time_grants
WHERE purpose='social_email_verification' AND oauth_handoff_id=? AND token_hash=?
LIMIT 1 FOR UPDATE`, handoffID, verificationHash[:]).Scan(
			&grantID, &grantEmail, &grantExpires, &grantConsumed, &grantInvalidated)
		if errors.Is(err, sql.ErrNoRows) {
			return SessionSecret{}, ErrUnauthorized
		}
		if err != nil {
			return SessionSecret{}, err
		}
		if grantConsumed.Valid {
			return SessionSecret{}, ErrReplay
		}
		if grantInvalidated.Valid {
			return SessionSecret{}, ErrRevoked
		}
		if !grantExpires.After(when) {
			return SessionSecret{}, ErrExpired
		}
		if grantEmail != normalized {
			return SessionSecret{}, ErrForbidden
		}
		res, err := tx.ExecContext(ctx, `UPDATE auth_one_time_grants SET consumed_at=? WHERE id=? AND consumed_at IS NULL AND invalidated_at IS NULL`, when, grantID)
		if err != nil {
			return SessionSecret{}, err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return SessionSecret{}, err
		}
		if rows != 1 {
			return SessionSecret{}, ErrReplay
		}
	}

	userID, err := newOpaqueID("usr_", 18)
	if err != nil {
		return SessionSecret{}, err
	}
	identityID, err := newOpaqueID("oid_", 18)
	if err != nil {
		return SessionSecret{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_users
(id,email,email_normalized,display_name,status,email_verified_at,version,created_at,updated_at)
VALUES (?,?,?,?, 'active',?,1,?,?)`, userID, normalized, normalized, strings.TrimSpace(displayName), when, when, when); err != nil {
		if mysqlDuplicate(err) {
			return SessionSecret{}, ErrConflict
		}
		return SessionSecret{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO oauth_identities
(id,user_id,provider,provider_subject_hash,provider_email_normalized,provider_email_verified,display_name,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,?)`, identityID, userID, provider, subjectHash, normalized, providerVerified, strings.TrimSpace(displayName), when, when); err != nil {
		if mysqlDuplicate(err) {
			return SessionSecret{}, ErrConflict
		}
		return SessionSecret{}, err
	}
	sessionSecret, err := newPasswordSession(userID, s.sessionTTL, input.CorrelationID, when)
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
		sessionSecret.Session.LastSeenAt, input.CorrelationID, when, when); err != nil {
		return SessionSecret{}, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE oauth_handoffs SET consumed_at=?,user_id=?,oauth_identity_id=? WHERE id=? AND consumed_at IS NULL`, when, userID, identityID, handoffID)
	if err != nil {
		return SessionSecret{}, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return SessionSecret{}, err
	}
	if rows != 1 {
		return SessionSecret{}, ErrReplay
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('public','',?,'auth.oauth.social.registration','oauth_identity',?,'success',?,JSON_OBJECT('provider',?),?)`,
		userID, identityID, input.CorrelationID, provider, when); err != nil {
		return SessionSecret{}, err
	}
	if err := tx.Commit(); err != nil {
		return SessionSecret{}, err
	}
	return sessionSecret, nil
}
