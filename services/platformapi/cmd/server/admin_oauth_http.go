package main

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"strings"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/redis/go-redis/v9"
)

const adminOAuthCSRFTTL = 10 * time.Minute

type adminOAuthPermissionAuthorizer struct {
	allowTestSettingsManage bool
}

func (a adminOAuthPermissionAuthorizer) Authorize(_ context.Context, actorID, permission string) error {
	if strings.TrimSpace(actorID) == "" || permission != authn.SettingsManage || !a.allowTestSettingsManage {
		return authn.ErrForbidden
	}
	return nil
}

type adminOAuthHTTPHandler struct {
	store       *authn.Store
	oauth       *authn.OAuthService
	csrf        *authn.CSRFManager
	origins     *authn.OriginPolicy
	authorizer  authn.SettingsPermissionAuthorizer
}

func buildAdminOAuthHandler(db *sql.DB, redisClient *redis.Client, testAuth bool) (http.Handler, bool, error) {
	if os.Getenv("GOJET_ADMIN_OAUTH_ENABLED") != "1" {
		return nil, false, nil
	}
	if db == nil || redisClient == nil {
		return nil, false, authn.ErrInvalid
	}
	testSettingsManage := os.Getenv("GOJET_TEST_SETTINGS_MANAGE_ENABLED") == "1"
	if testSettingsManage && !testAuth {
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
	replay, err := authn.NewRedisDigestReplayStore(redisClient, "auth:csrf:admin-oauth", time.Hour)
	if err != nil {
		return nil, false, err
	}
	csrf, err := authn.NewCSRFManager(csrfKey, adminOAuthCSRFTTL, replay)
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
	h := &adminOAuthHTTPHandler{
		store:      authn.NewStore(db),
		oauth:      oauthService,
		csrf:       csrf,
		origins:    originPolicy,
		authorizer: adminOAuthPermissionAuthorizer{allowTestSettingsManage: testAuth && testSettingsManage},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/oauth/providers", h.handleListProviders)
	mux.HandleFunc("PATCH /api/admin/oauth/providers/{provider}", h.handleUpdateProvider)
	mux.HandleFunc("POST /api/admin/oauth/providers/{provider}/test", h.handleTestProvider)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authn.ApplyPrivateAuthHeaders(w.Header())
		mux.ServeHTTP(w, r)
	}), true, nil
}

func mountAdminOAuthRoutes(root *http.ServeMux, handler http.Handler) {
	for _, pattern := range []string{
		"GET /api/admin/oauth/providers",
		"PATCH /api/admin/oauth/providers/{provider}",
		"POST /api/admin/oauth/providers/{provider}/test",
	} {
		root.Handle(pattern, handler)
	}
}

func (h *adminOAuthHTTPHandler) currentAuthorizedSession(w http.ResponseWriter, r *http.Request) (authn.Session, bool) {
	session, err := authn.AuthenticateRequest(r.Context(), h.store, r, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return authn.Session{}, false
	}
	if err := h.authorizer.Authorize(r.Context(), session.UserID, authn.SettingsManage); err != nil {
		writeAuthServiceError(w, authn.ErrForbidden, false)
		return authn.Session{}, false
	}
	return session, true
}

func (h *adminOAuthHTTPHandler) mutationAuthority(w http.ResponseWriter, r *http.Request) (authn.Session, *authn.UnsafeMutationAuthority, bool) {
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

func (h *adminOAuthHTTPHandler) handleListProviders(w http.ResponseWriter, r *http.Request) {
	session, ok := h.currentAuthorizedSession(w, r)
	if !ok {
		return
	}
	configs, err := h.oauth.ListProviderConfigs(r.Context())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	csrfToken, err := h.csrf.Issue(session.ID, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"providers": configs, "csrf_token": csrfToken})
}

func (h *adminOAuthHTTPHandler) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	session, authority, ok := h.mutationAuthority(w, r)
	if !ok {
		return
	}
	provider := strings.TrimSpace(r.PathValue("provider"))
	if !authn.ValidProvider(provider) {
		writeAuthServiceError(w, authn.ErrInvalid, false)
		return
	}
	var input struct {
		Enabled          bool     `json:"enabled"`
		ClientID         string   `json:"client_id"`
		ClientSecret     string   `json:"client_secret"`
		AuthorizationURL string   `json:"authorization_url"`
		TokenURL         string   `json:"token_url"`
		UserInfoURL      string   `json:"userinfo_url"`
		RedirectURI      string   `json:"redirect_uri"`
		Scopes           []string `json:"scopes"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	correlationID, err := requestCorrelation(r)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	updated, err := h.oauth.UpdateProviderConfig(r.Context(), session, authority, h.authorizer, session.UserID, correlationID, authn.OAuthProviderUpdate{
		Provider: provider, Enabled: input.Enabled, ClientID: input.ClientID, ClientSecret: input.ClientSecret,
		AuthorizationURL: input.AuthorizationURL, TokenURL: input.TokenURL, UserInfoURL: input.UserInfoURL,
		RedirectURI: input.RedirectURI, Scopes: input.Scopes,
	}, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"provider": updated})
}

func (h *adminOAuthHTTPHandler) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	session, _, ok := h.mutationAuthority(w, r)
	if !ok {
		return
	}
	if err := h.authorizer.Authorize(r.Context(), session.UserID, authn.SettingsManage); err != nil {
		writeAuthServiceError(w, authn.ErrForbidden, false)
		return
	}
	provider := strings.TrimSpace(r.PathValue("provider"))
	if !authn.ValidProvider(provider) {
		writeAuthServiceError(w, authn.ErrInvalid, false)
		return
	}
	configs, err := h.oauth.ListProviderConfigs(r.Context())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	for _, cfg := range configs {
		if cfg.Provider != provider {
			continue
		}
		if !cfg.Enabled || !cfg.Configured || !cfg.SecretConfigured {
			writeAuthServiceError(w, authn.ErrConflict, false)
			return
		}
		writeAuthJSON(w, http.StatusOK, map[string]any{
			"provider": provider,
			"status": "configuration_ready",
			"configured": true,
			"enabled": true,
			"secret_configured": true,
		})
		return
	}
	writeAuthServiceError(w, authn.ErrNotFound, false)
}
