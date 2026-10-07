package auth

import (
	"context"
	"bytes"
	"database/sql"
	"os"
	"time"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func optionalFixture(provider string) OAuthProviderExchangeRequest {
	input := OAuthProviderExchangeRequest{Provider: provider, Code: "fixture-code", ClientID: "fixture-client", ClientSecret: "fixture-secret", PKCEVerifier: "fixture-verifier", RedirectURI: "https://site.p20.test/oauth/" + provider + "/callback"}
	if provider == ProviderX {
		input.TokenURL = "https://api.x.com/2/oauth2/token"
		input.UserInfoURL = "https://api.x.com/2/users/me"
	} else {
		input.TokenURL = "https://www.linkedin.com/oauth/v2/accessToken"
		input.UserInfoURL = "https://api.linkedin.com/v2/userinfo"
	}
	return input
}

func TestHTTPProviderAdapterOptionalDirect(t *testing.T) {
	for _, provider := range []string{ProviderX, ProviderLinkedIn} {
		t.Run(provider, func(t *testing.T) {
			input := optionalFixture(provider)
			a := NewHTTPProviderAdapter()
			calls := 0
			a.client.Transport = oauthRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Fatal("token request body unavailable")
					}
					form, err := url.ParseQuery(string(body))
					if err != nil || r.URL.String() != input.TokenURL || r.Method != "POST" || form.Get("code_verifier") != input.PKCEVerifier || form.Get("redirect_uri") != input.RedirectURI || form.Get("code") != input.Code || form.Get("grant_type") != "authorization_code" {
						t.Fatal("token exchange binding failed")
					}
					if provider == ProviderX {
						id, secret, ok := r.BasicAuth()
						if !ok || id != input.ClientID || secret != input.ClientSecret || form.Has("client_secret") {
							t.Fatal("X confidential-client authentication failed")
						}
					} else if form.Get("client_secret") != input.ClientSecret {
						t.Fatal("LinkedIn application authentication missing")
					}
					return rainbowResponse(`{"access_token":"fixture-access","token_type":"Bearer","id_token":"ignored-unverified-jwt"}`), nil
				}
				if calls != 2 || r.URL.String() != input.UserInfoURL || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer fixture-access" {
					t.Fatal("authenticated userinfo request failed")
				}
				if provider == ProviderX {
					return rainbowResponse(`{"data":{"id":"1234567890123456789","name":"Fixture","verified":true,"email":"untrusted@example.test"}}`), nil
				}
				return rainbowResponse(`{"sub":"linkedin-subject","name":"Fixture","email":"member@example.test","email_verified":true}`), nil
			})
			claim, err := a.Exchange(context.Background(), input)
			if err != nil || calls != 2 || claim.DisplayName != "Fixture" {
				t.Fatal("direct provider exchange failed")
			}
			if provider == ProviderX && (claim.Subject != "1234567890123456789" || claim.EmailVerified || claim.Email != "") {
				t.Fatal("X identity or email authority incorrect")
			}
			if provider == ProviderLinkedIn && (claim.Subject != "linkedin-subject" || !claim.EmailVerified) {
				t.Fatal("LinkedIn userinfo authority incorrect")
			}
		})
	}
}

func TestHTTPProviderAdapterOptionalDirectDenials(t *testing.T) {
	for _, provider := range []string{ProviderX, ProviderLinkedIn} {
		for _, body := range []string{`{}`, `{"access_token":"token","token_type":"MAC"}`, `{"error":"invalid_grant","access_token":"token"}`} {
			a := NewHTTPProviderAdapter()
			calls := 0
			a.client.Transport = oauthRoundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				return rainbowResponse(body), nil
			})
			if _, err := a.Exchange(context.Background(), optionalFixture(provider)); err == nil || calls != 1 {
				t.Fatal("invalid token response accepted")
			}
		}
		for _, body := range []string{`{}`, `{"data":{"id":"not-numeric"}}`, `{"sub":""}`, `{"sub":"identity","email_verified":"true"}`} {
			a := NewHTTPProviderAdapter()
			calls := 0
			a.client.Transport = oauthRoundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return rainbowResponse(`{"access_token":"token","token_type":"bearer"}`), nil
				}
				return rainbowResponse(body), nil
			})
			if _, err := a.Exchange(context.Background(), optionalFixture(provider)); err == nil {
				t.Fatal("invalid userinfo accepted")
			}
		}
		a := NewHTTPProviderAdapter()
		a.client.Transport = oauthRoundTrip(func(*http.Request) (*http.Response, error) {
			t.Fatal("unapproved endpoint reached network")
			return nil, nil
		})
		input := optionalFixture(provider)
		input.TokenURL += "?redirect=untrusted"
		if _, err := a.Exchange(context.Background(), input); err == nil {
			t.Fatal("noncanonical endpoint accepted")
		}
	}
}

