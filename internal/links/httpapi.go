package links

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type API struct {
	store           *MySQLStore
	testAuthEnabled bool
}

type actorContext struct {
	ActorID string
	Role    string
}

type createRequest struct {
	Hostname           string        `json:"hostname"`
	DomainKind         string        `json:"domain_kind"`
	Code               string        `json:"code"`
	Title              string        `json:"title"`
	PrimaryDestination string        `json:"primary_destination"`
	RedirectStatus     int           `json:"redirect_status"`
	Routing            []RoutingRule `json:"routing"`
	AB                 []ABVariant   `json:"ab"`
	UTM                UTMConfig     `json:"utm"`
	Access             AccessConfig  `json:"access"`
	ExpiresAt          *time.Time    `json:"expires_at"`
	ClickLimit         *uint64       `json:"click_limit"`
	OneTime            bool          `json:"one_time"`
	ChangeReason       string        `json:"change_reason"`
}

type updateRequest struct {
	ExpectedVersion    uint64        `json:"expected_version"`
	Hostname           string        `json:"hostname"`
	DomainKind         string        `json:"domain_kind"`
	Code               string        `json:"code"`
	Title              string        `json:"title"`
	PrimaryDestination string        `json:"primary_destination"`
	RedirectStatus     int           `json:"redirect_status"`
	Status             string        `json:"status"`
	Routing            []RoutingRule `json:"routing"`
	AB                 []ABVariant   `json:"ab"`
	UTM                UTMConfig     `json:"utm"`
	Access             AccessConfig  `json:"access"`
	ExpiresAt          *time.Time    `json:"expires_at"`
	ClickLimit         *uint64       `json:"click_limit"`
	OneTime            bool          `json:"one_time"`
	ChangeReason       string        `json:"change_reason"`
}

type deleteRequest struct {
	ExpectedVersion uint64 `json:"expected_version"`
	ChangeReason    string `json:"change_reason"`
}

func NewAPI(store *MySQLStore, testAuthEnabled bool) *API {
	return &API{store: store, testAuthEnabled: testAuthEnabled}
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/links", a.listLinks)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/links", a.createLink)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/links/export", a.exportLinks)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/links/{linkId}", a.getLink)
	mux.HandleFunc("PATCH /api/workspaces/{workspaceId}/links/{linkId}", a.updateLink)
	mux.HandleFunc("DELETE /api/workspaces/{workspaceId}/links/{linkId}", a.deleteLink)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/links/{linkId}/history", a.history)
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (a *API) authenticate(w http.ResponseWriter, r *http.Request, workspaceID string, mutation bool) (actorContext, bool) {
	// P05 cannot pretend P12/P15 are implemented. The only current adapter is
	// explicitly test-only and must be enabled by the process environment.
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
	return fmt.Sprintf("p05-%d", time.Now().UTC().UnixNano())
}

