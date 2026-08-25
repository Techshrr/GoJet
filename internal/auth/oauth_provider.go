package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
)

func (s *OAuthService) ListProviderConfigs(ctx context.Context) ([]OAuthProviderConfig, error) {
	if s == nil || s.db == nil {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT provider,enabled,client_id,client_secret_ciphertext,secret_key_id,
       authorization_url,token_url,userinfo_url,redirect_uri,scopes_json,version
FROM oauth_provider_configs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	configs := make(map[string]OAuthProviderConfig, len(Providers))
	for rows.Next() {
		cfg, err := scanProviderConfig(rows)
		if err != nil {
			return nil, err
		}
		configs[cfg.Provider] = cfg
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(configs) != len(Providers) {
		return nil, ErrConflict
	}
	out := make([]OAuthProviderConfig, 0, len(Providers))
	for _, provider := range Providers {
		cfg, ok := configs[provider]
		if !ok {
			return nil, ErrConflict
		}
		out = append(out, cfg)
	}
	return out, nil
}

func (s *OAuthService) UpdateProviderConfig(ctx context.Context, current Session, authority *UnsafeMutationAuthority, authorizer SettingsPermissionAuthorizer, actorID, correlationID string, input OAuthProviderUpdate, now time.Time) (OAuthProviderConfig, error) {
	actorID = strings.TrimSpace(actorID)
	correlationID = strings.TrimSpace(correlationID)
	input.Provider = strings.TrimSpace(input.Provider)
	input.ClientID = strings.TrimSpace(input.ClientID)
	input.ClientSecret = strings.TrimSpace(input.ClientSecret)
	if s == nil || s.db == nil || s.crypto == nil || !authority.consumeFor(current) || authorizer == nil || actorID == "" || actorID != current.UserID || !validCorrelationID(correlationID) || !ValidProvider(input.Provider) || input.ClientID == "" || input.ClientSecret == "" {
		return OAuthProviderConfig{}, ErrInvalid
	}
	if err := requireCurrentSessionDB(ctx, s.db, current, now); err != nil {
		return OAuthProviderConfig{}, err
	}
	if err := authorizer.Authorize(ctx, actorID, SettingsManage); err != nil {
		return OAuthProviderConfig{}, ErrForbidden
	}
	if err := validateProviderURLs(input.Provider, input.AuthorizationURL, input.TokenURL, input.UserInfoURL, input.RedirectURI); err != nil {
		return OAuthProviderConfig{}, err
	}
	scopes, err := normalizeScopes(input.Scopes)
	if err != nil {
		return OAuthProviderConfig{}, err
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return OAuthProviderConfig{}, err
	}
	ciphertext, err := s.crypto.Encrypt(input.ClientSecret, "oauth_client_secret:"+input.Provider)
	if err != nil {
		return OAuthProviderConfig{}, err
	}
	when := now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return OAuthProviderConfig{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
UPDATE oauth_provider_configs
SET enabled=?,client_id=?,client_secret_ciphertext=?,secret_key_id=?,authorization_url=?,token_url=?,userinfo_url=?,redirect_uri=?,scopes_json=?,version=version+1,updated_by=?,updated_at=?
WHERE provider=?`,
		input.Enabled, input.ClientID, ciphertext, s.crypto.KeyID(), strings.TrimSpace(input.AuthorizationURL), strings.TrimSpace(input.TokenURL), strings.TrimSpace(input.UserInfoURL), strings.TrimSpace(input.RedirectURI), scopesJSON, actorID, when, input.Provider)
	if err != nil {
		return OAuthProviderConfig{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return OAuthProviderConfig{}, err
	}
	if n != 1 {
		return OAuthProviderConfig{}, ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('admin',?,NULL,'auth.oauth.provider.updated','oauth_provider_config',?,'success',?,JSON_OBJECT('provider',?,'enabled',?),?)`,
		actorID, input.Provider, correlationID, input.Provider, input.Enabled, when); err != nil {
		return OAuthProviderConfig{}, err
	}
	if err := tx.Commit(); err != nil {
		return OAuthProviderConfig{}, err
	}
	return s.getProviderConfig(ctx, input.Provider)
}

type rawOAuthProviderConfig struct {
	safe       OAuthProviderConfig
	ciphertext []byte
	keyID      sql.NullString
}

func (s *OAuthService) getProviderConfig(ctx context.Context, provider string) (OAuthProviderConfig, error) {
	raw, err := s.loadRawProviderConfig(ctx, provider)
	return raw.safe, err
}

func (s *OAuthService) loadRawProviderConfig(ctx context.Context, provider string) (rawOAuthProviderConfig, error) {
	return scanRawProviderConfig(s.db.QueryRowContext(ctx, `
SELECT provider,enabled,client_id,client_secret_ciphertext,secret_key_id,
       authorization_url,token_url,userinfo_url,redirect_uri,scopes_json,version
FROM oauth_provider_configs WHERE provider=?`, provider))
}

func (s *OAuthService) loadRawProviderConfigTx(ctx context.Context, tx *sql.Tx, provider string) (rawOAuthProviderConfig, error) {
	return scanRawProviderConfig(tx.QueryRowContext(ctx, `
SELECT provider,enabled,client_id,client_secret_ciphertext,secret_key_id,
       authorization_url,token_url,userinfo_url,redirect_uri,scopes_json,version
FROM oauth_provider_configs WHERE provider=?`, provider))
}

func scanProviderConfig(scanner rowScanner) (OAuthProviderConfig, error) {
	raw, err := scanRawProviderConfig(scanner)
	return raw.safe, err
}

func scanRawProviderConfig(scanner rowScanner) (rawOAuthProviderConfig, error) {
	var (
		cfg        OAuthProviderConfig
		ciphertext []byte
		keyID      sql.NullString
		scopesJSON []byte
	)
	if err := scanner.Scan(&cfg.Provider, &cfg.Enabled, &cfg.ClientID, &ciphertext, &keyID, &cfg.AuthorizationURL, &cfg.TokenURL, &cfg.UserInfoURL, &cfg.RedirectURI, &scopesJSON, &cfg.Version); errors.Is(err, sql.ErrNoRows) {
		return rawOAuthProviderConfig{}, ErrNotFound
	} else if err != nil {
		return rawOAuthProviderConfig{}, err
	}
	if !ValidProvider(cfg.Provider) {
		return rawOAuthProviderConfig{}, ErrConflict
	}
	if len(scopesJSON) == 0 {
		return rawOAuthProviderConfig{}, ErrConflict
	}
	if err := json.Unmarshal(scopesJSON, &cfg.Scopes); err != nil {
		return rawOAuthProviderConfig{}, ErrConflict
	}
	cfg.SecretConfigured = len(ciphertext) > 0 && keyID.Valid && strings.TrimSpace(keyID.String) != ""
	cfg.Configured = cfg.ClientID != "" && cfg.SecretConfigured && cfg.AuthorizationURL != "" && cfg.TokenURL != "" && cfg.RedirectURI != ""
	return rawOAuthProviderConfig{safe: cfg, ciphertext: append([]byte(nil), ciphertext...), keyID: keyID}, nil
}

func buildAuthorizationURL(cfg OAuthProviderConfig, state, challenge string) (string, error) {
	parsed, err := url.Parse(cfg.AuthorizationURL)
	if err != nil {
		return "", ErrInvalid
	}
	query := parsed.Query()
	query.Set("client_id", cfg.ClientID)
	query.Set("redirect_uri", cfg.RedirectURI)
	query.Set("response_type", "code")
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	if len(cfg.Scopes) > 0 {
		query.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func validOAuthIntent(intent string) bool {
	switch intent {
	case OAuthIntentLogin, OAuthIntentRegister, OAuthIntentBind:
		return true
	default:
		return false
	}
}

func postOAuthPath(intent string) string {
	switch intent {
	case OAuthIntentBind:
		return "/app/settings/connected-accounts"
	case OAuthIntentRegister:
		return "/social-registration"
	default:
		return "/app"
	}
}

func validateProviderURLs(provider, authorizationURL, tokenURL, userInfoURL, redirectURI string) error {
	for _, value := range []string{authorizationURL, tokenURL, userInfoURL, redirectURI} {
		if _, err := reviewedHTTPSURL(value); err != nil {
			return err
		}
	}
	redirect, _ := url.Parse(strings.TrimSpace(redirectURI))
	if redirect.Path != "/oauth/"+provider+"/callback" {
		return ErrInvalid
	}
	return nil
}

func reviewedHTTPSURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, ErrInvalid
	}
	return parsed, nil
}

func normalizeScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 || len(scopes) > 32 {
		return nil, ErrInvalid
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || len(scope) > 128 || strings.ContainsAny(scope, "\r\n\t") {
			return nil, ErrInvalid
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	if len(out) == 0 {
		return nil, ErrInvalid
	}
	return out, nil
}
