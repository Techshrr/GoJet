package auth

import (
	"context"
	"net/url"
	"strings"
)

// exchangeWeChat implements the website authorization-code wire protocol.
// OpenID is application-scoped; never silently substitute an optional UnionID.
func (a *HTTPProviderAdapter) exchangeWeChat(ctx context.Context, input OAuthProviderExchangeRequest) (OAuthProviderClaim, error) {
	denied := OAuthProviderClaim{}
	var token struct {
		AccessToken string `json:"access_token"`
		OpenID      string `json:"openid"`
		ErrCode     int    `json:"errcode"`
	}
	if a.readQueryJSON(ctx, input.TokenURL, url.Values{
		"appid": {input.ClientID}, "secret": {input.ClientSecret},
		"code": {input.Code}, "grant_type": {"authorization_code"},
	}, &token) != nil || token.ErrCode != 0 || token.AccessToken == "" || strings.TrimSpace(token.OpenID) == "" {
		return denied, ErrForbidden
	}
	var profile struct {
		OpenID   string `json:"openid"`
		Nickname string `json:"nickname"`
		ErrCode  int    `json:"errcode"`
	}
	if a.readQueryJSON(ctx, input.UserInfoURL, url.Values{
		"access_token": {token.AccessToken}, "openid": {token.OpenID}, "lang": {"zh_CN"},
	}, &profile) != nil || profile.ErrCode != 0 || profile.OpenID != token.OpenID {
		return denied, ErrForbidden
	}
	return OAuthProviderClaim{Subject: token.OpenID, DisplayName: profile.Nickname}, nil
}
