package auth

import (
 "context"
 "io"
 "net/http"
 "strings"
 "testing"
)

type oauthRoundTrip func(*http.Request) (*http.Response, error)
func (f oauthRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHTTPProviderAdapterGitHubProtocol(t *testing.T) {
 calls := 0
 a := NewHTTPProviderAdapter()
 a.client.Transport = oauthRoundTrip(func(r *http.Request) (*http.Response, error) {
  calls++
  body := `{"access_token":"test-access-token","token_type":"bearer"}`
  switch calls {
  case 1:
   if r.Method != "POST" || r.URL.String() != "https://github.com/login/oauth/access_token" { t.Fatal("unexpected token endpoint") }
   if err := r.ParseForm(); err != nil { t.Fatal(err) }
   for k,v := range map[string]string{"code":"code", "client_secret":"secret", "code_verifier":"verifier", "redirect_uri":"https://gojet.test/oauth/github/callback"} {
    if r.Form.Get(k) != v { t.Fatalf("missing exchange field %s",k) }
   }
  case 2:
   if r.URL.String() != "https://api.github.com/user" || r.Header.Get("Authorization") != "Bearer test-access-token" { t.Fatal("identity request not bound to token") }
   body = `{"id":123,"login":"user","email":"untrusted@example.test"}`
  default: t.Fatal("unexpected extra request")
  }
  return &http.Response{StatusCode:200,Body:io.NopCloser(strings.NewReader(body)),Header:make(http.Header)},nil
 })
 claim,err := a.Exchange(context.Background(),githubExchangeFixture())
 if err != nil || calls != 2 || claim.Subject != "123" || claim.Email != "" || claim.EmailVerified { t.Fatal("unsafe identity mapping") }
}

func githubExchangeFixture() OAuthProviderExchangeRequest {
 return OAuthProviderExchangeRequest{Provider:ProviderGitHub, TokenURL:"https://github.com/login/oauth/access_token",UserInfoURL:"https://api.github.com/user",Code:"code",ClientID:"id",ClientSecret:"secret",RedirectURI:"https://gojet.test/oauth/github/callback",PKCEVerifier:"verifier"}
}

func TestHTTPProviderAdapterRejectsUnsafeConfiguration(t *testing.T) {
 for _, change := range []func(*OAuthProviderExchangeRequest){
  func(r *OAuthProviderExchangeRequest){r.TokenURL="https://attacker.test/token"},
  func(r *OAuthProviderExchangeRequest){r.UserInfoURL="http://127.0.0.1/user"},
  func(r *OAuthProviderExchangeRequest){r.Provider=ProviderGoogle},
  func(r *OAuthProviderExchangeRequest){r.PKCEVerifier=""},
 } {
  a:=NewHTTPProviderAdapter()
  a.client.Transport=oauthRoundTrip(func(*http.Request)(*http.Response,error){t.Fatal("unsafe outbound request");return nil,nil})
  input:=githubExchangeFixture();change(&input)
  if _,err:=a.Exchange(context.Background(),input);err==nil {t.Fatal("accepted unsafe configuration")}
 }
}

func TestHTTPProviderAdapterFailsClosedOnProviderResponse(t *testing.T) {
 for _,tc:=range []struct{status int;body string}{
  {302,`{"access_token":"secret","token_type":"bearer"}`},
  {500,"provider secret"}, {200,"not json"},
  {200,`{"error":"bad_verification_code","access_token":"secret","token_type":"bearer"}`},
  {200,`{"access_token":"secret","token_type":"unknown"}`},
  {200,strings.Repeat("x",(1<<20)+1)},
 } {
  a:=NewHTTPProviderAdapter();calls:=0
  a.client.Transport=oauthRoundTrip(func(*http.Request)(*http.Response,error){calls++;return &http.Response{StatusCode:tc.status,Body:io.NopCloser(strings.NewReader(tc.body)),Header:make(http.Header)},nil})
  claim,err:=a.Exchange(context.Background(),githubExchangeFixture())
  if err!=ErrForbidden || calls!=1 || claim.Subject!="" {t.Fatal("provider failure leaked or authenticated")}
 }
 a:=NewHTTPProviderAdapter()
 if a.client.Timeout<=0 || a.client.CheckRedirect(&http.Request{},nil)!=http.ErrUseLastResponse {t.Fatal("missing network bounds")}
}
