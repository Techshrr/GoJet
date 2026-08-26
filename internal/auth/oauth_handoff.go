package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	OAuthBrowserHandoffTTL     = 5 * time.Minute
	OAuthSocialRegistrationTTL = 15 * time.Minute
	OAuthHandoffKindCallback   = "callback"
	OAuthHandoffKindSocial     = "social_registration"
)

type OAuthBrowserHandoff struct {
	Code      string
	ExpiresAt time.Time
}

type OAuthHandoffExchange struct {
	Session                *SessionSecret
	SocialRegistrationCode string
	ExpiresAt              time.Time
}

// CreateBrowserHandoff converts an already validated provider callback into a
// short-lived browser-safe authority. Provider subject material is hashed before
// persistence and the raw browser code is never stored.
func (s *OAuthService) CreateBrowserHandoff(ctx context.Context, callback OAuthCallbackResult, correlationID string, now time.Time) (OAuthBrowserHandoff, error) {
	correlationID = strings.TrimSpace(correlationID)
	if s == nil || s.db == nil || !ValidProvider(callback.Provider) || callback.Intent == OAuthIntentBind ||
		(callback.Intent != OAuthIntentLogin && callback.Intent != OAuthIntentRegister) || strings.TrimSpace(callback.StateID) == "" ||
		strings.TrimSpace(callback.ProviderSubject) == "" || !validCorrelationID(correlationID) {
		return OAuthBrowserHandoff{}, ErrInvalid
	}

	when := now.UTC().Truncate(time.Microsecond)
	if when.IsZero() {
		return OAuthBrowserHandoff{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return OAuthBrowserHandoff{}, err
	}
	defer tx.Rollback()

	var stateProvider, stateIntent string
	var stateUserID, stateSessionID sql.NullString
	var stateConsumed sql.NullTime
	if err := tx.QueryRowContext(ctx, `
SELECT provider,intent,initiating_user_id,initiating_session_id,consumed_at
FROM oauth_states WHERE id=? FOR UPDATE`, callback.StateID).
		Scan(&stateProvider, &stateIntent, &stateUserID, &stateSessionID, &stateConsumed); errors.Is(err, sql.ErrNoRows) {
		return OAuthBrowserHandoff{}, ErrForbidden
	} else if err != nil {
		return OAuthBrowserHandoff{}, err
	}
	if !stateConsumed.Valid || stateProvider != callback.Provider || stateIntent != callback.Intent ||
		stateUserID.String != callback.InitiatingUserID || stateSessionID.String != callback.InitiatingSessionID {
		return OAuthBrowserHandoff{}, ErrForbidden
	}

	subjectHash := HashOpaque(callback.Provider + "\x00" + strings.TrimSpace(callback.ProviderSubject))
	var identityID, userID sql.NullString
	err = tx.QueryRowContext(ctx, `
SELECT id,user_id FROM oauth_identities
WHERE provider=? AND provider_subject_hash=?
LIMIT 1 FOR UPDATE`, callback.Provider, subjectHash[:]).Scan(&identityID, &userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return OAuthBrowserHandoff{}, err
	}
	if err == nil && callback.Intent == OAuthIntentRegister {
		return OAuthBrowserHandoff{}, ErrConflict
	}

	var emailValue any
	verified := callback.ProviderEmailVerified
	if strings.TrimSpace(callback.ProviderEmail) != "" {
		normalized, err := NormalizeEmail(callback.ProviderEmail)
		if err != nil {
			return OAuthBrowserHandoff{}, ErrForbidden
		}
		emailValue = normalized
	} else {
		verified = false
	}

	handoffID, err := newOpaqueID("ohd_", 18)
	if err != nil {
		return OAuthBrowserHandoff{}, err
	}
	code, err := NewOpaqueSecret("goh_", 32)
	if err != nil {
		return OAuthBrowserHandoff{}, err
	}
	expiresAt := when.Add(OAuthBrowserHandoffTTL)
	var identityValue, userValue any
	if identityID.Valid {
		identityValue = identityID.String
	}
	if userID.Valid {
		userValue = userID.String
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO oauth_handoffs
(id,provider,handoff_kind,code_hash,intent,user_id,oauth_identity_id,provider_subject_hash,email_normalized,
 provider_email_verified,display_name,expires_at,consumed_at,correlation_id,created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?, ?,NULL,?,?)`,
		handoffID, callback.Provider, OAuthHandoffKindCallback, code.Hash[:], callback.Intent, userValue, identityValue,
		subjectHash[:], emailValue, verified, strings.TrimSpace(callback.DisplayName), expiresAt, correlationID, when)
	if err != nil {
		if mysqlDuplicate(err) {
			return OAuthBrowserHandoff{}, ErrConflict
		}
		return OAuthBrowserHandoff{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('system','',?,'auth.oauth.handoff.created','oauth_handoff',?,'success',?,JSON_OBJECT('provider',?,'intent',?,'kind','callback'),?)`,
		userValue, handoffID, correlationID, callback.Provider, callback.Intent, when); err != nil {
		return OAuthBrowserHandoff{}, err
	}
	if err := tx.Commit(); err != nil {
		return OAuthBrowserHandoff{}, err
	}
	return OAuthBrowserHandoff{Code: code.Value, ExpiresAt: expiresAt}, nil
}

// ExchangeBrowserHandoff consumes the callback handoff exactly once. Existing
// provider identities receive a fresh server session; unbound identities receive
// only a second short-lived opaque social-registration continuation code.
func (s *OAuthService) ExchangeBrowserHandoff(ctx context.Context, rawCode, correlationID string, now time.Time) (OAuthHandoffExchange, error) {
	rawCode = strings.TrimSpace(rawCode)
	correlationID = strings.TrimSpace(correlationID)
	if s == nil || s.db == nil || !strings.HasPrefix(rawCode, "goh_") || !validCorrelationID(correlationID) {
		return OAuthHandoffExchange{}, ErrInvalid
	}
	when := now.UTC().Truncate(time.Microsecond)
	hash := HashOpaque(rawCode)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return OAuthHandoffExchange{}, err
	}
	defer tx.Rollback()

	var (
		handoffID, provider, intent, displayName string
		userID, identityID, email                sql.NullString
		subjectHash                              []byte
		providerEmailVerified                    bool
		expiresAt                                time.Time
		consumedAt                               sql.NullTime
	)
	err = tx.QueryRowContext(ctx, `
SELECT id,provider,intent,user_id,oauth_identity_id,provider_subject_hash,email_normalized,
       provider_email_verified,display_name,expires_at,consumed_at
FROM oauth_handoffs
WHERE handoff_kind=? AND code_hash=?
LIMIT 1 FOR UPDATE`, OAuthHandoffKindCallback, hash[:]).Scan(
		&handoffID, &provider, &intent, &userID, &identityID, &subjectHash, &email,
		&providerEmailVerified, &displayName, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthHandoffExchange{}, ErrUnauthorized
	}
	if err != nil {
		return OAuthHandoffExchange{}, err
	}
	if consumedAt.Valid {
		return OAuthHandoffExchange{}, ErrReplay
	}
	if !expiresAt.After(when) {
		return OAuthHandoffExchange{}, ErrExpired
	}
	if !ValidProvider(provider) || (intent != OAuthIntentLogin && intent != OAuthIntentRegister) || len(subjectHash) != 32 {
		return OAuthHandoffExchange{}, ErrForbidden
	}
	res, err := tx.ExecContext(ctx, `UPDATE oauth_handoffs SET consumed_at=? WHERE id=? AND consumed_at IS NULL`, when, handoffID)
	if err != nil {
		return OAuthHandoffExchange{}, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return OAuthHandoffExchange{}, err
	}
	if rows != 1 {
		return OAuthHandoffExchange{}, ErrReplay
	}

	if userID.Valid || identityID.Valid {
		if !userID.Valid || !identityID.Valid {
			return OAuthHandoffExchange{}, ErrForbidden
		}
		var storedUserID, storedProvider string
		var storedSubject []byte
		if err := tx.QueryRowContext(ctx, `
SELECT user_id,provider,provider_subject_hash FROM oauth_identities WHERE id=? FOR UPDATE`, identityID.String).
			Scan(&storedUserID, &storedProvider, &storedSubject); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return OAuthHandoffExchange{}, ErrForbidden
			}
			return OAuthHandoffExchange{}, err
		}
		if storedUserID != userID.String || storedProvider != provider || len(storedSubject) != 32 || !EqualOpaqueHash(bytes32(storedSubject), bytes32(subjectHash)) {
			return OAuthHandoffExchange{}, ErrForbidden
		}
		var status string
		var verifiedAt sql.NullTime
		if err := tx.QueryRowContext(ctx, `SELECT status,email_verified_at FROM auth_users WHERE id=? FOR UPDATE`, userID.String).
			Scan(&status, &verifiedAt); err != nil {
			return OAuthHandoffExchange{}, err
		}
		if status == UserStatusLocked {
			return OAuthHandoffExchange{}, ErrLocked
		}
		if status != UserStatusActive || !verifiedAt.Valid {
			return OAuthHandoffExchange{}, ErrForbidden
		}
		sessionSecret, err := newPasswordSession(userID.String, defaultPasswordSessionTTL, correlationID, when)
		if err != nil {
			return OAuthHandoffExchange{}, err
		}
		tokenHash := HashOpaque(sessionSecret.Token)
		csrfHash := HashOpaque(sessionSecret.CSRFToken)
		if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_sessions
