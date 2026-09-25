package auth

import (
 "context"
 "encoding/json"
 "io"
 "net/http"
 "net/url"
 "strconv"
 "strings"
 "time"
)

// HTTPProviderAdapter exchanges codes only with explicitly supported provider endpoints.
// It never exposes provider response bodies or access tokens to callers.
type HTTPProviderAdapter struct { client *http.Client }

func NewHTTPProviderAdapter() *HTTPProviderAdapter {
 return &HTTPProviderAdapter{client: &http.Client{
  Timeout: 15*time.Second,
  CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
 }}
}

func (a *HTTPProviderAdapter) Exchange(ctx context.Context, input OAuthProviderExchangeRequest) (OAuthProviderClaim, error) {
 denied := OAuthProviderClaim{}
 if a == nil || a.client == nil || !supportedHTTPEndpoints(input) ||
  input.Code == "" || input.ClientID == "" || input.ClientSecret == "" || input.PKCEVerifier == "" {
  return denied, ErrForbidden
 }
 if input.Provider == ProviderWeChat { return a.exchangeWeChat(ctx, input) }
 if input.Provider == ProviderFacebook { return a.exchangeFacebook(ctx, input) }
 if input.Provider == ProviderQQ { return a.exchangeQQ(ctx, input) }
 form := url.Values{"grant_type": {"authorization_code"}, "client_id": {input.ClientID}, "client_secret": {input.ClientSecret},
  "code": {input.Code}, "redirect_uri": {input.RedirectURI}, "code_verifier": {input.PKCEVerifier}}
 req, err := http.NewRequestWithContext(ctx, http.MethodPost, input.TokenURL, strings.NewReader(form.Encode()))
 if err != nil { return denied, ErrForbidden }
 req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
 var token struct { AccessToken string `json:"access_token"`; TokenType string `json:"token_type"`; Error string `json:"error"` }
 if a.read(req, &token) != nil || token.Error != "" || token.AccessToken == "" || !strings.EqualFold(token.TokenType, "bearer") {
  return denied, ErrForbidden
 }
 req, err = http.NewRequestWithContext(ctx, http.MethodGet, input.UserInfoURL, nil)
 if err != nil { return denied, ErrForbidden }
 req.Header.Set("Authorization", "Bearer " + token.AccessToken)
 if input.Provider == ProviderGoogle {
  var user struct {
   Subject string `json:"sub"`
   Email string `json:"email"`
   EmailVerified bool `json:"email_verified"`
   Name string `json:"name"`
  }
  if a.read(req, &user) != nil || strings.TrimSpace(user.Subject) == "" || len(user.Subject) > 255 {
   return denied, ErrForbidden
  }
  return OAuthProviderClaim{Subject: user.Subject, Email: user.Email,
   EmailVerified: user.EmailVerified, DisplayName: user.Name}, nil
 }
 var user struct { ID int64 `json:"id"`; Name string `json:"name"`; Login string `json:"login"` }
 if a.read(req, &user) != nil || user.ID <= 0 { return denied, ErrForbidden }
 name := user.Name
 if name == "" { name = user.Login }
 // A public profile email is not proof of verification. Missing email takes
 // the existing social-registration verification path; never auto-link it.
 return OAuthProviderClaim{Subject: strconv.FormatInt(user.ID, 10), DisplayName: name}, nil
}

func (a *HTTPProviderAdapter) read(req *http.Request, out any) error {
 req.Header.Set("Accept", "application/json")
 req.Header.Set("User-Agent", "GoJet-OAuth")
 response, err := a.client.Do(req)
 if err != nil { return ErrForbidden }
 defer response.Body.Close()
 if response.StatusCode != http.StatusOK { return ErrForbidden }
 const maxBody = 1 << 20
 body, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
 if err != nil || len(body) > maxBody || json.Unmarshal(body, out) != nil { return ErrForbidden }
 return nil
}

func supportedHTTPEndpoints(input OAuthProviderExchangeRequest) bool {
 switch input.Provider {
 case ProviderGitHub:
  return input.TokenURL == "https://github.com/login/oauth/access_token" && input.UserInfoURL == "https://api.github.com/user"
 case ProviderWeChat:
  return input.TokenURL == "https://api.weixin.qq.com/sns/oauth2/access_token" && input.UserInfoURL == "https://api.weixin.qq.com/sns/userinfo"
 case ProviderFacebook:
  return facebookEndpoints(input)
 case ProviderQQ:
  return input.TokenURL == "https://graph.qq.com/oauth2.0/token" && input.UserInfoURL == "https://graph.qq.com/user/get_user_info"
 case ProviderGoogle:
  return input.TokenURL == "https://oauth2.googleapis.com/token" && input.UserInfoURL == "https://openidconnect.googleapis.com/v1/userinfo"
 default:
  return false
 }
}

func (a *HTTPProviderAdapter) readQueryJSON(ctx context.Context, endpoint string, params url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return ErrForbidden
	}
	return a.read(req, out)
}
