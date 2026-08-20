package links

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

type restoreRequest struct {
	ExpectedVersion uint64 `json:"expected_version"`
	RestoreVersion  uint64 `json:"restore_version"`
	ChangeReason    string `json:"change_reason"`
}

type bulkItem struct {
	ID      uint64 `json:"id"`
	Version uint64 `json:"version"`
}

type bulkRequest struct {
	Action       string     `json:"action"`
	Items        []bulkItem `json:"items"`
	ChangeReason string     `json:"change_reason"`
}

type bulkItemResult struct {
	ID      uint64 `json:"id"`
	Status  string `json:"status"`
	Version uint64 `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

// FullHandler retains the P05 command surface without a risk dependency. The
// risk endpoint itself fails closed until a RedisRiskStore is supplied.
func (a *API) FullHandler() http.Handler {
	return a.FullHandlerWithRisk(nil)
}

// FullHandlerWithRisk is the authoritative P05 platform API surface. Applying
// securityHeaders to the outer mux guarantees bulk/restore/risk routes receive
// the same no-store/noindex/nosniff contract as the core CRUD routes.
func (a *API) FullHandlerWithRisk(risk *RedisRiskStore) http.Handler {
	extended := http.NewServeMux()
	extended.HandleFunc("POST /api/workspaces/{workspaceId}/links/bulk", a.bulkLinks)
	extended.HandleFunc("POST /api/workspaces/{workspaceId}/links/{linkId}/restore", a.restoreLink)
	extended.HandleFunc("GET /api/workspaces/{workspaceId}/links/{linkId}/risk", func(w http.ResponseWriter, r *http.Request) {
		a.linkRisk(w, r, risk)
	})
	extended.Handle("/", a.Handler())
	return securityHeaders(extended)
}

func (a *API) linkRisk(w http.ResponseWriter, r *http.Request, risk *RedisRiskStore) {
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
	if risk == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "risk_dependency_unavailable", "Destination-risk state is unavailable.")
		return
	}
	_, state, err := risk.Resolve(r.Context(), link.ID, link.RiskFingerprint, time.Now().UTC())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "risk_dependency_unavailable", "Destination-risk state is unavailable.")
		return
	}
	// Do not expose provider evidence, thresholds, policy internals or targets.
	writeJSON(w, http.StatusOK, map[string]any{
		"link_id": link.ID,
		"fingerprint": link.RiskFingerprint,
		"state": state,
		"current": true,
	})
}

func (a *API) restoreLink(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r.PathValue("linkId"))
	if !ok {
		return
	}
	var request restoreRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	restored, err := a.store.Restore(r.Context(), id, RestoreInput{
		WorkspaceID: workspaceID,
		ActorID: actor.ActorID,
		CorrelationID: correlationID(r),
		ChangeReason: request.ChangeReason,
		ExpectedVersion: request.ExpectedVersion,
		RestoreVersion: request.RestoreVersion,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, restored)
}

func (a *API) bulkLinks(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	var request bulkRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action != "pause" && action != "activate" && action != "delete" {
		writeAPIError(w, http.StatusBadRequest, "unsupported_bulk_action", "Bulk action is not owned by P05 or is invalid.")
		return
	}
	if len(request.Items) == 0 || len(request.Items) > 100 || strings.TrimSpace(request.ChangeReason) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_bulk_request", "Bulk request is invalid.")
		return
	}

	results := make([]bulkItemResult, 0, len(request.Items))
	for _, item := range request.Items {
		result := bulkItemResult{ID: item.ID}
		if item.ID == 0 || item.Version == 0 {
			result.Status = "failed"
			result.Error = "invalid_item"
			results = append(results, result)
			continue
		}
		correlation := correlationID(r) + "-" + strings.TrimSpace(request.Action)
		switch action {
		case "pause", "activate":
			status := "paused"
			if action == "activate" {
				status = "active"
			}
			updated, err := a.store.SetStatus(r.Context(), workspaceID, item.ID, item.Version, status, actor.ActorID, correlation, request.ChangeReason)
			if err != nil {
				result.Status, result.Error = bulkError(err)
			} else {
				result.Status = "success"
				result.Version = updated.Version
			}
		case "delete":
			err := a.store.Delete(r.Context(), workspaceID, item.ID, item.Version, actor.ActorID, correlation, request.ChangeReason)
			if err != nil {
				result.Status, result.Error = bulkError(err)
			} else {
				result.Status = "success"
				result.Version = item.Version + 1
			}
		}
		results = append(results, result)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"action": action,
		"results": results,
		"unsupported_until_owner_nodes": []string{"tag:P12", "folder:P12"},
	})
}

func bulkError(err error) (string, string) {
	switch {
	case errors.Is(err, ErrConflict):
		return "conflict", "stale_version_or_code_conflict"
	case errors.Is(err, ErrNotFound):
		return "failed", "not_found"
	case errors.Is(err, ErrInvalidInput):
		return "failed", "invalid_input"
	default:
		return "failed", "server_error"
	}
}
