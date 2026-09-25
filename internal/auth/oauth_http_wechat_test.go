package auth

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestHTTPProviderAdapterWeChatProtocol(t *testing.T) {
	for _, body := range []string{
		`{"openid":"subject","nickname":"Person","unionid":"different-namespace"}`,
		`{"openid":"foreign-subject","nickname":"Person"}`,
		`{"errcode":40003,"openid":"subject"}`,
		`{}`,
	} {
		a := NewHTTPProviderAdapter()
		calls := 0
		a.client.Transport = oauthRoundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			q := r.URL.Query()
			response := body
			if r.Method != http.MethodGet || r.URL.Scheme != "https" || r.URL.Host != "api.weixin.qq.com" {
				t.Fatal("wrong WeChat authority")
			}
			if calls == 1 {
				if r.URL.Path != "/sns/oauth2/access_token" || q.Get("appid") != "id" || q.Get("secret") != "secret" || q.Get("code") != "code" || q.Get("grant_type") != "authorization_code" {
					t.Fatal("incorrect WeChat code exchange")
				}
				response = `{"access_token":"wechat-token","openid":"subject"}`
			} else if calls == 2 {
				if r.URL.Path != "/sns/userinfo" || q.Get("openid") != "subject" || q.Get("access_token") != "wechat-token" || q.Get("secret") != "" {
					t.Fatal("incorrect WeChat profile binding")
				}
			} else {
				t.Fatal("unexpected request")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header)}, nil
		})
		input := githubExchangeFixture()
		input.Provider = ProviderWeChat
		input.TokenURL = "https://api.weixin.qq.com/sns/oauth2/access_token"
		input.UserInfoURL = "https://api.weixin.qq.com/sns/userinfo"
		claim, err := a.Exchange(context.Background(), input)
		if strings.Contains(body, "unionid") {
			if err != nil || claim.Subject != "subject" || claim.Email != "" || claim.EmailVerified || calls != 2 {
				t.Fatal("incorrect WeChat identity mapping")
			}
		} else if err != ErrForbidden || claim.Subject != "" || calls != 2 {
			t.Fatal("invalid WeChat profile accepted")
		}
	}
}

func TestWeChatAuthorizationParameters(t *testing.T) {
	raw, err := buildAuthorizationURL(OAuthProviderConfig{Provider: ProviderWeChat, ClientID: "app", AuthorizationURL: "https://open.weixin.qq.com/connect/qrconnect", RedirectURI: "https://gojet.test/oauth/wechat/callback"}, "state", "challenge")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("appid") != "app" || q.Get("client_id") != "" || q.Get("state") != "state" || q.Get("scope") != "snsapi_login" || q.Get("code_challenge") != "" || u.Fragment != "wechat_redirect" {
		t.Fatal("incorrect WeChat authorization parameters")
	}
}
