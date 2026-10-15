package auth

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestHTTPProviderAdapterQQProtocol(t *testing.T) {
	for _, wrongClient := range []bool{false, true} {
		a := NewHTTPProviderAdapter()
		calls := 0
		a.client.Transport = oauthRoundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.Method != http.MethodGet || r.URL.Scheme != "https" || r.URL.Host != "graph.qq.com" {
				t.Fatal("unexpected QQ request authority")
			}
			q := r.URL.Query()
			body := ""
			switch calls {
			case 1:
				if r.URL.Path != "/oauth2.0/token" || q.Get("fmt") != "json" || q.Get("client_secret") != "secret" || q.Get("code") != "code" || q.Get("redirect_uri") != "https://gojet.test/oauth/qq/callback" {
					t.Fatal("incorrect QQ token binding")
				}
				body = `{"access_token":"qq-token"}`
			case 2:
				if r.URL.Path != "/oauth2.0/me" || q.Get("access_token") != "qq-token" || q.Get("fmt") != "json" {
					t.Fatal("incorrect QQ OpenID binding")
				}
				body = `{"client_id":"id","openid":"qq-subject"}`
				if wrongClient {
					body = `{"client_id":"foreign-app","openid":"qq-subject"}`
				}
			case 3:
				if wrongClient || r.URL.Path != "/user/get_user_info" || q.Get("openid") != "qq-subject" || q.Get("oauth_consumer_key") != "id" || q.Get("access_token") != "qq-token" {
					t.Fatal("incorrect QQ profile binding")
				}
				body = `{"ret":0,"nickname":"Person"}`
			default:
				t.Fatal("unexpected extra QQ request")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})
		input := githubExchangeFixture()
		input.Provider = ProviderQQ
		input.TokenURL = "https://graph.qq.com/oauth2.0/token"
		input.UserInfoURL = "https://graph.qq.com/user/get_user_info"
		input.RedirectURI = "https://gojet.test/oauth/qq/callback"
		claim, err := a.Exchange(context.Background(), input)
		if wrongClient {
			if err != ErrForbidden || calls != 2 || claim.Subject != "" {
				t.Fatal("foreign application identity accepted")
			}
		} else if err != nil || calls != 3 || claim.Subject != "qq-subject" || claim.Email != "" || claim.EmailVerified {
			t.Fatal("incorrect QQ identity mapping")
		}
	}
}

func TestQQAuthorizationScopeSeparator(t *testing.T) {
	raw, err := buildAuthorizationURL(OAuthProviderConfig{Provider: ProviderQQ, AuthorizationURL: "https://graph.qq.com/oauth2.0/authorize", Scopes: []string{"get_user_info", "list_album"}}, "state", "challenge")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Query().Get("scope") != "get_user_info,list_album" || u.Query().Get("state") != "state" {
		t.Fatal("invalid QQ authorization parameters")
	}
}

func TestHTTPProviderAdapterQQRejectsMalformedResponses(t *testing.T) {
	for _, tc := range []struct {
		stage int
		body  string
	}{
		{1, `{"error":100,"access_token":"token"}`},
		{1, `{}`},
		{2, `callback({"client_id":"id","openid":"subject"})`},
		{2, `{"client_id":"id","openid":""}`},
		{3, `{"nickname":"Person"}`},
		{3, `{"ret":1,"nickname":"Person"}`},
	} {
		a := NewHTTPProviderAdapter()
		calls := 0
		a.client.Transport = oauthRoundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			body := `{"access_token":"token"}`
			if calls == 2 {
				body = `{"client_id":"id","openid":"subject"}`
			}
			if calls == tc.stage {
				body = tc.body
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})
		input := githubExchangeFixture()
		input.Provider = ProviderQQ
		input.TokenURL = "https://graph.qq.com/oauth2.0/token"
		input.UserInfoURL = "https://graph.qq.com/user/get_user_info"
		claim, err := a.Exchange(context.Background(), input)
		if err != ErrForbidden || claim.Subject != "" || calls != tc.stage {
			t.Fatal("malformed QQ response did not fail closed")
		}
	}
}
