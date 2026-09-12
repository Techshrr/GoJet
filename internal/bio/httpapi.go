package bio

import (
	"context"
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
	risk            RiskAuthority
	testAuthEnabled bool
	actorResolver   ActorResolver
}

type actorContext struct {
	ActorID string
	Role    string
}

type childRequest struct {
	ID             uint64 `json:"id,omitempty"`
	Position       uint   `json:"position"`
	Label          string `json:"label"`
	DestinationURL string `json:"destination_url"`
}

type createRequest struct {
	Title        string         `json:"title"`
	Bio          string         `json:"bio"`
	Links        []childRequest `json:"links"`
	ChangeReason string         `json:"change_reason"`
}

type updateRequest struct {
	ExpectedVersion uint64          `json:"expected_version"`
	Title           *string         `json:"title"`
	Bio             *string         `json:"bio"`
	Links           *[]childRequest `json:"links"`
	ChangeReason    string          `json:"change_reason"`
}

type transitionRequest struct {
	ExpectedVersion uint64 `json:"expected_version"`
	ChangeReason    string `json:"change_reason"`
}

type deleteRequest struct {
	ExpectedVersion uint64 `json:"expected_version"`
	ChangeReason    string `json:"change_reason"`
}

func NewAPI(store *Store, risk RiskAuthority, testAuthEnabled bool) (*API, error) {
	if store == nil || risk == nil {
		return nil, ErrInvalidInput
	}
	return &API{store: store, risk: risk, testAuthEnabled: testAuthEnabled}, nil
}

func NewAPIWithActorResolver(store *Store, risk RiskAuthority, resolver ActorResolver) (*API, error) {
	if store == nil || risk == nil || resolver == nil {
		return nil, ErrInvalidInput
	}
	return &API{store: store, risk: risk, actorResolver: resolver}, nil
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/bio-pages", a.list)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/bio-pages", a.create)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/bio-pages/{pageId}", a.get)
	mux.HandleFunc("PATCH /api/workspaces/{workspaceId}/bio-pages/{pageId}", a.update)
	mux.HandleFunc("DELETE /api/workspaces/{workspaceId}/bio-pages/{pageId}", a.delete)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/bio-pages/{pageId}/publish", a.publish)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/bio-pages/{pageId}/pause", a.pause)
	mux.HandleFunc("GET /p/{slug}", a.publicPage)
	mux.HandleFunc("GET /api/public/bio/{slug}", a.publicAPI)
	return bioSecurityHeaders(mux)
}

func bioSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
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

	// Preserve the predecessor P11 test-only adapter for isolated P11 authority tests.
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
		"quota": map[string]any{
			"used": result.QuotaUsed, "limit": result.QuotaLimit, "reached": result.QuotaUsed >= result.QuotaLimit,
		},
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
	created, err := a.store.Create(r.Context(), CreateInput{
		WorkspaceID:   workspaceID,
		Title:         request.Title,
		Bio:           request.Bio,
		Links:         childInputs(request.Links),
		ActorID:       actor.ActorID,
		CorrelationID: correlationID(r),
		Reason:        request.ChangeReason,
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
	pageID, ok := parsePageID(w, r.PathValue("pageId"))
	if !ok {
		return
	}
	page, err := a.store.Get(r.Context(), workspaceID, pageID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	page, err = a.refreshRisk(r.Context(), page, time.Now().UTC())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	pageID, ok := parsePageID(w, r.PathValue("pageId"))
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
	var linksInput *[]ChildInput
	if request.Links != nil {
		value := childInputs(*request.Links)
		linksInput = &value
	}
	updated, err := a.store.Update(r.Context(), UpdateInput{
		WorkspaceID:     workspaceID,
		PageID:          pageID,
		ExpectedVersion: request.ExpectedVersion,
		Title:           request.Title,
		Bio:             request.Bio,
		Links:           linksInput,
		ActorID:         actor.ActorID,
		CorrelationID:   correlationID(r),
		Reason:          request.ChangeReason,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *API) publish(w http.ResponseWriter, r *http.Request) {
	a.transition(w, r, "published")
}

func (a *API) pause(w http.ResponseWriter, r *http.Request) {
	a.transition(w, r, "paused")
}

func (a *API) transition(w http.ResponseWriter, r *http.Request, status string) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	pageID, ok := parsePageID(w, r.PathValue("pageId"))
	if !ok {
		return
	}
	var request transitionRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.ExpectedVersion == 0 || strings.TrimSpace(request.ChangeReason) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", "expected_version and change_reason are required.")
		return
	}
	if status == "published" {
		page, err := a.store.Get(r.Context(), workspaceID, pageID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if page.Version != request.ExpectedVersion {
			writeStoreError(w, ErrConflict)
			return
		}
		if _, err := a.refreshRisk(r.Context(), page, time.Now().UTC()); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	updated, err := a.store.Transition(r.Context(), TransitionInput{
		WorkspaceID:     workspaceID,
		PageID:          pageID,
		ExpectedVersion: request.ExpectedVersion,
		Status:          status,
		ActorID:         actor.ActorID,
		CorrelationID:   correlationID(r),
		Reason:          request.ChangeReason,
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
	pageID, ok := parsePageID(w, r.PathValue("pageId"))
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
		WorkspaceID:     workspaceID,
		PageID:          pageID,
		ExpectedVersion: request.ExpectedVersion,
		ActorID:         actor.ActorID,
		CorrelationID:   correlationID(r),
		Reason:          request.ChangeReason,
	}); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) refreshRisk(ctx context.Context, page Page, now time.Time) (Page, error) {
	for i := range page.Links {
		child := &page.Links[i]
		decision, state, resolveErr := a.risk.Resolve(ctx, child.ID, child.DestinationFingerprint, now)
		mapped := "review"
		var checkedAt *time.Time
		if resolveErr == nil {
			mapped = mapRiskState(state)
			if state == links.RiskAllow || state == links.RiskReview || state == links.RiskBlock {
				value := decision.CheckedAt.UTC()
				checkedAt = &value
			}
		}
		if err := a.store.SyncChildRisk(ctx, child.ID, child.DestinationFingerprint, mapped, checkedAt); err != nil {
			return Page{}, err
		}
		child.RiskStatus = mapped
		child.RiskCheckedAt = checkedAt
	}
	return page, nil
}

func childInputs(input []childRequest) []ChildInput {
	result := make([]ChildInput, 0, len(input))
	for _, child := range input {
		result = append(result, ChildInput{
			ID:             child.ID,
			Position:       child.Position,
			Label:          child.Label,
			DestinationURL: child.DestinationURL,
		})
	}
	return result
}

func correlationID(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if value != "" && len(value) <= 128 {
		return value
	}
	return fmt.Sprintf("p11-%d", time.Now().UTC().UnixNano())
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

func parsePageID(w http.ResponseWriter, value string) (uint64, bool) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_bio_page_id", "Bio page ID is invalid.")
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

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_input", "Request validation failed.")
	case errors.Is(err, ErrNotFound):
		writeAPIError(w, http.StatusNotFound, "not_found", "Bio resource not found.")
	case errors.Is(err, ErrDeleted):
		writeAPIError(w, http.StatusGone, "deleted", "Bio resource has been removed.")
	case errors.Is(err, ErrQuota):
		writeAPIError(w, http.StatusTooManyRequests, "quota_reached", "Workspace Bio quota reached.")
	case errors.Is(err, ErrConflict):
		writeAPIError(w, http.StatusConflict, "version_conflict", "The Bio resource changed; retry against the current version.")
	case errors.Is(err, ErrRiskUnresolved):
		writeAPIError(w, http.StatusConflict, "child_link_risk_unresolved", "Bio cannot be published while a child link is not currently allowed.")
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