func TestHTTPProviderAdapterRuntimeInventory(t *testing.T) {
	want := "google,facebook,github,qq,wechat,rainbow,x,linkedin"
	if strings.Join(RuntimeProviders(), ",") != want || len(Providers) != 6 || !ValidProvider("x") || !ValidProvider("linkedin") || ValidProvider("twitter") {
		t.Fatal("runtime extension or historical inventory drift")
	}
	for _, provider := range []string{ProviderX, ProviderLinkedIn} {
		input := optionalFixture(provider)
		cfg := OAuthProviderConfig{Provider: provider, ClientID: input.ClientID, TokenURL: input.TokenURL, UserInfoURL: input.UserInfoURL, RedirectURI: input.RedirectURI}
		if provider == ProviderX {
			cfg.AuthorizationURL = "https://x.com/i/oauth2/authorize"
			cfg.Scopes = []string{"tweet.read", "users.read"}
		} else {
			cfg.AuthorizationURL = "https://www.linkedin.com/oauth/v2/authorization"
			cfg.Scopes = []string{"openid", "profile", "email"}
		}
		if !validOptionalDirectConfig(cfg) {
			t.Fatal("valid optional configuration rejected")
		}
		location, err := buildAuthorizationURL(cfg, "fixture-state", "fixture-challenge")
		if err != nil {
			t.Fatal("authorization URL failed")
		}
		u, _ := url.Parse(location)
		if u.Query().Get("state") != "fixture-state" || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("code_challenge") != "fixture-challenge" {
			t.Fatal("optional provider state/PKCE missing")
		}
		cfg.Scopes = []string{"unapproved.write"}
		if validOptionalDirectConfig(cfg) {
			t.Fatal("unapproved scope accepted")
		}
	}
}

// Real migrated persistence and callback/handoff authorities; upstream responses
// are explicit protocol fixtures, not evidence of a live provider login.
func TestHTTPProviderAdapterOptionalDurableState(t *testing.T) {
	if os.Getenv("GOJET_P20_OAUTH_BROWSER_PROBE") != "1" {
		t.Skip("requires isolated administrator probe database")
	}
	must := func(err error) { t.Helper(); if err != nil { t.Fatal("optional provider persistence failed:", err) } }
	db, err := sql.Open("mysql", os.Getenv("GOJET_MYSQL_DSN"))
	must(err)
	defer db.Close()
	crypto, err := NewOAuthCrypto("p20-fixture", bytes.Repeat([]byte{0x37}, 32))
	must(err)
	s, err := NewOAuthService(db, crypto, 10*time.Minute)
	must(err)
	for _, provider := range []string{ProviderX, ProviderLinkedIn} {
		ctx := context.Background()
		now := time.Now().UTC()
		a := NewHTTPProviderAdapter()
		calls := 0
		a.client.Transport = oauthRoundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				must(r.ParseForm())
				if r.Form.Get("code_verifier") == "" || r.Form.Get("redirect_uri") != "https://site.p20.test/oauth/"+provider+"/callback" { t.Fatal("durable exchange lost PKCE or redirect binding") }
				return rainbowResponse(`{"access_token":"fixture-access","token_type":"bearer"}`), nil
			}
			if provider == ProviderX { return rainbowResponse(`{"data":{"id":"987654321","name":"Fixture"}}`), nil }
			return rainbowResponse(`{"sub":"durable-linkedin-subject","name":"Fixture"}`), nil
		})
		start, err := s.StartWithHTTPProvider(ctx, a, OAuthStartInput{Provider: provider, Intent: OAuthIntentLogin, CorrelationID: "p20-optional-start-"+provider}, now)
		must(err)
		input := OAuthCallbackInput{Provider: provider, State: start.State+"tampered", Code: "fixture-code", CorrelationID: "p20-optional-callback-"+provider}
		if _, err := s.Callback(ctx, a, input, now); err == nil || calls != 0 { t.Fatal("tampered state reached upstream") }
		input.State = start.State
		callback, err := s.Callback(ctx, a, input, now)
		must(err)
		if callback.Provider != provider || callback.ProviderSubject == "" || calls != 2 { t.Fatal("optional callback identity lost") }
		if _, err := s.Callback(ctx, a, input, now); err != ErrReplay || calls != 2 { t.Fatal("optional state replay reached upstream") }
		_, err = s.CreateBrowserHandoff(ctx, callback, "p20-optional-handoff-"+provider, now)
		must(err)
		var count int
		must(db.QueryRowContext(ctx, "SELECT COUNT(*) FROM oauth_handoffs WHERE provider=?", provider).Scan(&count))
		if count != 1 { t.Fatal("optional-provider handoff was not durably issued once") }
	}
}
