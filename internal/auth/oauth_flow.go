package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

func (s *OAuthService) Start(ctx context.Context, input OAuthStartInput, now time.Time) (OAuthStartResult, error) {
	input.Provider = strings.TrimSpace(input.Provider)
	input.Intent = strings.TrimSpace(input.Intent)
	input.InitiatingUserID = strings.TrimSpace(input.InitiatingUserID)
	input.InitiatingSessionID = strings.TrimSpace(input.InitiatingSessionID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if s == nil || s.db == nil || s.crypto == nil || !ValidProvider(input.Provider) || !validOAuthIntent(input.Intent) || !validCorrelationID(input.CorrelationID) {
		return OAuthStartResult{}, ErrInvalid
	}
	if input.Intent == OAuthIntentBind {
		if input.InitiatingUserID == "" || input.InitiatingSessionID == "" {
			return OAuthStartResult{}, ErrUnauthorized
		}
		current := Session{ID: input.InitiatingSessionID, UserID: input.InitiatingUserID}
		if !input.MutationAuthority.consumeFor(current) {
			return OAuthStartResult{}, ErrForbidden
		}
		if err := requireCurrentSessionDB(ctx, s.db, current, now); err != nil {
			return OAuthStartResult{}, err
		}
	} else if input.InitiatingUserID != "" || input.InitiatingSessionID != "" {
		return OAuthStartResult{}, ErrInvalid
	}
	raw, err := s.loadRawProviderConfig(ctx, input.Provider)
	if err != nil {
		return OAuthStartResult{}, err
	}
	if !raw.safe.Enabled || !raw.safe.Configured {
		return OAuthStartResult{}, ErrForbidden
	}
	stateID, err := newOpaqueID("ost_", 18)
	if err != nil {
		return OAuthStartResult{}, err
	}
	state, err := NewOpaqueSecret("gos_", 32)
	if err != nil {
		return OAuthStartResult{}, err
	}
	verifier, err := NewOpaqueSecret("gpk_", 32)
	if err != nil {
		return OAuthStartResult{}, err
	}
	pkceCiphertext, err := s.crypto.Encrypt(verifier.Value, "oauth_pkce:"+stateID)
	if err != nil {
		return OAuthStartResult{}, err
	}
	when := now.UTC().Truncate(time.Microsecond)
	expiresAt := when.Add(s.stateTTL)
	var userValue, sessionValue any
	if input.InitiatingUserID != "" {
		userValue = input.InitiatingUserID
	}
	if input.InitiatingSessionID != "" {
		sessionValue = input.InitiatingSessionID
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO oauth_states
(id,provider,state_hash,initiating_user_id,initiating_session_id,intent,redirect_path,pkce_verifier_ciphertext,pkce_key_id,expires_at,consumed_at,correlation_id,created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,NULL,?,?)`,
		stateID, input.Provider, state.Hash[:], userValue, sessionValue, input.Intent, postOAuthPath(input.Intent), pkceCiphertext, s.crypto.KeyID(), expiresAt, input.CorrelationID, when)
	if err != nil {
		if mysqlDuplicate(err) {
			return OAuthStartResult{}, ErrConflict
		}
		return OAuthStartResult{}, err
	}
	challengeBytes := sha256.Sum256([]byte(verifier.Value))
	challenge := base64.RawURLEncoding.EncodeToString(challengeBytes[:])
	authorizationURL, err := buildAuthorizationURL(raw.safe, state.Value, challenge)
	if err != nil {
		return OAuthStartResult{}, err
	}
	return OAuthStartResult{StateID: stateID, Provider: input.Provider, Intent: input.Intent, AuthorizationURL: authorizationURL, State: state.Value, PKCEVerifier: verifier.Value, ExpiresAt: expiresAt}, nil
}

func (s *OAuthService) Callback(ctx context.Context, adapter OAuthProviderAdapter, input OAuthCallbackInput, now time.Time) (OAuthCallbackResult, error) {
	input.Provider = strings.TrimSpace(input.Provider)
	input.State = strings.TrimSpace(input.State)
	input.Code = strings.TrimSpace(input.Code)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if s == nil || s.db == nil || s.crypto == nil || adapter == nil || !ValidProvider(input.Provider) || !strings.HasPrefix(input.State, "gos_") || input.Code == "" || !validCorrelationID(input.CorrelationID) {
		return OAuthCallbackResult{}, ErrInvalid
	}
	stateHash := HashOpaque(input.State)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return OAuthCallbackResult{}, err
	}
	defer tx.Rollback()
	var (
		stateID             string
		intent              string
		initiatingUserID    sql.NullString
		initiatingSessionID sql.NullString
		pkceCiphertext      []byte
		pkceKeyID           sql.NullString
		expiresAt           time.Time
		consumedAt          sql.NullTime
	)
	err = tx.QueryRowContext(ctx, `
SELECT id,intent,initiating_user_id,initiating_session_id,pkce_verifier_ciphertext,pkce_key_id,expires_at,consumed_at
FROM oauth_states
WHERE provider=? AND state_hash=?
LIMIT 1 FOR UPDATE`, input.Provider, stateHash[:]).Scan(
		&stateID, &intent, &initiatingUserID, &initiatingSessionID, &pkceCiphertext, &pkceKeyID, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthCallbackResult{}, ErrForbidden
	}
	if err != nil {
		return OAuthCallbackResult{}, err
	}
	when := now.UTC().Truncate(time.Microsecond)
	if consumedAt.Valid {
		return OAuthCallbackResult{}, ErrReplay
	}
	if !expiresAt.After(when) {
		return OAuthCallbackResult{}, ErrExpired
	}
	if !pkceKeyID.Valid || len(pkceCiphertext) == 0 {
		return OAuthCallbackResult{}, ErrForbidden
	}
	verifier, err := s.crypto.Decrypt(pkceCiphertext, pkceKeyID.String, "oauth_pkce:"+stateID)
	if err != nil {
		return OAuthCallbackResult{}, ErrForbidden
	}
	raw, err := s.loadRawProviderConfigTx(ctx, tx, input.Provider)
	if err != nil {
		return OAuthCallbackResult{}, err
	}
	if !raw.safe.Enabled || !raw.safe.Configured {
		return OAuthCallbackResult{}, ErrForbidden
	}
	clientSecret, err := s.crypto.Decrypt(raw.ciphertext, raw.keyID.String, "oauth_client_secret:"+input.Provider)
	if err != nil {
		return OAuthCallbackResult{}, ErrForbidden
	}
	claim, err := adapter.Exchange(ctx, OAuthProviderExchangeRequest{Provider: input.Provider, Code: input.Code, ClientID: raw.safe.ClientID, ClientSecret: clientSecret, RedirectURI: raw.safe.RedirectURI, PKCEVerifier: verifier})
	if err != nil {
		return OAuthCallbackResult{}, ErrForbidden
	}
	claim.Subject = strings.TrimSpace(claim.Subject)
	claim.DisplayName = strings.TrimSpace(claim.DisplayName)
	if claim.Subject == "" || len(claim.Subject) > 1024 || len(claim.DisplayName) > 255 {
		return OAuthCallbackResult{}, ErrForbidden
	}
	if strings.TrimSpace(claim.Email) != "" {
		normalized, err := NormalizeEmail(claim.Email)
		if err != nil {
			return OAuthCallbackResult{}, ErrForbidden
		}
		claim.Email = normalized
	}
	res, err := tx.ExecContext(ctx, `UPDATE oauth_states SET consumed_at=? WHERE id=? AND consumed_at IS NULL`, when, stateID)
	if err != nil {
		return OAuthCallbackResult{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return OAuthCallbackResult{}, err
	}
	if n != 1 {
		return OAuthCallbackResult{}, ErrReplay
	}
	var userValue any
	if initiatingUserID.Valid {
		userValue = initiatingUserID.String
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('system','',?,'auth.oauth.callback','oauth_state',?,'success',?,JSON_OBJECT('provider',?,'intent',?),?)`,
		userValue, stateID, input.CorrelationID, input.Provider, intent, when); err != nil {
		return OAuthCallbackResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return OAuthCallbackResult{}, err
	}
	return OAuthCallbackResult{
		StateID:               stateID,
		Provider:              input.Provider,
		Intent:                intent,
		InitiatingUserID:      initiatingUserID.String,
		InitiatingSessionID:   initiatingSessionID.String,
		ProviderSubject:       claim.Subject,
		ProviderEmail:         claim.Email,
		ProviderEmailVerified: claim.EmailVerified,
		DisplayName:           claim.DisplayName,
	}, nil
}