func (a *API) listLinks(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, false)
	if !ok {
		return
	}
	_ = actor

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
	from, err := optionalTime(r.URL.Query().Get("updated_from"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_updated_from", "Invalid updated_from timestamp.")
		return
	}
	to, err := optionalTime(r.URL.Query().Get("updated_to"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_updated_to", "Invalid updated_to timestamp.")
		return
	}
	result, err := a.store.List(r.Context(), ListOptions{
		WorkspaceID: workspaceID,
		Query:       r.URL.Query().Get("q"),
		Hostname:    r.URL.Query().Get("hostname"),
		Status:      r.URL.Query().Get("status"),
		UpdatedFrom: from,
		UpdatedTo:   to,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": result.Items,
		"total": result.Total,
		"filters": map[string]any{
			"implemented": []string{"q", "hostname", "status", "updated_from", "updated_to"},
			"deferred_to_owners": map[string]string{"campaign": "P07/P12", "tag": "P12"},
		},
	})
}

func (a *API) createLink(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	var request createRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.DomainKind == "custom" {
		writeAPIError(w, http.StatusConflict, "domain_unavailable", "Custom-domain binding requires the P06 entitlement and domain-trust authority.")
		return
	}
	if request.RedirectStatus == 0 {
		request.RedirectStatus = http.StatusFound
	}
	created, err := a.store.Create(r.Context(), CreateInput{
		WorkspaceID: workspaceID, ActorID: actor.ActorID, CorrelationID: correlationID(r), ChangeReason: request.ChangeReason,
		Hostname: request.Hostname, DomainKind: request.DomainKind, Code: request.Code, Title: request.Title,
		PrimaryDestination: request.PrimaryDestination, RedirectStatus: request.RedirectStatus, Routing: request.Routing, AB: request.AB,
		UTM: request.UTM, Access: request.Access, ExpiresAt: request.ExpiresAt, ClickLimit: request.ClickLimit, OneTime: request.OneTime,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (a *API) getLink(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	if _, ok := a.authenticate(w, r, workspaceID, false); !ok {
		return
	}
	id, ok := parsePathID(w, r.PathValue("linkId"))
	if !ok {
		return
	}
	link, err := a.store.GetByID(r.Context(), workspaceID, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, link)
}

func (a *API) updateLink(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r.PathValue("linkId"))
	if !ok {
		return
	}
	var request updateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.DomainKind == "custom" {
		writeAPIError(w, http.StatusConflict, "domain_unavailable", "Custom-domain binding requires the P06 entitlement and domain-trust authority.")
		return
	}
	updated, err := a.store.Update(r.Context(), id, UpdateInput{
		WorkspaceID: workspaceID, ActorID: actor.ActorID, CorrelationID: correlationID(r), ChangeReason: request.ChangeReason,
		ExpectedVersion: request.ExpectedVersion, Hostname: request.Hostname, DomainKind: request.DomainKind, Code: request.Code,
		Title: request.Title, PrimaryDestination: request.PrimaryDestination, RedirectStatus: request.RedirectStatus, Status: request.Status,
		Routing: request.Routing, AB: request.AB, UTM: request.UTM, Access: request.Access, ExpiresAt: request.ExpiresAt,
		ClickLimit: request.ClickLimit, OneTime: request.OneTime,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *API) deleteLink(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r.PathValue("linkId"))
	if !ok {
		return
	}
	var request deleteRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := a.store.Delete(r.Context(), workspaceID, id, request.ExpectedVersion, actor.ActorID, correlationID(r), request.ChangeReason); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) history(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	if _, ok := a.authenticate(w, r, workspaceID, false); !ok {
		return
	}
	id, ok := parsePathID(w, r.PathValue("linkId"))
	if !ok {
		return
	}
	versions, err := a.store.History(r.Context(), workspaceID, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": versions})
}

func (a *API) exportLinks(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	if _, ok := a.authenticate(w, r, workspaceID, false); !ok {
		return
	}
	rows, err := a.store.ExportCSVRows(r.Context(), workspaceID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="gojet-links.csv"`)
	writer := csv.NewWriter(w)
	if err := writer.WriteAll(rows); err != nil {
		return
	}
	writer.Flush()
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

func parsePathID(w http.ResponseWriter, value string) (uint64, bool) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_link_id", "Link ID is invalid.")
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

func optionalTime(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidDestination), errors.Is(err, ErrInvalidABWeights):
		writeAPIError(w, http.StatusBadRequest, "invalid_input", "Request validation failed.")
	case errors.Is(err, ErrNotFound):
		writeAPIError(w, http.StatusNotFound, "not_found", "Link not found.")
	case errors.Is(err, ErrConflict):
		writeAPIError(w, http.StatusConflict, "conflict", "The link changed or the requested code is already in use.")
	default:
		writeAPIError(w, http.StatusInternalServerError, "server_error", "The request could not be completed.")
	}
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSONStatus(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	writeJSONStatus(w, status, value)
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
