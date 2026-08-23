package files

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestPublicAuthTokenBindsSlugPasswordAndExpiry(t *testing.T) {
	api := &API{publicAuthSecret: []byte("0123456789abcdef0123456789abcdef")}
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	token, _, err := api.signPublicAuth("slug-one", "verifier-one", now)
	if err != nil {
		t.Fatal(err)
	}
	if !api.verifyPublicAuthToken(token, "slug-one", "verifier-one", now.Add(time.Minute)) {
		t.Fatal("current token must verify")
	}
	if api.verifyPublicAuthToken(token, "slug-two", "verifier-one", now.Add(time.Minute)) {
		t.Fatal("token must not authorize another slug")
	}
	if api.verifyPublicAuthToken(token, "slug-one", "verifier-two", now.Add(time.Minute)) {
		t.Fatal("token must not authorize a changed password verifier")
	}
	if api.verifyPublicAuthToken(token, "slug-one", "verifier-one", now.Add(publicAuthTTL+time.Second)) {
		t.Fatal("expired token must fail")
	}
}

func TestPublicAuthCookieIsHttpOnlyAndSecureOutsideTestAdapter(t *testing.T) {
	api := &API{publicAuthSecret: []byte("0123456789abcdef0123456789abcdef")}
	now := time.Now().UTC()
	request := httptest.NewRequest("POST", "http://example.test/f/slug", nil)
	response := httptest.NewRecorder()
	if err := api.setPublicAuthCookie(response, request, "slug", "verifier", now); err != nil {
		t.Fatal(err)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("production cookie flags are unsafe: %#v", cookies)
	}
}
