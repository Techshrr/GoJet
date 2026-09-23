package main

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
)

const googleOneTapCookie = "__Host-gojet_one_tap"

var googleOneTapVerifier = authn.NewGoogleOneTapVerifier()

func (h *authHTTPHandler) oneTapAllowed(w http.ResponseWriter, r *http.Request) (string, bool) {
	policy, err := authn.NewOriginPolicy(strings.Split(os.Getenv("GOJET_AUTH_ALLOWED_ORIGIN"), ",")...)
	if err != nil || policy.ValidateUnsafe(r) != nil {
		writeAuthProblem(w, http.StatusForbidden, "forbidden", "The request could not be validated.")
		return "", false
	}
	// Never replace an existing browser login via an automatic prompt.
	if cookie, err := r.Cookie(authn.SessionCookieName); err == nil && cookie.Value != "" {
		writeAuthJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return "", false
	}
	clientID, err := h.oauth.GoogleOneTapClientID(r.Context())
	if err != nil {
		writeAuthProblem(w, http.StatusServiceUnavailable, "provider_error", "Sign in is temporarily unavailable.")
		return "", false
	}
	if clientID == "" {
		writeAuthJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return "", false
	}
	return clientID, true
}

func (h *authHTTPHandler) handleGoogleOneTapStart(w http.ResponseWriter, r *http.Request) {
	clientID, ok := h.oneTapAllowed(w, r)
	if !ok { return }
	correlation, err := authCorrelation("")
	if err != nil { writeAuthServiceError(w, err, false); return }
	start, err := h.oauth.Start(r.Context(), authn.OAuthStartInput{Provider: authn.ProviderGoogle, Intent: authn.OAuthIntentLogin, CorrelationID: correlation}, time.Now().UTC())
	if err != nil { writeAuthServiceError(w, err, false); return }
	http.SetCookie(w, &http.Cookie{Name: googleOneTapCookie, Value: start.State, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: start.ExpiresAt})
	writeAuthJSON(w, http.StatusOK, map[string]any{"enabled": true, "client_id": clientID, "state": start.State, "nonce": start.PKCEVerifier})
}

func (h *authHTTPHandler) handleGoogleOneTapComplete(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.oneTapAllowed(w, r); !ok { return }
	var input struct { State string `json:"state"`; Credential string `json:"credential"` }
	if !decodeAuthJSON(w, r, &input) { return }
	cookie, err := r.Cookie(googleOneTapCookie)
	if err != nil || input.State == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(input.State)) != 1 {
		writeAuthProblem(w, http.StatusBadRequest, "state_error", "The sign-in challenge could not be validated.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: googleOneTapCookie, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	correlation, err := authCorrelation("")
	if err != nil { writeAuthServiceError(w, err, false); return }
	handoff, err := h.oauth.CompleteGoogleOneTap(r.Context(), googleOneTapVerifier, input.State, input.Credential, correlation, time.Now().UTC())
	if err != nil { writeAuthProblem(w, http.StatusBadRequest, "state_error", "The sign-in credential could not be validated."); return }
	writeAuthJSON(w, http.StatusOK, map[string]any{"status": "handoff_ready", "handoff_code": handoff.Code, "expires_at": handoff.ExpiresAt})
}
