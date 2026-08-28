package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const AdminSessionCookie = "gojet_admin_session"

type HTTPAPI struct {
	service *Service
	now     func() time.Time
}

func NewHTTPAPI(service *Service) (*HTTPAPI, error) {
	if service == nil {
		return nil, ErrInvalid
	}
	return &HTTPAPI{service: service, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (a *HTTPAPI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/auth/login", a.login)
	mux.HandleFunc("POST /api/admin/auth/logout", a.logout)
	mux.HandleFunc("GET /api/admin/auth/session", a.currentSession)
	mux.HandleFunc("GET /api/admin/auth/sessions", a.sessions)
	mux.HandleFunc("POST /api/admin/auth/sessions/{sessionId}/revoke", a.revokeSession)
	mux.HandleFunc("POST /api/admin/auth/totp/enroll", a.enrollTOTP)
	mux.HandleFunc("POST /api/admin/auth/totp/confirm", a.confirmTOTP)
	mux.HandleFunc("GET /api/admin/overview", a.overview)
	mux.HandleFunc("GET /api/admin/permissions", a.permissions)
	mux.HandleFunc("GET /api/admin/roles", a.roles)
	mux.HandleFunc("POST /api/admin/roles", a.createRole)
	mux.HandleFunc("GET /api/admin/administrators", a.administrators)
	mux.HandleFunc("POST /api/admin/administrators", a.createAdministrator)
	mux.HandleFunc("GET /api/admin/users", a.managedUsers)
	mux.HandleFunc("GET /api/admin/users/{userId}", a.managedUser)
	mux.HandleFunc("POST /api/admin/users/{userId}/suspend", a.suspendManagedUser)
	mux.HandleFunc("POST /api/admin/users/{userId}/restore", a.restoreManagedUser)
	mux.HandleFunc("GET /api/admin/workspaces", a.managedWorkspaces)
	mux.HandleFunc("GET /api/admin/workspaces/{workspaceId}", a.managedWorkspace)
	mux.HandleFunc("POST /api/admin/workspaces/{workspaceId}/suspend", a.suspendManagedWorkspace)
	mux.HandleFunc("POST /api/admin/workspaces/{workspaceId}/restore", a.restoreManagedWorkspace)
	mux.HandleFunc("GET /api/admin/audit", a.audit)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		adminHeaders(w.Header())
		mux.ServeHTTP(w, r)
	})
}

func adminHeaders(h http.Header) {
	h.Set("Cache-Control", "private, no-store")
	h.Set("Pragma", "no-cache")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, ErrInvalid)
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, ErrInvalid)
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	switch {
	case errors.Is(err, ErrInvalid):
		status = http.StatusBadRequest
		code = "invalid_request"
	case errors.Is(err, ErrUnauthorized):
		status = http.StatusUnauthorized
		code = "unauthorized"
	case errors.Is(err, ErrForbidden):
		status = http.StatusForbidden
		code = "forbidden"
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
		code = "not_found"
	case errors.Is(err, ErrConflict), errors.Is(err, ErrReplayMismatch):
		status = http.StatusConflict
		code = "conflict"
	case errors.Is(err, ErrLocked):
		status = http.StatusLocked
		code = "locked"
	case errors.Is(err, ErrRateLimited):
		status = http.StatusTooManyRequests
		code = "rate_limited"
	case errors.Is(err, ErrMFARequired):
		status = http.StatusPreconditionRequired
		code = "totp_required"
	case errors.Is(err, ErrMFAInvalid):
		status = http.StatusUnauthorized
		code = "invalid_totp"
	case errors.Is(err, ErrReasonRequired):
		status = http.StatusUnprocessableEntity
		code = "reason_required"
	}
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code}})
}

func (a *HTTPAPI) requireOrigin(w http.ResponseWriter, r *http.Request) bool {
	if !a.service.ValidateOrigin(r.Header.Get("Origin")) {
		writeError(w, ErrForbidden)
		return false
	}
	return true
}

func (a *HTTPAPI) principal(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	cookie, err := r.Cookie(AdminSessionCookie)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		writeError(w, ErrUnauthorized)
		return Principal{}, false
	}
	p, err := a.service.Authenticate(r.Context(), cookie.Value, a.now())
	if err != nil {
		writeError(w, err)
		return Principal{}, false
	}
	return p, true
}

func (a *HTTPAPI) mutationPrincipal(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	if !a.requireOrigin(w, r) {
		return Principal{}, false
	}
	p, ok := a.principal(w, r)
	if !ok {
		return Principal{}, false
	}
	if !a.service.ValidateCSRF(p, r.Header.Get("X-CSRF-Token")) {
		writeError(w, ErrForbidden)
		return Principal{}, false
	}
	return p, true
}

func authority(r *http.Request, reason string) MutationAuthority {
	return MutationAuthority{
		Reason:         strings.TrimSpace(reason),
		CorrelationID:  strings.TrimSpace(r.Header.Get("X-Correlation-ID")),
		IdempotencyKey: strings.TrimSpace(r.Header.Get("Idempotency-Key")),
	}
}
