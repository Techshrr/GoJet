package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// GoogleOneTapClientID exposes only the public client ID when both independent
// server-side switches are enabled. The P17 settings authority owns the switch.
func (s *OAuthService) GoogleOneTapClientID(ctx context.Context) (string, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT value_json FROM admin_platform_settings WHERE setting_key='google_one_tap'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) { return "", nil }
	if err != nil { return "", err }
	var config map[string]string
	if json.Unmarshal(raw, &config) != nil { return "", ErrInvalid }
	if config["enabled"] != "true" { return "", nil }
	provider, err := s.getProviderConfig(ctx, ProviderGoogle)
	if err != nil { return "", err }
	if !provider.Enabled || !provider.Configured { return "", nil }
	return provider.ClientID, nil
}

type googleOneTapExchange struct {
	verifier *GoogleOneTapVerifier
	now time.Time
}

func (a googleOneTapExchange) Exchange(ctx context.Context, input OAuthProviderExchangeRequest) (OAuthProviderClaim, error) {
	if input.Provider != ProviderGoogle { return OAuthProviderClaim{}, ErrForbidden }
	return a.verifier.Verify(ctx, input.Code, input.ClientID, input.PKCEVerifier, a.now)
}

// CompleteGoogleOneTap reuses the locked state transaction, replay rejection,
// audit and handoff authority. The HTTP caller must verify browser binding first.
func (s *OAuthService) CompleteGoogleOneTap(ctx context.Context, verifier *GoogleOneTapVerifier, state, credential, correlation string, now time.Time) (OAuthBrowserHandoff, error) {
	clientID, err := s.GoogleOneTapClientID(ctx)
	if err != nil || clientID == "" { return OAuthBrowserHandoff{}, ErrForbidden }
	callback, err := s.Callback(ctx, googleOneTapExchange{verifier: verifier, now: now}, OAuthCallbackInput{
		Provider: ProviderGoogle, State: state, Code: credential, CorrelationID: correlation,
	}, now)
	if err != nil { return OAuthBrowserHandoff{}, err }
	return s.CreateBrowserHandoff(ctx, callback, correlation+"-handoff", now)
}
