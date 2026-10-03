package auth

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"
	"time"
)

const RainbowEndpoint = "https://auth.idcli.com/connect.php"

func validRainbowType(value string) bool {
	switch value {
	case "qq", "wx", "alipay", "baidu", "huawei", "google", "facebook", "twitter", "dingtalk", "gitee", "github":
		return true
	default:
		return false
	}
}

// The random state's prefix identifies the aggregate channel. The full state is
// hashed in oauth_states, so changing this public context invalidates the state.
func rainbowStateType(state string) string {
	parts := strings.SplitN(state, "_", 4)
	if len(parts) != 4 || parts[0] != "gos" || parts[1] != "rb" || !validRainbowType(parts[2]) {
		return ""
	}
	entropy, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(entropy) != 32 {
		return ""
	}
	return parts[2]
}

func validRainbowConfig(authorizationURL, tokenURL, userInfoURL string, scopes []string) bool {
	return authorizationURL == RainbowEndpoint && tokenURL == RainbowEndpoint && userInfoURL == RainbowEndpoint && len(scopes) == 1 && validRainbowType(scopes[0])
}

// StartWithHTTPProvider performs Rainbow's server-to-server bootstrap. For
// ordinary OAuth providers Start already returns the authorization URL.
func (s *OAuthService) StartWithHTTPProvider(ctx context.Context, adapter *HTTPProviderAdapter, input OAuthStartInput, now time.Time) (OAuthStartResult, error) {
	input.Provider = strings.TrimSpace(input.Provider)
	start, err := s.Start(ctx, input, now)
	if err != nil || input.Provider != ProviderRainbow {
		return start, err
	}
	raw, err := s.loadRawProviderConfig(ctx, ProviderRainbow)
	if err != nil || !raw.safe.Enabled || !raw.safe.Configured || rainbowStateType(start.State) != raw.safe.Scopes[0] {
		return OAuthStartResult{}, ErrForbidden
	}
	secret, err := s.crypto.Decrypt(raw.ciphertext, raw.keyID.String, "oauth_client_secret:rainbow")
	if err != nil {
		return OAuthStartResult{}, ErrForbidden
	}
	location, err := adapter.rainbowAuthorization(ctx, raw.safe, secret, start.State)
	if err != nil {
		return OAuthStartResult{}, ErrForbidden
	}
	start.AuthorizationURL = location
	return start, nil
}

func (a *HTTPProviderAdapter) rainbowAuthorization(ctx context.Context, cfg OAuthProviderConfig, secret, state string) (string, error) {
	channel := rainbowStateType(state)
	if a == nil || a.client == nil || secret == "" || cfg.ClientID == "" || !validRainbowConfig(cfg.AuthorizationURL, cfg.TokenURL, cfg.UserInfoURL, cfg.Scopes) || channel != cfg.Scopes[0] {
		return "", ErrForbidden
	}
	if validateProviderURLs(ProviderRainbow, cfg.AuthorizationURL, cfg.TokenURL, cfg.UserInfoURL, cfg.RedirectURI) != nil {
		return "", ErrForbidden
	}
	redirect, _ := url.Parse(cfg.RedirectURI)
	query := redirect.Query()
	query.Set("state", state)
	redirect.RawQuery = query.Encode()
	var response struct {
		Code *int   `json:"code"`
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	if a.readQueryJSON(ctx, RainbowEndpoint, url.Values{"act": {"login"}, "appid": {cfg.ClientID}, "appkey": {secret}, "type": {channel}, "redirect_uri": {redirect.String()}}, &response) != nil || response.Code == nil || *response.Code != 0 || response.Type != channel {
		return "", ErrForbidden
	}
	location, err := reviewedHTTPSURL(response.URL)
	if err != nil {
		return "", ErrForbidden
	}
	decoded, err := url.QueryUnescape(response.URL)
	if err != nil || strings.Contains(decoded, secret) {
		return "", ErrForbidden
	}
	for key := range location.Query() {
		if strings.EqualFold(key, "appkey") || strings.EqualFold(key, "client_secret") {
			return "", ErrForbidden
		}
	}
	return location.String(), nil
}

func (a *HTTPProviderAdapter) exchangeRainbow(ctx context.Context, input OAuthProviderExchangeRequest) (OAuthProviderClaim, error) {
	if !validRainbowType(input.ProviderType) {
		return OAuthProviderClaim{}, ErrForbidden
	}
	var response struct {
		Code      *int   `json:"code"`
		Type      string `json:"type"`
		Subject   string `json:"social_uid"`
		Nickname  string `json:"nickname"`
	}
	if a.readQueryJSON(ctx, RainbowEndpoint, url.Values{"act": {"callback"}, "appid": {input.ClientID}, "appkey": {input.ClientSecret}, "type": {input.ProviderType}, "code": {input.Code}}, &response) != nil || response.Code == nil || *response.Code != 0 || response.Type != input.ProviderType {
		return OAuthProviderClaim{}, ErrForbidden
	}
	subject := strings.TrimSpace(response.Subject)
	if subject == "" || len(subject) > 900 || strings.ContainsRune(subject, '\x00') || len(response.Nickname) > 255 {
		return OAuthProviderClaim{}, ErrForbidden
	}
	// Aggregate identities are separate from direct providers and other channels.
	// HULINK does not attest email ownership; retain GoJet email verification.
	return OAuthProviderClaim{Subject: input.ProviderType + "\x00" + subject, DisplayName: response.Nickname}, nil
}
