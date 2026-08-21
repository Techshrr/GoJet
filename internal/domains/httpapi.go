package domains

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type WorkspaceDomainsAPI struct {
	store           *MySQLStore
	testAuthEnabled bool
}

type createDomainRequest struct {
	Hostname     string `json:"hostname"`
	ChangeReason string `json:"change_reason"`
}

type domainActor struct {
	ActorID string
	Role    string
}

func NewWorkspaceDomainsAPI(store *MySQLStore, testAuthEnabled bool) *WorkspaceDomainsAPI {
	return &WorkspaceDomainsAPI{store: store, testAuthEnabled: testAuthEnabled}
}

func (a *WorkspaceDomainsAPI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/domains", a.list)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/domains", a.create)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/domains/{domainId}", a.detail)
	return domainAPIHeaders(mux)
}

func domainAPIHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (a *WorkspaceDomainsAPI) authenticate(w http.ResponseWriter, r *http.Request, workspaceID string, mutation bool) (domainActor, bool) {
	if !a.testAuthEnabled {
		writeDomainAPIError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is not available in this implementation stage.")
		return domainActor{}, false
	}
	actorID := strings.TrimSpace(r.Header.Get("X-GoJet-Test-Actor"))
	role := strings.ToLower(strings.TrimSpace(r.Header.Get("X-GoJet-Test-Workspace-Role")))
	headerWorkspace := strings.TrimSpace(r.Header.Get("X-GoJet-Test-Workspace"))
	if actorID == "" || role == "" || headerWorkspace == "" || headerWorkspace != workspaceID {
		writeDomainAPIError(w, http.StatusForbidden, "forbidden", "Workspace access denied.")
		return domainActor{}, false
	}
	if role != "owner" && role != "admin" && role != "member" && role != "viewer" {
		writeDomainAPIError(w, http.StatusForbidden, "forbidden", "Workspace access denied.")
		return domainActor{}, false
	}
	if mutation && role == "viewer" {
		writeDomainAPIError(w, http.StatusForbidden, "read_only", "This Workspace role is read-only.")
		return domainActor{}, false
	}
	return domainActor{ActorID: actorID, Role: role}, true
}

func (a *WorkspaceDomainsAPI) list(w http.ResponseWriter, r *http.Request) {
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
	if _, ok := a.authenticate(w, r, workspaceID, false); !ok {
		return
	}
	view, err := a.store.WorkspaceDomainsView(r.Context(), workspaceID, time.Now().UTC())
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeDomainJSON(w, http.StatusOK, view)
}

func (a *WorkspaceDomainsAPI) detail(w http.ResponseWriter, r *http.Request) {
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
	if _, ok := a.authenticate(w, r, workspaceID, false); !ok {
		return
	}
	domainID, err := strconv.ParseUint(strings.TrimSpace(r.PathValue("domainId")), 10, 64)
	if err != nil || domainID == 0 {
		writeDomainAPIError(w, http.StatusBadRequest, "invalid_domain_id", "Domain ID is invalid.")
		return
	}
	view, err := a.store.DomainDetailView(r.Context(), workspaceID, domainID, time.Now().UTC())
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeDomainJSON(w, http.StatusOK, view)
}

func (a *WorkspaceDomainsAPI) create(w http.ResponseWriter, r *http.Request) {
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	var request createDomainRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeDomainAPIError(w, http.StatusBadRequest, "invalid_json", "Request body is invalid.")
		return
	}
	request.ChangeReason = strings.TrimSpace(request.ChangeReason)
	if request.ChangeReason == "" {
		writeDomainAPIError(w, http.StatusBadRequest, "invalid_input", "A change reason is required.")
		return
	}
	created, err := a.store.CreateDomain(r.Context(), CreateDomainInput{
		WorkspaceID: workspaceID,
		ActorID: actor.ActorID,
		CorrelationID: domainCorrelationID(r),
		Reason: request.ChangeReason,
		Hostname: request.Hostname,
		Now: time.Now().UTC(),
	})
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	// Ownership plaintext is intentionally returned only by this creation
	// response. Subsequent list/detail DTOs expose token version/status but never
	// the plaintext TXT value or persisted verifier.
	writeDomainJSON(w, http.StatusCreated, created)
}

func domainCorrelationID(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if value != "" && len(value) <= 128 {
		return value
	}
	return fmt.Sprintf("p06-%d", time.Now().UTC().UnixNano())
}

func writeDomainStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidHostname), errors.Is(err, ErrInvalidDomainMutation):
		writeDomainAPIError(w, http.StatusBadRequest, "invalid_input", "Request validation failed.")
	case errors.Is(err, ErrEntitlementRequired):
		writeDomainAPIError(w, http.StatusConflict, "entitlement_required", "Custom-domain entitlement is not currently active for new changes.")
	case errors.Is(err, ErrDomainLimitReached):
		writeDomainAPIError(w, http.StatusConflict, "domain_limit_reached", "The current custom-domain limit has been reached.")
	case errors.Is(err, ErrHostnameConflict):
		writeDomainAPIError(w, http.StatusConflict, "hostname_unavailable", "The hostname is unavailable.")
	case errors.Is(err, ErrDomainNotFound):
		writeDomainAPIError(w, http.StatusNotFound, "not_found", "Domain not found.")
	default:
		writeDomainAPIError(w, http.StatusInternalServerError, "server_error", "The request could not be completed.")
	}
}

func writeDomainAPIError(w http.ResponseWriter, status int, code, message string) {
	writeDomainJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}

func writeDomainJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
