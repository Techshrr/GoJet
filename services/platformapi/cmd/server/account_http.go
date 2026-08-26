package main

import (
	"database/sql"
	"net/http"
	"os"
	"strings"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/redis/go-redis/v9"
)

const accountCSRFTTL = 10 * time.Minute

type accountHTTPHandler struct {
	store    *authn.Store
	accounts *authn.AccountService
	oauth    *authn.OAuthService
	csrf     *authn.CSRFManager
	origins  *authn.OriginPolicy
}

func buildAccountHandler(db *sql.DB, redisClient *redis.Client) (http.Handler, bool, error) {
	if os.Getenv("GOJET_ACCOUNT_ENABLED") != "1" {
		return nil, false, nil
	}
	if db == nil || redisClient == nil {
		return nil, false, authn.ErrInvalid
	}
	csrfKey, err := decodeExactHexKey(os.Getenv("GOJET_AUTH_CSRF_KEY_HEX"))
	if err != nil {
		return nil, false, err
	}
	rawOrigins := strings.TrimSpace(os.Getenv("GOJET_AUTH_ALLOWED_ORIGIN"))
	if rawOrigins == "" {
		return nil, false, authn.ErrInvalid
	}
	origins := make([]string, 0, 2)
	for _, raw := range strings.Split(rawOrigins, ",") {
		if value := strings.TrimSpace(raw); value != "" {
			origins = append(origins, value)
		}
	}
	originPolicy, err := authn.NewOriginPolicy(origins...)
	if err != nil {
		return nil, false, err
	}
	replay, err := authn.NewRedisDigestReplayStore(redisClient, "auth:csrf:account", time.Hour)
	if err != nil {
		return nil, false, err
	}
	csrf, err := authn.NewCSRFManager(csrfKey, accountCSRFTTL, replay)
	if err != nil {
		return nil, false, err
	}
	oauthKeyID := strings.TrimSpace(os.Getenv("GOJET_OAUTH_KEY_ID"))
	oauthKey, err := decodeExactHexKey(os.Getenv("GOJET_OAUTH_KEY_HEX"))
	if err != nil || oauthKeyID == "" {
		return nil, false, authn.ErrInvalid
	}
	oauthCrypto, err := authn.NewOAuthCrypto(oauthKeyID, oauthKey)
	if err != nil {
		return nil, false, err
	}
	oauthService, err := authn.NewOAuthService(db, oauthCrypto, 10*time.Minute)
	if err != nil {
		return nil, false, err
	}
	accounts, err := authn.NewAccountService(db)
	if err != nil {
		return nil, false, err
	}
	h := &accountHTTPHandler{
		store:    authn.NewStore(db),
		accounts: accounts,
		oauth:    oauthService,
		csrf:     csrf,
		origins:  originPolicy,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/logout", h.handleLogout)
	mux.HandleFunc("GET /api/me", h.handleMe)
	mux.HandleFunc("PATCH /api/me/profile", h.handleProfile)
	mux.HandleFunc("PATCH /api/me/password", h.handlePassword)
	mux.HandleFunc("GET /api/me/sessions", h.handleSessions)
	mux.HandleFunc("DELETE /api/me/sessions/{sessionId}", h.handleSessionRevoke)
	mux.HandleFunc("GET /api/me/connected-accounts", h.handleConnectedAccounts)
	mux.HandleFunc("POST /api/me/connected-accounts/{provider}/start", h.handleConnectedAccountStart)
	mux.HandleFunc("DELETE /api/me/connected-accounts/{provider}", h.handleConnectedAccountDelete)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authn.ApplyPrivateAuthHeaders(w.Header())
		mux.ServeHTTP(w, r)
	}), true, nil
}

func mountAccountRoutes(root *http.ServeMux, handler http.Handler) {
	for _, pattern := range []string{
		"POST /api/auth/logout",
		"GET /api/me",
		"PATCH /api/me/profile",
		"PATCH /api/me/password",
		"GET /api/me/sessions",
		"DELETE /api/me/sessions/{sessionId}",
		"GET /api/me/connected-accounts",
		"POST /api/me/connected-accounts/{provider}/start",
		"DELETE /api/me/connected-accounts/{provider}",
	} {
		root.Handle(pattern, handler)
	}
}

func (h *accountHTTPHandler) currentSession(w http.ResponseWriter, r *http.Request) (authn.Session, bool) {
	session, err := authn.AuthenticateRequest(r.Context(), h.store, r, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return authn.Session{}, false
	}
	return session, true
}

