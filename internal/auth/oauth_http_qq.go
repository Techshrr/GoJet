package auth

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// exchangeQQ follows Tencent's server-side flow. Provider-required query
// credentials stay inside this adapter; request URLs/errors are never returned.
func (a *HTTPProviderAdapter) exchangeQQ(ctx context.Context, input OAuthProviderExchangeRequest) (OAuthProviderClaim, error) {
	denied := OAuthProviderClaim{}
	var token struct {
		AccessToken string          `json:"access_token"`
		Error       json.RawMessage `json:"error"`
	}
	params := url.Values{
		"grant_type": {"authorization_code"}, "client_id": {input.ClientID},
		"client_secret": {input.ClientSecret}, "code": {input.Code},
		"redirect_uri": {input.RedirectURI}, "fmt": {"json"},
	}
	if a.readQueryJSON(ctx, input.TokenURL, params, &token) != nil || len(token.Error) != 0 || token.AccessToken == "" {
		return denied, ErrForbidden
	}
	var identity struct {
		ClientID string          `json:"client_id"`
		OpenID   string          `json:"openid"`
		Error    json.RawMessage `json:"error"`
	}
	if a.readQueryJSON(ctx, "https://graph.qq.com/oauth2.0/me", url.Values{
		"access_token": {token.AccessToken}, "fmt": {"json"},
	}, &identity) != nil || len(identity.Error) != 0 || identity.ClientID != input.ClientID || strings.TrimSpace(identity.OpenID) == "" {
		return denied, ErrForbidden
	}
	var profile struct {
		Ret      *int   `json:"ret"`
		Nickname string `json:"nickname"`
	}
	if a.readQueryJSON(ctx, input.UserInfoURL, url.Values{
		"access_token": {token.AccessToken}, "oauth_consumer_key": {input.ClientID},
		"openid": {identity.OpenID},
	}, &profile) != nil || profile.Ret == nil || *profile.Ret != 0 {
		return denied, ErrForbidden
	}
	return OAuthProviderClaim{Subject: identity.OpenID, DisplayName: profile.Nickname}, nil
}
