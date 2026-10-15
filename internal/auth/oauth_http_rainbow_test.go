package auth

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func rainbowFixtureConfig() OAuthProviderConfig {
	return OAuthProviderConfig{Provider: ProviderRainbow, ClientID: "fixture-app", AuthorizationURL: RainbowEndpoint, TokenURL: RainbowEndpoint, UserInfoURL: RainbowEndpoint, RedirectURI: "https://site.p20.test/oauth/rainbow/callback", Scopes: []string{"qq"}}
}

func rainbowResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestHTTPProviderAdapterRainbowProtocol(t *testing.T) {
	cfg := rainbowFixtureConfig()
	state, err := NewOpaqueSecret("gos_rb_qq_", 32)
	if err != nil {
		t.Fatal("state creation failed")
	}
	adapter := NewHTTPProviderAdapter()
	calls := 0
	adapter.client.Transport = oauthRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Scheme != "https" || r.URL.Host != "auth.idcli.com" || r.URL.Path != "/connect.php" {
			t.Fatal("unapproved aggregate endpoint")
		}
		q := r.URL.Query()
		if q.Get("appid") != cfg.ClientID || q.Get("appkey") != "fixture-private-key" || q.Get("type") != "qq" {
			t.Fatal("aggregate application authority mismatch")
		}
		if q.Get("act") == "login" {
			redirect, err := url.Parse(q.Get("redirect_uri"))
			if err != nil || redirect.Host != "site.p20.test" || redirect.Path != "/oauth/rainbow/callback" || redirect.Query().Get("state") != state.Value {
				t.Fatal("aggregate redirect lost GoJet state")
			}
			return rainbowResponse(`{"code":0,"type":"qq","url":"https://graph.qq.com/oauth2.0/authorize?opaque=fixture"}`), nil
		}
		if q.Get("act") != "callback" || q.Get("code") != "fixture-code" {
			t.Fatal("unexpected aggregate exchange")
		}
		return rainbowResponse(`{"code":0,"type":"qq","social_uid":"subject","nickname":"Fixture","access_token":"never-store-token","email":"untrusted@example.test"}`), nil
	})
	location, err := adapter.rainbowAuthorization(context.Background(), cfg, "fixture-private-key", state.Value)
	if err != nil || !strings.HasPrefix(location, "https://graph.qq.com/") || strings.Contains(location, "fixture-private-key") {
		t.Fatal("server-side aggregate bootstrap failed")
	}
	claim, err := adapter.Exchange(context.Background(), OAuthProviderExchangeRequest{Provider: ProviderRainbow, ProviderType: "qq", TokenURL: RainbowEndpoint, UserInfoURL: RainbowEndpoint, ClientID: cfg.ClientID, ClientSecret: "fixture-private-key", Code: "fixture-code", PKCEVerifier: "local-verifier", RedirectURI: cfg.RedirectURI})
	if err != nil || calls != 2 || claim.Subject != "qq\x00subject" || claim.Email != "" || claim.EmailVerified {
		t.Fatal("aggregate identity mapping failed")
	}
}

func TestHTTPProviderAdapterRainbowRejectsUnsafeResponses(t *testing.T) {
	cfg := rainbowFixtureConfig()
	state, _ := NewOpaqueSecret("gos_rb_qq_", 32)
	for _, body := range []string{
		`{"type":"qq","url":"https://graph.qq.com/authorize"}`,
		`{"code":2,"type":"qq","url":"https://graph.qq.com/authorize"}`,
		`{"code":0,"type":"wx","url":"https://graph.qq.com/authorize"}`,
		`{"code":0,"type":"qq","url":"javascript:alert(1)"}`,
		`{"code":0,"type":"qq","url":"http://graph.qq.com/authorize"}`,
		`{"code":0,"type":"qq","url":"https://user:pass@graph.qq.com/authorize"}`,
		`{"code":0,"type":"qq","url":"https://graph.qq.com/authorize?appkey=redacted"}`,
		`{"code":0,"type":"qq","url":"https://graph.qq.com/authorize?echo=fixture%2Dprivate%2Dkey"}`,
	} {
		a := NewHTTPProviderAdapter()
		a.client.Transport = oauthRoundTrip(func(*http.Request) (*http.Response, error) { return rainbowResponse(body), nil })
		if location, err := a.rainbowAuthorization(context.Background(), cfg, "fixture-private-key", state.Value); err == nil || location != "" {
			t.Fatal("unsafe aggregate bootstrap accepted")
		}
	}
	for _, body := range []string{
		`{"type":"qq","social_uid":"subject"}`, `{"code":2,"type":"qq","social_uid":"subject"}`,
		`{"code":0,"type":"wx","social_uid":"subject"}`, `{"code":0,"type":"qq","social_uid":""}`,
	} {
		a := NewHTTPProviderAdapter()
		a.client.Transport = oauthRoundTrip(func(*http.Request) (*http.Response, error) { return rainbowResponse(body), nil })
		if claim, err := a.Exchange(context.Background(), OAuthProviderExchangeRequest{Provider: ProviderRainbow, ProviderType: "qq", TokenURL: RainbowEndpoint, UserInfoURL: RainbowEndpoint, ClientID: "app", ClientSecret: "secret", Code: "code", PKCEVerifier: "verifier"}); err == nil || claim.Subject != "" {
			t.Fatal("unverified aggregate identity accepted")
		}
	}
	if validRainbowConfig(RainbowEndpoint, "https://untrusted.test/connect.php", RainbowEndpoint, []string{"qq"}) || validRainbowConfig(RainbowEndpoint, RainbowEndpoint, RainbowEndpoint, []string{"linkedin"}) || rainbowStateType("gos_rb_qq_short") != "" {
		t.Fatal("invalid aggregate authority accepted")
	}
}

