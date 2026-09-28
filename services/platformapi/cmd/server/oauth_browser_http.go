package main

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
)

func (h *authHTTPHandler) registerOAuthBrowserRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/public/auth/{provider}/start", h.handleOAuthBrowserStart)
	mux.HandleFunc("GET /api/public/auth/{provider}/callback", h.handleOAuthCallback)
}

func oauthBrowserCookie(provider string) string {
	return "__Host-gojet_oauth_" + provider
}

func setOAuthBrowserCookie(w http.ResponseWriter, result authn.OAuthStartResult) {
	http.SetCookie(w, &http.Cookie{Name: oauthBrowserCookie(result.Provider), Value: result.State, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: result.ExpiresAt})
}

func validOAuthBrowserCallback(r *http.Request, provider, state string) bool {
	cookie, err := r.Cookie(oauthBrowserCookie(provider))
	return err == nil && state != "" && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) == 1
}

func (h *authHTTPHandler) handleOAuthBrowserStart(w http.ResponseWriter, r *http.Request) {
	authn.ApplyPrivateAuthHeaders(w.Header())
	provider := strings.TrimSpace(r.PathValue("provider"))
	intent := strings.TrimSpace(r.URL.Query().Get("intent"))
	if intent == "" {
		intent = authn.OAuthIntentLogin
	}
	// Binding remains an authenticated, CSRF-protected account mutation.
	if !authn.ValidProvider(provider) || (intent != authn.OAuthIntentLogin && intent != authn.OAuthIntentRegister) {
		writeAuthProblem(w, http.StatusBadRequest, "invalid_request", "The sign-in request could not be validated.")
		return
	}
	// Rainbow uses a server-side bootstrap protocol, not an OAuth authorization URL.
	if provider == authn.ProviderRainbow && !h.testAuth {
		writeAuthProblem(w, http.StatusServiceUnavailable, "provider_error", "This sign-in provider is temporarily unavailable.")
		return
	}
	correlation, err := authCorrelation("")
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	result, err := h.oauth.Start(r.Context(), authn.OAuthStartInput{Provider: provider, Intent: intent, CorrelationID: correlation}, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	setOAuthBrowserCookie(w, result)
	// Only the S256 challenge is public. Neither verifier nor client secret is returned.
	w.Header().Set("Location", result.AuthorizationURL)
	w.WriteHeader(http.StatusFound)
}

func clearOAuthBrowserCookie(w http.ResponseWriter, provider string) {
	http.SetCookie(w, &http.Cookie{Name: oauthBrowserCookie(provider), Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