func (h *accountHTTPHandler) mutationAuthority(w http.ResponseWriter, r *http.Request) (authn.Session, *authn.UnsafeMutationAuthority, bool) {
	now := time.Now().UTC()
	session, err := authn.AuthenticateRequest(r.Context(), h.store, r, now)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return authn.Session{}, nil, false
	}
	authority, err := authn.AuthorizeUnsafeMutation(r.Context(), r, session, h.origins, h.csrf, now)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return authn.Session{}, nil, false
	}
	return session, authority, true
}

func requestCorrelation(r *http.Request) (string, error) {
	return authCorrelation(r.Header.Get("X-Request-ID"))
}

func (h *accountHTTPHandler) handleMe(w http.ResponseWriter, r *http.Request) {
	session, ok := h.currentSession(w, r)
	if !ok {
		return
	}
	user, err := h.store.GetUserByID(r.Context(), session.UserID)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	csrf, err := h.csrf.Issue(session.ID, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":                user.ID,
			"email":             user.Email,
			"display_name":      user.DisplayName,
			"status":            user.Status,
			"email_verified_at": user.EmailVerifiedAt,
		},
		"session": map[string]any{
			"id":           session.ID,
			"status":       session.Status,
			"expires_at":   session.ExpiresAt,
			"last_seen_at": session.LastSeenAt,
			"created_at":   session.CreatedAt,
		},
		"csrf_token": csrf,
	})
}

func (h *accountHTTPHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	session, authority, ok := h.mutationAuthority(w, r)
	if !ok {
		return
	}
	correlationID, err := requestCorrelation(r)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	if err := h.accounts.RevokeSession(r.Context(), session, authority, session.ID, correlationID, time.Now().UTC()); err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authn.SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	writeAuthJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (h *accountHTTPHandler) handleProfile(w http.ResponseWriter, r *http.Request) {
	session, authority, ok := h.mutationAuthority(w, r)
	if !ok {
		return
	}
	var input struct {
		DisplayName string `json:"display_name"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	correlationID, err := requestCorrelation(r)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	user, err := h.accounts.UpdateProfile(r.Context(), session, authority, input.DisplayName, correlationID, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"user": map[string]any{
		"id": user.ID, "email": user.Email, "display_name": user.DisplayName, "status": user.Status,
	}})
}

func (h *accountHTTPHandler) handlePassword(w http.ResponseWriter, r *http.Request) {
	session, authority, ok := h.mutationAuthority(w, r)
	if !ok {
		return
	}
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	correlationID, err := requestCorrelation(r)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	if err := h.accounts.ChangePassword(r.Context(), session, authority, input.CurrentPassword, input.NewPassword, correlationID, time.Now().UTC()); err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]string{"status": "password_changed"})
}

func (h *accountHTTPHandler) handleSessions(w http.ResponseWriter, r *http.Request) {
	session, ok := h.currentSession(w, r)
	if !ok {
		return
	}
	items, err := h.accounts.ListSessions(r.Context(), session, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"sessions": items})
}

func (h *accountHTTPHandler) handleSessionRevoke(w http.ResponseWriter, r *http.Request) {
	session, authority, ok := h.mutationAuthority(w, r)
	if !ok {
		return
	}
	correlationID, err := requestCorrelation(r)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	if err := h.accounts.RevokeSession(r.Context(), session, authority, r.PathValue("sessionId"), correlationID, time.Now().UTC()); err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (h *accountHTTPHandler) handleConnectedAccounts(w http.ResponseWriter, r *http.Request) {
	session, ok := h.currentSession(w, r)
	if !ok {
		return
	}
	items, err := h.oauth.ListConnectedAccounts(r.Context(), session, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"accounts": items})
}

func (h *accountHTTPHandler) handleConnectedAccountStart(w http.ResponseWriter, r *http.Request) {
	session, authority, ok := h.mutationAuthority(w, r)
	if !ok {
		return
	}
	correlationID, err := requestCorrelation(r)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	result, err := h.oauth.Start(r.Context(), authn.OAuthStartInput{
		Provider:            r.PathValue("provider"),
		Intent:              authn.OAuthIntentBind,
		InitiatingUserID:    session.UserID,
		InitiatingSessionID: session.ID,
		CorrelationID:       correlationID,
		MutationAuthority:   authority,
	}, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{
		"provider":          result.Provider,
		"authorization_url": result.AuthorizationURL,
		"expires_at":        result.ExpiresAt,
	})
}

func (h *accountHTTPHandler) handleConnectedAccountDelete(w http.ResponseWriter, r *http.Request) {
	session, authority, ok := h.mutationAuthority(w, r)
	if !ok {
		return
	}
	correlationID, err := requestCorrelation(r)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	if err := h.oauth.UnbindConnectedAccount(r.Context(), session, authority, r.PathValue("provider"), correlationID, time.Now().UTC()); err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}