func TestHTTPProviderAdapterRainbowDurableState(t *testing.T) {
	if os.Getenv("GOJET_P20_OAUTH_BROWSER_PROBE") != "1" {
		t.Skip("requires isolated migrated MySQL; upstream HTTP responses remain protocol fixtures")
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal("aggregate persistence probe failed")
		}
	}
	db, err := sql.Open("mysql", os.Getenv("GOJET_MYSQL_DSN"))
	must(err)
	defer db.Close()
	crypto, err := NewOAuthCrypto("p20-rainbow-fixture", bytes.Repeat([]byte{0x41}, 32))
	must(err)
	ciphertext, err := crypto.Encrypt("fixture-private-key", "oauth_client_secret:rainbow")
	must(err)
	cfg := rainbowFixtureConfig()
	_, err = db.Exec(`UPDATE oauth_provider_configs SET enabled=1,client_id=?,client_secret_ciphertext=?,secret_key_id=?,authorization_url=?,token_url=?,userinfo_url=?,redirect_uri=?,scopes_json=JSON_ARRAY('qq'),version=version+1 WHERE provider='rainbow'`, cfg.ClientID, ciphertext, crypto.KeyID(), RainbowEndpoint, RainbowEndpoint, RainbowEndpoint, cfg.RedirectURI)
	must(err)
	s, err := NewOAuthService(db, crypto, 10*time.Minute)
	must(err)
	a := NewHTTPProviderAdapter()
	calls := 0
	a.client.Transport = oauthRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("type") != "qq" {
			t.Fatal("state-bound aggregate channel lost")
		}
		if r.URL.Query().Get("act") == "login" {
			return rainbowResponse(`{"code":0,"type":"qq","url":"https://graph.qq.com/oauth2.0/authorize"}`), nil
		}
		return rainbowResponse(`{"code":0,"type":"qq","social_uid":"durable-subject","nickname":"Fixture"}`), nil
	})
	ctx := context.Background()
	now := time.Now().UTC()
	start, err := s.StartWithHTTPProvider(ctx, a, OAuthStartInput{Provider: ProviderRainbow, Intent: OAuthIntentLogin, CorrelationID: "p20-rainbow-start"}, now)
	must(err)
	input := OAuthCallbackInput{Provider: ProviderRainbow, State: strings.Replace(start.State, "gos_rb_qq_", "gos_rb_wx_", 1), Code: "fixture-code", CorrelationID: "p20-rainbow-callback"}
	if _, err := s.Callback(ctx, a, input, now); err == nil || calls != 1 {
		t.Fatal("tampered aggregate channel reached exchange")
	}
	input.State = start.State
	_, err = db.Exec(`UPDATE oauth_provider_configs SET scopes_json=JSON_ARRAY('wx') WHERE provider='rainbow'`)
	must(err)
	if _, err := s.Callback(ctx, a, input, now); err == nil || calls != 1 {
		t.Fatal("configuration channel change accepted old state")
	}
	_, err = db.Exec(`UPDATE oauth_provider_configs SET scopes_json=JSON_ARRAY('qq') WHERE provider='rainbow'`)
	must(err)
	callback, err := s.Callback(ctx, a, input, now)
	must(err)
	if callback.ProviderSubject != "qq\x00durable-subject" || calls != 2 {
		t.Fatal("aggregate callback identity mismatch")
	}
	if _, err := s.Callback(ctx, a, input, now); err != ErrReplay || calls != 2 {
		t.Fatal("aggregate state replay reached upstream")
	}
}
