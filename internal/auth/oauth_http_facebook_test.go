package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
)

func facebookExchangeFixture() OAuthProviderExchangeRequest {
	input := githubExchangeFixture()
	input.Provider = ProviderFacebook
	input.TokenURL = "https://graph.facebook.com/v23.0/oauth/access_token"
	input.UserInfoURL = "https://graph.facebook.com/v23.0/me"
	return input
}

func TestHTTPProviderAdapterFacebookProtocol(t *testing.T) {
	a := NewHTTPProviderAdapter()
	calls := 0
	a.client.Transport = oauthRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		q := r.URL.Query()
		body := `{"access_token":"facebook-test-token"}`
		if r.Method != http.MethodGet || r.URL.Scheme != "https" || r.URL.Host != "graph.facebook.com" {
			t.Fatal("unexpected Facebook request authority")
		}
		switch calls {
		case 1:
			if r.URL.Path != "/v23.0/oauth/access_token" || q.Get("client_id") != "id" || q.Get("client_secret") != "secret" || q.Get("code") != "code" || q.Get("redirect_uri") != facebookExchangeFixture().RedirectURI {
				t.Fatal("incorrect Facebook code exchange binding")
			}
		case 2:
			mac := hmac.New(sha256.New, []byte("secret"))
			mac.Write([]byte("facebook-test-token"))
			if r.URL.Path != "/v23.0/me" || q.Get("fields") != "id,name" || q.Get("appsecret_proof") != hex.EncodeToString(mac.Sum(nil)) || r.Header.Get("Authorization") != "Bearer facebook-test-token" || q.Get("access_token") != "" {
				t.Fatal("incorrect Facebook identity request binding")
			}
			body = `{"id":"stable-subject","name":"Person","email":"untrusted@example.test","verified":true}`
		default:
			t.Fatal("unexpected extra request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	claim, err := a.Exchange(context.Background(), facebookExchangeFixture())
	if err != nil || calls != 2 || claim.Subject != "stable-subject" || claim.Email != "" || claim.EmailVerified {
		t.Fatal("incorrect Facebook identity mapping")
	}
}

func TestHTTPProviderAdapterFacebookEndpointRejection(t *testing.T) {
	for _, endpoint := range []string{
		"https://graph.facebook.com/v24.0/me", "http://graph.facebook.com/v23.0/me",
		"https://graph.facebook.com.attacker.test/v23.0/me", "https://graph.facebook.com/v23.0/me?fields=email",
		"https://graph.facebook.com:443/v23.0/me", "https://user@graph.facebook.com/v23.0/me",
	} {
		input := facebookExchangeFixture()
		input.UserInfoURL = endpoint
		a := NewHTTPProviderAdapter()
		a.client.Transport = oauthRoundTrip(func(*http.Request) (*http.Response, error) {
			t.Fatal("unsafe endpoint reached")
			return nil, nil
		})
		if _, err := a.Exchange(context.Background(), input); err != ErrForbidden {
			t.Fatal("unsafe endpoint accepted")
		}
	}
}

func TestHTTPProviderAdapterFacebookErrorRejection(t *testing.T) {
	for _, stage := range []int{1, 2} {
		a := NewHTTPProviderAdapter()
		calls := 0
		a.client.Transport = oauthRoundTrip(func(*http.Request) (*http.Response, error) {
			calls++
			body := `{"access_token":"test-token"}`
			if calls == stage {
				body = `{"error":{"message":"secret provider response"},"access_token":"token","id":"subject"}`
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})
		claim, err := a.Exchange(context.Background(), facebookExchangeFixture())
		if err != ErrForbidden || calls != stage || claim.Subject != "" {
			t.Fatal("provider error did not fail closed")
		}
	}
}
