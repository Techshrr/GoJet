package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type WorkspaceWebhookHTTPAPI struct {
	authority       *WorkspaceWebhookAuthority
	testAuthEnabled bool
}

func NewWorkspaceWebhookHTTPAPI(authority *WorkspaceWebhookAuthority, testAuthEnabled bool) (*WorkspaceWebhookHTTPAPI, error) {
	if authority == nil {
		return nil, ErrInvalid
	}
	return &WorkspaceWebhookHTTPAPI{authority: authority, testAuthEnabled: testAuthEnabled}, nil
}

func (a *WorkspaceWebhookHTTPAPI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/webhooks", a.list)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/webhooks", a.create)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/webhooks/{webhookId}", a.get)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/webhooks/{webhookId}/rotate-secret", a.rotateSecret)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/webhooks/{webhookId}/enable", a.enable)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/webhooks/{webhookId}/disable", a.disable)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/webhooks/{webhookId}/deliveries", a.deliveries)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/webhooks/{webhookId}/deliveries/{deliveryId}/retry", a.retryDelivery)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}

func (a *WorkspaceWebhookHTTPAPI) actor(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !a.testAuthEnabled {
		webhookWriteError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable")
		return "", false
	}
	actor := strings.TrimSpace(r.Header.Get("X-GoJet-Test-Actor"))
	email := strings.TrimSpace(r.Header.Get("X-GoJet-Test-Email"))
	if actor == "" || email == "" {
		webhookWriteError(w, http.StatusUnauthorized, "authentication_required")
		return "", false
	}
	return actor, true
}

func (a *WorkspaceWebhookHTTPAPI) list(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.actor(w, r)
	if !ok {
		return
	}
	items, err := a.authority.List(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")), actor)
	if err != nil {
		webhookWriteAuthorityError(w, err)
		return
	}
	webhookWriteJSON(w, http.StatusOK, map[string]any{"webhooks": items})
}

func (a *WorkspaceWebhookHTTPAPI) create(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.actor(w, r)
	if !ok {
		return
	}
	var input WorkspaceWebhookInput
	if !webhookDecodeJSON(w, r, &input) {
		return
	}
	result, err := a.authority.Create(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")), actor, input, webhookCorrelationID(r), time.Now().UTC())
	if err != nil {
		webhookWriteAuthorityError(w, err)
		return
	}
	webhookWriteJSON(w, http.StatusCreated, result)
}

func (a *WorkspaceWebhookHTTPAPI) get(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.actor(w, r)
	if !ok {
		return
	}
	item, err := a.authority.Get(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")), actor, strings.TrimSpace(r.PathValue("webhookId")))
	if err != nil {
		webhookWriteAuthorityError(w, err)
		return
	}
	webhookWriteJSON(w, http.StatusOK, map[string]any{"webhook": item})
}

func (a *WorkspaceWebhookHTTPAPI) rotateSecret(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.actor(w, r)
	if !ok {
		return
	}
	result, err := a.authority.RotateSecret(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")), actor, strings.TrimSpace(r.PathValue("webhookId")), webhookCorrelationID(r), time.Now().UTC())
	if err != nil {
		webhookWriteAuthorityError(w, err)
		return
	}
	webhookWriteJSON(w, http.StatusOK, result)
}

func (a *WorkspaceWebhookHTTPAPI) enable(w http.ResponseWriter, r *http.Request) {
	a.setEnabled(w, r, true)
}

func (a *WorkspaceWebhookHTTPAPI) disable(w http.ResponseWriter, r *http.Request) {
	a.setEnabled(w, r, false)
}

func (a *WorkspaceWebhookHTTPAPI) setEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	actor, ok := a.actor(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
	webhookID := strings.TrimSpace(r.PathValue("webhookId"))
	var item WorkspaceWebhook
	var err error
	if enabled {
		item, err = a.authority.Enable(r.Context(), workspaceID, actor, webhookID, webhookCorrelationID(r), time.Now().UTC())
	} else {
		item, err = a.authority.Disable(r.Context(), workspaceID, actor, webhookID, webhookCorrelationID(r), time.Now().UTC())
	}
	if err != nil {
		webhookWriteAuthorityError(w, err)
		return
	}
	webhookWriteJSON(w, http.StatusOK, map[string]any{"webhook": item})
}

func (a *WorkspaceWebhookHTTPAPI) deliveries(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.actor(w, r)
	if !ok {
		return
	}
	items, err := a.authority.ListDeliveries(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")), actor, strings.TrimSpace(r.PathValue("webhookId")))
	if err != nil {
		webhookWriteAuthorityError(w, err)
		return
	}
	webhookWriteJSON(w, http.StatusOK, map[string]any{"deliveries": items})
}

func (a *WorkspaceWebhookHTTPAPI) retryDelivery(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.actor(w, r)
	if !ok {
		return
	}
	item, err := a.authority.RetryDelivery(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")), actor, strings.TrimSpace(r.PathValue("webhookId")), strings.TrimSpace(r.PathValue("deliveryId")), webhookCorrelationID(r), time.Now().UTC())
	if err != nil {
		webhookWriteAuthorityError(w, err)
		return
	}
	webhookWriteJSON(w, http.StatusOK, map[string]any{"delivery": item})
}

func webhookDecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		webhookWriteError(w, http.StatusBadRequest, "invalid_json")
		return false
	}
	return true
}

func webhookWriteAuthorityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		webhookWriteError(w, http.StatusBadRequest, "invalid_request")
	case errors.Is(err, ErrUnauthorized):
		webhookWriteError(w, http.StatusUnauthorized, "authentication_required")
	case errors.Is(err, ErrForbidden):
		webhookWriteError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, ErrNotFound):
		webhookWriteError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, ErrConflict):
		webhookWriteError(w, http.StatusConflict, "conflict")
	case errors.Is(err, ErrRateLimited):
		webhookWriteError(w, http.StatusTooManyRequests, "rate_limited")
	default:
		webhookWriteError(w, http.StatusInternalServerError, "server_error")
	}
}

func webhookWriteError(w http.ResponseWriter, status int, code string) {
	webhookWriteJSON(w, status, map[string]any{"error": map[string]any{"code": code}})
}

func webhookWriteJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func webhookCorrelationID(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Request-ID")); value != "" && len(value) <= 128 {
		return value
	}
	return fmt.Sprintf("p17-webhook-%d", time.Now().UTC().UnixNano())
}