(id,user_id,token_hash,csrf_secret_hash,status,expires_at,last_seen_at,correlation_id,created_at,updated_at)
VALUES (?,?,?,?,'active',?,?,?,?,?)`,
			sessionSecret.Session.ID, userID.String, tokenHash[:], csrfHash[:], sessionSecret.Session.ExpiresAt,
			sessionSecret.Session.LastSeenAt, correlationID, when, when); err != nil {
			return OAuthHandoffExchange{}, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('public','',?,'auth.oauth.handoff.login','oauth_handoff',?,'success',?,JSON_OBJECT('provider',?),?)`,
			userID.String, handoffID, correlationID, provider, when); err != nil {
			return OAuthHandoffExchange{}, err
		}
		if err := tx.Commit(); err != nil {
			return OAuthHandoffExchange{}, err
		}
		return OAuthHandoffExchange{Session: &sessionSecret, ExpiresAt: sessionSecret.Session.ExpiresAt}, nil
	}

	socialID, err := newOpaqueID("ohd_", 18)
	if err != nil {
		return OAuthHandoffExchange{}, err
	}
	socialCode, err := NewOpaqueSecret("gsr_", 32)
	if err != nil {
		return OAuthHandoffExchange{}, err
	}
	socialExpires := when.Add(OAuthSocialRegistrationTTL)
	var emailValue any
	if email.Valid {
		emailValue = email.String
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO oauth_handoffs
(id,provider,handoff_kind,code_hash,intent,user_id,oauth_identity_id,provider_subject_hash,email_normalized,
 provider_email_verified,display_name,expires_at,consumed_at,correlation_id,created_at)
VALUES (?,?,?,?,'register',NULL,NULL,?,?,?,?,?,NULL,?,?)`,
		socialID, provider, OAuthHandoffKindSocial, socialCode.Hash[:], subjectHash, emailValue,
		providerEmailVerified, displayName, socialExpires, correlationID, when)
	if err != nil {
		return OAuthHandoffExchange{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('public','',NULL,'auth.oauth.handoff.social','oauth_handoff',?,'success',?,JSON_OBJECT('provider',?,'kind','social_registration'),?)`,
		socialID, correlationID, provider, when); err != nil {
		return OAuthHandoffExchange{}, err
	}
	if err := tx.Commit(); err != nil {
		return OAuthHandoffExchange{}, err
	}
	return OAuthHandoffExchange{SocialRegistrationCode: socialCode.Value, ExpiresAt: socialExpires}, nil
}

func bytes32(raw []byte) [32]byte {
	var out [32]byte
	if len(raw) == len(out) {
		copy(out[:], raw)
	}
	return out
}
