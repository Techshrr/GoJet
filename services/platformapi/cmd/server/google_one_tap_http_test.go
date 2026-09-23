package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	authn "github.com/Techshrr/GoJet/internal/auth"
)

func TestGoogleOneTapOriginAndExistingSession(t *testing.T) {
	t.Setenv("GOJET_AUTH_ALLOWED_ORIGIN", "https://gojet.example")
	h := &authHTTPHandler{}
	for _, origin := range []string{"", "null", "https://attacker.example", "https://gojet.example.attacker.example"} {
		r := httptest.NewRequest(http.MethodPost, "https://gojet.example/api/public/auth/google/one-tap/start", nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.handleGoogleOneTapStart(w, r)
		if w.Code != http.StatusForbidden || len(w.Result().Cookies()) != 0 { t.Fatal("foreign origin reached challenge creation") }
	}
	r := httptest.NewRequest(http.MethodPost, "https://gojet.example/api/public/auth/google/one-tap/start", nil)
	r.Header.Set("Origin", "https://gojet.example")
	r.AddCookie(&http.Cookie{Name: authn.SessionCookieName, Value: "existing-browser-session"})
	w := httptest.NewRecorder()
	h.handleGoogleOneTapStart(w, r)
	if w.Code != http.StatusOK || w.Body.String() != "{\"enabled\":false}\n" || len(w.Result().Cookies()) != 0 { t.Fatal("existing login must suppress the prompt without issuing a challenge") }
}
