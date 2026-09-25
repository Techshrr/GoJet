package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var facebookTokenEndpoint = regexp.MustCompile(`^https://graph\.facebook\.com/(v[1-9][0-9]*\.0)/oauth/access_token$`)

func facebookEndpoints(input OAuthProviderExchangeRequest) bool {
	match := facebookTokenEndpoint.FindStringSubmatch(input.TokenURL)
	return len(match) == 2 && input.UserInfoURL == "https://graph.facebook.com/"+match[1]+"/me"
}

func (a *HTTPProviderAdapter) exchangeFacebook(ctx context.Context, input OAuthProviderExchangeRequest) (OAuthProviderClaim, error) {
	denied := OAuthProviderClaim{}
	params := url.Values{
		"client_id": {input.ClientID}, "client_secret": {input.ClientSecret},
		"redirect_uri": {input.RedirectURI}, "code": {input.Code},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, input.TokenURL+"?"+params.Encode(), nil)
	if err != nil {
		return denied, ErrForbidden
	}
	var token struct {
		AccessToken string          `json:"access_token"`
		Error       json.RawMessage `json:"error"`
	}
	if a.read(req, &token) != nil || token.AccessToken == "" || len(token.Error) != 0 {
		return denied, ErrForbidden
	}
	mac := hmac.New(sha256.New, []byte(input.ClientSecret))
	mac.Write([]byte(token.AccessToken))
	params = url.Values{"fields": {"id,name"}, "appsecret_proof": {hex.EncodeToString(mac.Sum(nil))}}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, input.UserInfoURL+"?"+params.Encode(), nil)
	if err != nil {
		return denied, ErrForbidden
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	var profile struct {
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Error json.RawMessage `json:"error"`
	}
	if a.read(req, &profile) != nil || len(profile.Error) != 0 || strings.TrimSpace(profile.ID) == "" {
		return denied, ErrForbidden
	}
	// Do not infer email verification from the Facebook profile or account flags.
	return OAuthProviderClaim{Subject: profile.ID, DisplayName: profile.Name}, nil
}
