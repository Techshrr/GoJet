package admin

import (
	"net/http"
	"strings"
)

func (a *HTTPAPI) login(w http.ResponseWriter, r *http.Request) {
	if !a.requireOrigin(w, r) {
		return
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TOTP     string `json:"totp"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	secret, err := a.service.Login(r.Context(), body.Email, body.Password, body.TOTP, strings.TrimSpace(r.Header.Get("X-Correlation-ID")), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: AdminSessionCookie, Value: secret.Token, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, Expires: secret.Session.ExpiresAt})
	writeJSON(w, http.StatusOK, map[string]any{"session": secret.Session, "csrf_token": secret.CSRFToken})
}
func (a *HTTPAPI) logout(w http.ResponseWriter, r *http.Request) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	if err := a.service.Logout(r.Context(), p, strings.TrimSpace(r.Header.Get("X-Correlation-ID")), a.now()); err != nil {
		writeError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: AdminSessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}
func (a *HTTPAPI) currentSession(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	csrf, err := a.service.RotateCSRF(r.Context(), p, a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	permissions := make([]string, 0, len(p.Permissions))
	for permission := range p.Permissions {
		permissions = append(permissions, permission)
	}
	sortStrings(permissions)
	writeJSON(w, http.StatusOK, map[string]any{"administrator": p.Administrator, "session": p.Session, "permissions": permissions, "csrf_token": csrf})
}
func (a *HTTPAPI) sessions(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	items, err := a.service.ListSessions(r.Context(), p, r.URL.Query().Get("administrator_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (a *HTTPAPI) revokeSession(w http.ResponseWriter, r *http.Request) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	item, replayed, err := a.service.RevokeSession(r.Context(), p, r.PathValue("sessionId"), authority(r, body.Reason), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": item, "replayed": replayed})
}
func (a *HTTPAPI) enrollTOTP(w http.ResponseWriter, r *http.Request) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	secret, err := a.service.EnrollTOTP(r.Context(), p, strings.TrimSpace(r.Header.Get("X-Correlation-ID")), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"secret": secret, "algorithm": "SHA1", "digits": 6, "period_seconds": 30})
}
func (a *HTTPAPI) confirmTOTP(w http.ResponseWriter, r *http.Request) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := a.service.ConfirmTOTP(r.Context(), p, body.Code, strings.TrimSpace(r.Header.Get("X-Correlation-ID")), a.now()); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
