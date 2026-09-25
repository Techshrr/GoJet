package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// UpdateProviderConfigForAdministratorTx participates in the P17 administrator
// transaction. The caller must enforce administrator authentication, settings
// permission, MFA, CSRF, idempotency and audit before committing this transaction.
func (s *OAuthService) UpdateProviderConfigForAdministratorTx(ctx context.Context, tx *sql.Tx, actorID string, expectedVersion uint64, input OAuthProviderUpdate, now time.Time) (OAuthProviderConfig, error) {
	input.Provider = strings.TrimSpace(input.Provider)
	input.ClientID = strings.TrimSpace(input.ClientID)
	input.ClientSecret = strings.TrimSpace(input.ClientSecret)
	if s == nil || s.crypto == nil || tx == nil || actorID == "" || expectedVersion == 0 || !ValidProvider(input.Provider) || input.ClientID == "" || len(input.ClientID) > 255 || len(input.ClientSecret) > 800 {
		return OAuthProviderConfig{}, ErrInvalid
	}
	if err := validateProviderURLs(input.Provider, input.AuthorizationURL, input.TokenURL, input.UserInfoURL, input.RedirectURI); err != nil {
		return OAuthProviderConfig{}, err
	}
	scopes, err := normalizeScopes(input.Scopes)
	if err != nil {
		return OAuthProviderConfig{}, err
	}
	var version uint64
	var ciphertext []byte
	var keyID sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT version,client_secret_ciphertext,secret_key_id FROM oauth_provider_configs WHERE provider=? FOR UPDATE`, input.Provider).Scan(&version, &ciphertext, &keyID)
	if err != nil {
		return OAuthProviderConfig{}, err
	}
	if version != expectedVersion {
		return OAuthProviderConfig{}, ErrConflict
	}
	if input.ClientSecret != "" {
		ciphertext, err = s.crypto.Encrypt(input.ClientSecret, "oauth_client_secret:"+input.Provider)
		if err != nil {
			return OAuthProviderConfig{}, err
		}
		keyID = sql.NullString{String: s.crypto.KeyID(), Valid: true}
	}
	if len(ciphertext) == 0 || !keyID.Valid {
		return OAuthProviderConfig{}, ErrInvalid
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return OAuthProviderConfig{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE oauth_provider_configs SET enabled=?,client_id=?,client_secret_ciphertext=?,secret_key_id=?,authorization_url=?,token_url=?,userinfo_url=?,redirect_uri=?,scopes_json=?,version=version+1,updated_by=?,updated_at=? WHERE provider=?`, input.Enabled, input.ClientID, ciphertext, keyID.String, strings.TrimSpace(input.AuthorizationURL), strings.TrimSpace(input.TokenURL), strings.TrimSpace(input.UserInfoURL), strings.TrimSpace(input.RedirectURI), scopesJSON, actorID, now.UTC(), input.Provider)
	if err != nil {
		return OAuthProviderConfig{}, err
	}
	raw, err := s.loadRawProviderConfigTx(ctx, tx, input.Provider)
	return raw.safe, err
}
