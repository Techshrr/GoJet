package textshares

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/links"
)

type API struct {
	store           *Store
	testAuthEnabled bool
	actorResolver   ActorResolver
	publicAuthKey   []byte
}

type actorContext struct {
	ActorID string
	Role    string
}

type createRequest struct {
	Title        string `json:"title"`
	Content      string `json:"content"`
	Visibility   string `json:"visibility"`
	Password     string `json:"password"`
	ExpiresAt    string `json:"expires_at"`
	OneTime      bool   `json:"one_time"`
	ChangeReason string `json:"change_reason"`
}

type updateRequest struct {
	ExpectedVersion uint64  `json:"expected_version"`
	Title           *string `json:"title"`
	Content         *string `json:"content"`
	Visibility      *string `json:"visibility"`
	Password        *string `json:"password"`
	ClearPassword   bool    `json:"clear_password"`
	ExpiresAt       *string `json:"expires_at"`
	ClearExpiresAt  bool    `json:"clear_expires_at"`
	OneTime         *bool   `json:"one_time"`
	ChangeReason    string  `json:"change_reason"`
}

type deleteRequest struct {
	ExpectedVersion uint64 `json:"expected_version"`
	ChangeReason    string `json:"change_reason"`
}

func NewAPI(store *Store, testAuthEnabled bool, publicAuthKey []byte) (*API, error) {
	if store == nil || len(publicAuthKey) < 32 {
		return nil, ErrInvalidInput
	}
	return &API{store: store, testAuthEnabled: testAuthEnabled, publicAuthKey: append([]byte(nil), publicAuthKey...)}, nil
}

func NewAPIWithActorResolver(store *Store, resolver ActorResolver, publicAuthKey []byte) (*API, error) {
	if store == nil || resolver == nil || len(publicAuthKey) < 32 {
		return nil, ErrInvalidInput
	}
	return &API{store: store, actorResolver: resolver, publicAuthKey: append([]byte(nil), publicAuthKey...)}, nil
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/text-shares", a.list)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/text-shares", a.create)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/text-shares/{shareId}", a.get)
	mux.HandleFunc("PATCH /api/workspaces/{workspaceId}/text-shares/{shareId}", a.update)
	mux.HandleFunc("DELETE /api/workspaces/{workspaceId}/text-shares/{shareId}", a.delete)
	mux.HandleFunc("GET /t/{slug}", a.publicPageGet)
	mux.HandleFunc("POST /t/{slug}", a.publicPagePost)
	mux.HandleFunc("POST /api/public/text/{slug}", a.publicTextAction)
	return textSecurityHeaders(mux)
}

func textSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func (a *API) authenticate(w http.ResponseWriter, r *http.Request, workspaceID string, mutation bool) (actorContext, bool) {
	if a.actorResolver != nil {
		actor, err := a.actorResolver(r, strings.TrimSpace(workspaceID))
		if err != nil {
			switch {
			case errors.Is(err, ErrAuthenticationRequired):
				writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			case errors.Is(err, ErrForbidden):
				writeAPIError(w, http.StatusForbidden, "forbidden", "Workspace access denied.")
			default:
				writeAPIError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is unavailable.")
			}
			return actorContext{}, false
		}
		actorID := strings.TrimSpace(actor.ActorID)
		role := strings.ToLower(strings.TrimSpace(actor.Role))
		if actorID == "" || strings.TrimSpace(workspaceID) == "" {
			writeAPIError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is unavailable.")
			return actorContext{}, false
		}
		if role != "owner" && role != "admin" && role != "member" && role != "viewer" {
			writeAPIError(w, http.StatusForbidden, "forbidden", "Workspace access denied.")
			return actorContext{}, false
		}
		if mutation && role == "viewer" {
			writeAPIError(w, http.StatusForbidden, "read_only", "This Workspace role is read-only.")
			return actorContext{}, false
		}
		return actorContext{ActorID: actorID, Role: role}, true
	}

	// Preserve the predecessor P10 test-only adapter for its isolated tests.
	if !a.testAuthEnabled {
		writeAPIError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is not available in this implementation stage.")
		return actorContext{}, false
	}
	actorID := strings.TrimSpace(r.Header.Get("X-GoJet-Test-Actor"))
	role := strings.ToLower(strings.TrimSpace(r.Header.Get("X-GoJet-Test-Workspace-Role")))
	headerWorkspace := strings.TrimSpace(r.Header.Get("X-GoJet-Test-Workspace"))
	if actorID == "" || role == "" || headerWorkspace == "" || headerWorkspace != workspaceID {
		writeAPIError(w, http.StatusForbidden, "forbidden", "Workspace access denied.")
		return actorContext{}, false
	}
	if role != "owner" && role != "admin" && role != "member" && role != "viewer" {
		writeAPIError(w, http.StatusForbidden, "forbidden", "Workspace access denied.")
		return actorContext{}, false
	}
	if mutation && role == "viewer" {
		writeAPIError(w, http.StatusForbidden, "read_only", "This Workspace role is read-only.")
		return actorContext{}, false
	}
	return actorContext{ActorID: actorID, Role: role}, true
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	if _, ok := a.authenticate(w, r, workspaceID, false); !ok {
		return
	}
	limit, err := optionalInt(r.URL.Query().Get("limit"), 50)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_limit", "Invalid list limit.")
		return
	}
	offset, err := optionalInt(r.URL.Query().Get("offset"), 0)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_offset", "Invalid list offset.")
		return
	}
	result, err := a.store.List(r.Context(), workspaceID, limit, offset)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": result.Items,
		"total": result.Total,
		"quota": map[string]any{"used": result.QuotaUsed, "limit": result.QuotaLimit, "reached": result.QuotaUsed >= result.QuotaLimit},
	})
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	var request createRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.ChangeReason) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", "change_reason is required.")
		return
	}
	var passwordHash string
	var err error
	if request.Password != "" {
		passwordHash, err = links.HashLinkPassword(request.Password)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_password", "Password does not meet the required policy.")
			return
		}
	}
	expiresAt, err := parseOptionalTime(request.ExpiresAt)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_expires_at", "expires_at must be an RFC3339 timestamp.")
		return
	}
	created, err := a.store.Create(r.Context(), CreateInput{
		WorkspaceID: workspaceID, Title: request.Title, Content: request.Content, Visibility: request.Visibility,
		PasswordHash: passwordHash, ExpiresAt: expiresAt, OneTime: request.OneTime,
		ActorID: actor.ActorID, CorrelationID: correlationID(r), Reason: request.ChangeReason,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	if _, ok := a.authenticate(w, r, workspaceID, false); !ok {
		return
	}
	id, ok := parseShareID(w, r.PathValue("shareId"))
	if !ok {
		return
	}
	resource, err := a.store.Get(r.Context(), workspaceID, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resource)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	id, ok := parseShareID(w, r.PathValue("shareId"))
	if !ok {
		return
	}
	var request updateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.ExpectedVersion == 0 || strings.TrimSpace(request.ChangeReason) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", "expected_version and change_reason are required.")
		return
	}
	if request.Password != nil && request.ClearPassword {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", "password and clear_password are mutually exclusive.")
		return
	}
	if request.ExpiresAt != nil && request.ClearExpiresAt {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", "expires_at and clear_expires_at are mutually exclusive.")
		return
	}
	var passwordHash *string
	if request.Password != nil {
		hashed, err := links.HashLinkPassword(*request.Password)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_password", "Password does not meet the required policy.")
			return
		}
		passwordHash = &hashed
	}
	var expiresAt *time.Time
	if request.ExpiresAt != nil {
		parsed, err := parseRequiredTime(*request.ExpiresAt)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_expires_at", "expires_at must be an RFC3339 timestamp.")
			return
		}
		expiresAt = &parsed
	}
	updated, err := a.store.Update(r.Context(), UpdateInput{
		WorkspaceID: workspaceID, ShareID: id, ExpectedVersion: request.ExpectedVersion,
		Title: request.Title, Content: request.Content, Visibility: request.Visibility,
		PasswordHash: passwordHash, ClearPassword: request.ClearPassword,
		ExpiresAt: expiresAt, ClearExpiresAt: request.ClearExpiresAt, OneTime: request.OneTime,
		ActorID: actor.ActorID, CorrelationID: correlationID(r), Reason: request.ChangeReason,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	id, ok := parseShareID(w, r.PathValue("shareId"))
	if !ok {
		return
	}
	var request deleteRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.ExpectedVersion == 0 || strings.TrimSpace(request.ChangeReason) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", "expected_version and change_reason are required.")
		return
	}
	if err := a.store.Delete(r.Context(), DeleteInput{
		WorkspaceID: workspaceID, ShareID: id, ExpectedVersion: request.ExpectedVersion,
		ActorID: actor.ActorID, CorrelationID: correlationID(r), Reason: request.ChangeReason,
	}); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func correlationID(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if value != "" && len(value) <= 128 {
		return value
	}
	return fmt.Sprintf("p10-%d", time.Now().UTC().UnixNano())
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "Request body is invalid.")
		return false
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "Request body must contain one JSON object.")
		return false
	}
	return true
}

func parseShareID(w http.ResponseWriter, value string) (uint64, bool) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_text_share_id", "Text share ID is invalid.")
		return 0, false
	}
	return id, true
}

func optionalInt(value string, fallback int) (int, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, ErrInvalidInput
	}
	return parsed, nil
}

func parseOptionalTime(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := parseRequiredTime(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseRequiredTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, ErrInvalidInput
	}
	return parsed.UTC(), nil
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_input", "Request validation failed.")
	case errors.Is(err, ErrNotFound):
		writeAPIError(w, http.StatusNotFound, "not_found", "Text resource not found.")
	case errors.Is(err, ErrDeleted):
		writeAPIError(w, http.StatusGone, "deleted", "Text resource has been removed.")
	case errors.Is(err, ErrQuota):
		writeAPIError(w, http.StatusTooManyRequests, "quota_reached", "Workspace Text quota reached.")
	case errors.Is(err, ErrConflict):
		writeAPIError(w, http.StatusConflict, "version_conflict", "The Text resource changed; retry against the current version.")
	case errors.Is(err, ErrConsumed):
		writeAPIError(w, http.StatusGone, "consumed", "This one-time Text resource has already been consumed.")
	case errors.Is(err, ErrExpired):
		writeAPIError(w, http.StatusGone, "expired", "Text resource has expired.")
	default:
		writeAPIError(w, http.StatusInternalServerError, "server_error", "The request could not be completed.")
	}
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
