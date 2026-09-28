package main

import (
	"net/http"
	"strings"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
)

func (h *accountHTTPHandler) handleConnectedAccountComplete(w http.ResponseWriter, r *http.Request) {
	session, authority, ok := h.mutationAuthority(w, r)
	if !ok {
		return
	}
	var input struct {
		State string `json:"state"`
		Code  string `json:"code"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	provider := strings.TrimSpace(r.PathValue("provider"))
	if !authn.ValidProvider(provider) || !validOAuthBrowserCallback(r, provider, input.State) {
		writeAuthServiceError(w, authn.ErrForbidden, false)
		return
	}
	now := time.Now().UTC()
	pending, err := h.oauth.PendingState(r.Context(), provider, input.State, now)
	if err != nil || pending.Intent != authn.OAuthIntentBind || pending.InitiatingUserID != session.UserID || pending.InitiatingSessionID != session.ID {
		writeAuthServiceError(w, authn.ErrForbidden, false)
		return
	}
	correlation, err := requestCorrelation(r)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	adapter := h.oauthAdapter
	if adapter == nil {
		adapter = authn.NewHTTPProviderAdapter()
	}
	callback, err := h.oauth.Callback(r.Context(), adapter, authn.OAuthCallbackInput{Provider: provider, State: input.State, Code: input.Code, CorrelationID: correlation}, now)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	_, err = h.oauth.BindConnectedAccount(r.Context(), session, authority, callback, correlation+"-bind", now)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	clearOAuthBrowserCookie(w, provider)
	writeAuthJSON(w, http.StatusOK, map[string]string{"status": "connected"})
}

func (h *accountHTTPHandler) registerConnectedAccountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/me/connected-accounts", h.handleConnectedAccounts)
	mux.HandleFunc("POST /api/me/connected-accounts/{provider}/start", h.handleConnectedAccountStart)
	mux.HandleFunc("POST /api/me/connected-accounts/{provider}/complete", h.handleConnectedAccountComplete)
	mux.HandleFunc("DELETE /api/me/connected-accounts/{provider}", h.handleConnectedAccountDelete)
}
