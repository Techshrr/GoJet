package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type WorkspaceAPIKeyHTTPAPI struct {
	authority       *WorkspaceAPIKeyAuthority
	testAuthEnabled bool
}

func NewWorkspaceAPIKeyHTTPAPI(authority *WorkspaceAPIKeyAuthority, testAuthEnabled bool) (*WorkspaceAPIKeyHTTPAPI, error) {
	if authority == nil {
		return nil, ErrInvalid
	}
	return &WorkspaceAPIKeyHTTPAPI{authority: authority, testAuthEnabled: testAuthEnabled}, nil
}

func (a *WorkspaceAPIKeyHTTPAPI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/api-keys", a.list)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/api-keys", a.create)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/api-keys/{keyId}/rotate", a.rotate)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/api-keys/{keyId}/revoke", a.revoke)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}

func (a *WorkspaceAPIKeyHTTPAPI) actor(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !a.testAuthEnabled {
		apiKeyWriteError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable")
		return "", false
	}
	actor := strings.TrimSpace(r.Header.Get("X-GoJet-Test-Actor"))
	email := strings.TrimSpace(r.Header.Get("X-GoJet-Test-Email"))
	if actor == "" || email == "" {
		apiKeyWriteError(w, http.StatusUnauthorized, "authentication_required")
		return "", false
	}
	return actor, true
}

func (a *WorkspaceAPIKeyHTTPAPI) list(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.actor(w, r)
	if !ok { return }
	keys, err := a.authority.List(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")), actor)
	if err != nil { apiKeyWriteAuthorityError(w, err); return }
	apiKeyWriteJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

func (a *WorkspaceAPIKeyHTTPAPI) create(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.actor(w, r)
	if !ok { return }
	var input WorkspaceAPIKeyInput
	if !apiKeyDecodeJSON(w, r, &input) { return }
	result, err := a.authority.Create(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")), actor, input, apiKeyCorrelationID(r), time.Now().UTC())
	if err != nil { apiKeyWriteAuthorityError(w, err); return }
	apiKeyWriteJSON(w, http.StatusCreated, result)
}

func (a *WorkspaceAPIKeyHTTPAPI) rotate(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.actor(w, r)
	if !ok { return }
	result, err := a.authority.Rotate(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")), actor, strings.TrimSpace(r.PathValue("keyId")), apiKeyCorrelationID(r), time.Now().UTC())
	if err != nil { apiKeyWriteAuthorityError(w, err); return }
	apiKeyWriteJSON(w, http.StatusOK, result)
}

func (a *WorkspaceAPIKeyHTTPAPI) revoke(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.actor(w, r)
	if !ok { return }
	result, err := a.authority.Revoke(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")), actor, strings.TrimSpace(r.PathValue("keyId")), apiKeyCorrelationID(r), time.Now().UTC())
	if err != nil { apiKeyWriteAuthorityError(w, err); return }
	apiKeyWriteJSON(w, http.StatusOK, map[string]any{"key": result})
}

func apiKeyDecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil { apiKeyWriteError(w, http.StatusBadRequest, "invalid_json"); return false }
	return true
}

func apiKeyWriteAuthorityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid): apiKeyWriteError(w, http.StatusBadRequest, "invalid_request")
	case errors.Is(err, ErrUnauthorized): apiKeyWriteError(w, http.StatusUnauthorized, "authentication_required")
	case errors.Is(err, ErrForbidden): apiKeyWriteError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, ErrNotFound): apiKeyWriteError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, ErrConflict): apiKeyWriteError(w, http.StatusConflict, "conflict")
	case errors.Is(err, ErrRateLimited): apiKeyWriteError(w, http.StatusTooManyRequests, "rate_limited")
	default: apiKeyWriteError(w, http.StatusInternalServerError, "server_error")
	}
}

func apiKeyWriteError(w http.ResponseWriter, status int, code string) { apiKeyWriteJSON(w, status, map[string]any{"error": map[string]any{"code": code}}) }
func apiKeyWriteJSON(w http.ResponseWriter, status int, value any) { w.WriteHeader(status); _ = json.NewEncoder(w).Encode(value) }
func apiKeyCorrelationID(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Request-ID")); value != "" && len(value) <= 128 { return value }
	return fmt.Sprintf("p17-api-key-%d", time.Now().UTC().UnixNano())
}
