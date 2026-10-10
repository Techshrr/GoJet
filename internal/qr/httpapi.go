package qrcodes

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/links"
)

type API struct {
	store           *Store
	links           *links.MySQLStore
	risk            *links.RedisRiskStore
	testAuthEnabled bool
	actorResolver   ActorResolver
}

type actorContext struct {
	ActorID string
	Role    string
}

type createRequest struct {
	SourceLinkID uint64 `json:"source_link_id"`
	Label        string `json:"label"`
	ChangeReason string `json:"change_reason"`
}

type deleteRequest struct {
	ChangeReason string `json:"change_reason"`
}

type sourceView struct {
	LinkID    uint64 `json:"link_id"`
	Hostname  string `json:"hostname,omitempty"`
	Code      string `json:"code,omitempty"`
	PublicURL string `json:"public_url,omitempty"`
	RiskState string `json:"risk_state"`
	Reason    string `json:"reason"`
}

type resourceView struct {
	ID           uint64     `json:"id"`
	WorkspaceID  string     `json:"workspace_id"`
	SourceLinkID uint64     `json:"source_link_id"`
	Label        string     `json:"label"`
	State        string     `json:"state"`
	Source       sourceView `json:"source"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func NewAPI(store *Store, linkStore *links.MySQLStore, risk *links.RedisRiskStore, testAuthEnabled bool) *API {
	return &API{store: store, links: linkStore, risk: risk, testAuthEnabled: testAuthEnabled}
}

func NewAPIWithActorResolver(store *Store, linkStore *links.MySQLStore, risk *links.RedisRiskStore, resolver ActorResolver) *API {
	return &API{store: store, links: linkStore, risk: risk, actorResolver: resolver}
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/qr-codes", a.list)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/qr-codes", a.create)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/qr-codes/{qrId}", a.get)
	mux.HandleFunc("DELETE /api/workspaces/{workspaceId}/qr-codes/{qrId}", a.delete)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/qr-codes/{qrId}/preview", a.preview)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/qr-codes/{qrId}/download", a.download)
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
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

	// Preserve the predecessor P08 test-only adapter for its isolated tests.
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

func correlationID(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if value != "" && len(value) <= 128 {
		return value
	}
	return fmt.Sprintf("p08-%d", time.Now().UTC().UnixNano())
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
	items := make([]resourceView, 0, len(result.Items))
	for _, item := range result.Items {
		view, err := a.describe(r, item)
		if err != nil {
			writeAPIError(w, http.StatusServiceUnavailable, "source_authority_unavailable", "Source Link authority is unavailable.")
			return
		}
		items = append(items, view)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
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
	if request.SourceLinkID == 0 || strings.TrimSpace(request.ChangeReason) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", "source_link_id and change_reason are required.")
		return
	}
	authority, err := a.links.ResolveQRSourceAuthority(r.Context(), workspaceID, request.SourceLinkID, a.risk, time.Now().UTC())
	if err != nil {
		writeSourceError(w, err)
		return
	}
	if !authority.Ready {
		writeSourceDenial(w, authority)
		return
	}
	created, err := a.store.Create(r.Context(), workspaceID, request.SourceLinkID, request.Label, actor.ActorID, correlationID(r), request.ChangeReason)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	view := buildView(created, authority)
	writeJSON(w, http.StatusCreated, view)
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	if _, ok := a.authenticate(w, r, workspaceID, false); !ok {
		return
	}
	id, ok := parseQRID(w, r.PathValue("qrId"))
	if !ok {
		return
	}
	resource, err := a.store.Get(r.Context(), workspaceID, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	view, err := a.describe(r, resource)
	if err != nil {
		writeSourceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	id, ok := parseQRID(w, r.PathValue("qrId"))
	if !ok {
		return
	}
	var request deleteRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.ChangeReason) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_input", "change_reason is required.")
		return
	}
	if err := a.store.Delete(r.Context(), workspaceID, id, actor.ActorID, correlationID(r), request.ChangeReason); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) preview(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, false)
}

func (a *API) download(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, true)
}

func (a *API) render(w http.ResponseWriter, r *http.Request, attachment bool) {
	workspaceID := r.PathValue("workspaceId")
	if _, ok := a.authenticate(w, r, workspaceID, false); !ok {
		return
	}
	id, ok := parseQRID(w, r.PathValue("qrId"))
	if !ok {
		return
	}
	resource, err := a.store.Get(r.Context(), workspaceID, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	authority, err := a.links.ResolveQRSourceAuthority(r.Context(), workspaceID, resource.SourceLinkID, a.risk, time.Now().UTC())
	if err != nil {
		writeSourceError(w, err)
		return
	}
	if !authority.Ready {
		writeSourceDenial(w, authority)
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "png"
	}
	artifact, err := Render(authority.PublicURL, format)
	if errors.Is(err, ErrUnsupportedFormat) {
		writeAPIError(w, http.StatusBadRequest, "unsupported_format", "Supported QR formats are png and svg.")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "render_failed", "The QR artifact could not be rendered.")
		return
	}
	w.Header().Set("Content-Type", artifact.ContentType)
	w.Header().Set("X-GoJet-Artifact-SHA256", artifact.SHA256)
	w.Header().Set("ETag", `"sha256-`+artifact.SHA256+`"`)
	filename := fmt.Sprintf("gojet-qr-%d.%s", resource.ID, artifact.Format)
	if attachment {
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	} else {
		w.Header().Set("Content-Disposition", `inline; filename="`+filename+`"`)
	}
	if artifact.Format == "svg" {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(artifact.Bytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(artifact.Bytes)
}

func (a *API) describe(r *http.Request, resource Resource) (resourceView, error) {
	authority, err := a.links.ResolveQRSourceAuthority(r.Context(), resource.WorkspaceID, resource.SourceLinkID, a.risk, time.Now().UTC())
	if err != nil {
		return resourceView{}, err
	}
	return buildView(resource, authority), nil
}

func buildView(resource Resource, authority links.QRSourceAuthority) resourceView {
	state := "source-link-block"
	if authority.Ready {
		state = "ready"
	} else if authority.RiskState == links.RiskReview {
		state = "source-link-review"
	}
	view := resourceView{
		ID: resource.ID, WorkspaceID: resource.WorkspaceID, SourceLinkID: resource.SourceLinkID, Label: resource.Label,
		State: state, CreatedAt: resource.CreatedAt, UpdatedAt: resource.UpdatedAt,
		Source: sourceView{LinkID: resource.SourceLinkID, RiskState: string(authority.RiskState), Reason: authority.Reason},
	}
	if authority.Link.ID != 0 {
		view.Source.Hostname = authority.Link.Hostname
		view.Source.Code = authority.Link.Code
	}
	if authority.Ready {
		view.Source.PublicURL = authority.PublicURL
	}
	return view
}

func writeSourceDenial(w http.ResponseWriter, authority links.QRSourceAuthority) {
	switch authority.RiskState {
	case links.RiskReview:
		writeAPIError(w, http.StatusConflict, "source_link_review", "QR generation and distribution are unavailable while the source Link is under review.")
	case links.RiskBlock:
		writeAPIError(w, http.StatusForbidden, "source_link_block", "QR generation and distribution are blocked by source Link safety policy.")
	default:
		writeAPIError(w, http.StatusConflict, "source_link_unavailable", "The source Link is not currently eligible for QR generation or distribution.")
	}
}

func writeSourceError(w http.ResponseWriter, err error) {
	if errors.Is(err, links.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, "source_link_not_found", "Source Link not found.")
		return
	}
	if errors.Is(err, links.ErrInvalidInput) {
		writeAPIError(w, http.StatusBadRequest, "invalid_source_link", "Source Link is invalid.")
		return
	}
	writeAPIError(w, http.StatusServiceUnavailable, "source_authority_unavailable", "Source Link authority is unavailable.")
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_input", "Request validation failed.")
	case errors.Is(err, ErrNotFound):
		writeAPIError(w, http.StatusNotFound, "not_found", "QR resource not found.")
	case errors.Is(err, ErrDeleted):
		writeAPIError(w, http.StatusGone, "deleted", "QR resource has been deleted.")
	case errors.Is(err, ErrQuota):
		writeAPIError(w, http.StatusTooManyRequests, "quota_reached", "Workspace QR quota reached.")
	default:
		writeAPIError(w, http.StatusInternalServerError, "server_error", "The request could not be completed.")
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "Request body is invalid.")
		return false
	}
	return true
}

func parseQRID(w http.ResponseWriter, value string) (uint64, bool) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_qr_id", "QR ID is invalid.")
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

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
