package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// Optional direct providers still consume the same durable state, PKCE, handoff
// and connected-account authorities as the original providers.
func (a *HTTPProviderAdapter) exchangeOptionalDirect(ctx context.Context, input OAuthProviderExchangeRequest) (OAuthProviderClaim, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {input.ClientID}, "code": {input.Code}, "redirect_uri": {input.RedirectURI}, "code_verifier": {input.PKCEVerifier}}
	if input.Provider == ProviderLinkedIn {
		form.Set("client_secret", input.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, input.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthProviderClaim{}, ErrForbidden
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if input.Provider == ProviderX {
		req.SetBasicAuth(url.QueryEscape(input.ClientID), url.QueryEscape(input.ClientSecret))
	}
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
	}
	if a.read(req, &token) != nil || token.AccessToken == "" || token.Error != "" || (token.TokenType != "" && !strings.EqualFold(token.TokenType, "bearer")) || (input.Provider == ProviderX && token.TokenType == "") {
		return OAuthProviderClaim{}, ErrForbidden
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, input.UserInfoURL, nil)
	if err != nil {
		return OAuthProviderClaim{}, ErrForbidden
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	if input.Provider == ProviderX {
		var profile struct {
			Data struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"data"`
			Errors json.RawMessage `json:"errors"`
		}
		if a.read(req, &profile) != nil || len(profile.Errors) != 0 || profile.Data.ID == "" || len(profile.Data.ID) > 64 || len(profile.Data.Name) > 255 {
			return OAuthProviderClaim{}, ErrForbidden
		}
		for _, digit := range profile.Data.ID {
			if digit < '0' || digit > '9' {
				return OAuthProviderClaim{}, ErrForbidden
			}
		}
		return OAuthProviderClaim{Subject: profile.Data.ID, DisplayName: profile.Data.Name}, nil
	}
	var profile struct {
		Subject       string `json:"sub"`
		Name          string `json:"name"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if a.read(req, &profile) != nil || strings.TrimSpace(profile.Subject) == "" || len(profile.Subject) > 255 || len(profile.Name) > 255 {
		return OAuthProviderClaim{}, ErrForbidden
	}
	// Only the authenticated userinfo response is consumed; unverified ID-token
	// payloads, X profile badges and inferred email ownership are never trusted.
	return OAuthProviderClaim{Subject: profile.Subject, DisplayName: profile.Name, Email: profile.Email, EmailVerified: profile.Email != "" && profile.EmailVerified}, nil
}

func validOptionalDirectConfig(cfg OAuthProviderConfig) bool {
	var required, allowed []string
	switch cfg.Provider {
	case ProviderX:
		if cfg.AuthorizationURL != "https://x.com/i/oauth2/authorize" || cfg.TokenURL != "https://api.x.com/2/oauth2/token" || cfg.UserInfoURL != "https://api.x.com/2/users/me" {
			return false
		}
		required = []string{"tweet.read", "users.read"}
		allowed = required
	case ProviderLinkedIn:
		if cfg.AuthorizationURL != "https://www.linkedin.com/oauth/v2/authorization" || cfg.TokenURL != "https://www.linkedin.com/oauth/v2/accessToken" || cfg.UserInfoURL != "https://api.linkedin.com/v2/userinfo" {
			return false
		}
		required = []string{"openid", "profile"}
		allowed = []string{"openid", "profile", "email"}
	default:
		return true
	}
	seen := map[string]bool{}
	for _, scope := range cfg.Scopes {
		accepted := false
		for _, candidate := range allowed {
			accepted = accepted || scope == candidate
		}
		if !accepted {
			return false
		}
		seen[scope] = true
	}
	for _, scope := range required {
		if !seen[scope] {
			return false
		}
	}
	return true
}
