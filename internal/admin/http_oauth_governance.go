package admin

import (
	"net/http"

	authn "github.com/Techshrr/GoJet/internal/auth"
)

// OAuthGovernanceHandler uses the same administrator session as other P17 pages.
func (a *HTTPAPI) OAuthGovernanceHandler(oauth *authn.OAuthService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/oauth/providers", func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.principal(w, r)
		if !ok {
			return
		}
		if err := a.service.Require(p, PermissionSettingsManage); err != nil {
			writeError(w, err)
			return
		}
		items, err := oauth.ListProviderConfigs(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		csrf, err := a.service.RotateCSRF(r.Context(), p, a.now())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": items, "csrf_token": csrf, "authority": "administrator"})
	})
	mux.HandleFunc("PATCH /api/admin/oauth/providers/{provider}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.mutationPrincipal(w, r)
		if !ok {
			return
		}
		var body struct {
			Enabled          bool     `json:"enabled"`
			ClientID         string   `json:"client_id"`
			ClientSecret     string   `json:"client_secret"`
			AuthorizationURL string   `json:"authorization_url"`
			TokenURL         string   `json:"token_url"`
			UserInfoURL      string   `json:"userinfo_url"`
			RedirectURI      string   `json:"redirect_uri"`
			Scopes           []string `json:"scopes"`
			ExpectedVersion  uint64   `json:"expected_version"`
			Reason           string   `json:"reason"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		input := authn.OAuthProviderUpdate{Provider: r.PathValue("provider"), Enabled: body.Enabled, ClientID: body.ClientID, ClientSecret: body.ClientSecret, AuthorizationURL: body.AuthorizationURL, TokenURL: body.TokenURL, UserInfoURL: body.UserInfoURL, RedirectURI: body.RedirectURI, Scopes: body.Scopes}
		item, replayed, err := a.service.UpdateOAuthProvider(r.Context(), p, oauth, input, body.ExpectedVersion, authority(r, body.Reason), a.now())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"provider": item, "replayed": replayed})
	})
	mux.HandleFunc("POST /api/admin/oauth/providers/{provider}/test", func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.mutationPrincipal(w, r)
		if !ok {
			return
		}
		if err := a.service.Require(p, PermissionSettingsManage); err != nil {
			writeError(w, err)
			return
		}
		items, err := oauth.ListProviderConfigs(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		for _, item := range items {
			if item.Provider != r.PathValue("provider") {
				continue
			}
			if !item.Enabled || !item.Configured || !item.SecretConfigured {
				writeError(w, ErrConflict)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"provider": item.Provider, "status": "configuration_ready", "configured": true, "enabled": true, "secret_configured": true})
			return
		}
		writeError(w, ErrNotFound)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		adminHeaders(w.Header())
		mux.ServeHTTP(w, r)
	})
}
