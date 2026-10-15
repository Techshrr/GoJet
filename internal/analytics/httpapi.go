package analytics

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type API struct {
	store           *Store
	testAuthEnabled bool
	actorResolver   ActorResolver
	enabled         bool
}

type actorContext struct {
	ActorID string
	Role    string
}

type conversionRequest struct {
	ConversionID string    `json:"conversion_id"`
	CampaignID   string    `json:"campaign_id"`
	LinkID       uint64    `json:"link_id"`
	OccurredAt   time.Time `json:"occurred_at"`
}

func NewAPI(store *Store, testAuthEnabled, enabled bool) *API {
	return &API{store: store, testAuthEnabled: testAuthEnabled, enabled: enabled}
}

func NewAPIWithActorResolver(store *Store, resolver ActorResolver, enabled bool) *API {
	return &API{store: store, actorResolver: resolver, enabled: enabled}
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/analytics/overview", a.overview)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/analytics/links/{linkId}", a.linkReport)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/analytics/conversions", a.recordConversion)
	return analyticsSecurityHeaders(mux)
}

func analyticsSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (a *API) authenticate(w http.ResponseWriter, r *http.Request, workspaceID string, mutation bool) (actorContext, bool) {
	if !a.enabled {
		writeAnalyticsError(w, http.StatusServiceUnavailable, "analytics_unavailable", "Analytics is not enabled.")
		return actorContext{}, false
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if a.actorResolver != nil {
		actor, err := a.actorResolver(r, workspaceID)
		if err != nil {
			switch {
			case errors.Is(err, ErrAuthenticationRequired):
				writeAnalyticsError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			case errors.Is(err, ErrForbidden):
				writeAnalyticsError(w, http.StatusForbidden, "forbidden", "Analytics access denied.")
			default:
				writeAnalyticsError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is unavailable.")
			}
			return actorContext{}, false
		}
		actorID := strings.TrimSpace(actor.ActorID)
		role := strings.ToLower(strings.TrimSpace(actor.Role))
		if actorID == "" || workspaceID == "" {
			writeAnalyticsError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is unavailable.")
			return actorContext{}, false
		}
		if role != "owner" && role != "admin" && role != "member" && role != "viewer" {
			writeAnalyticsError(w, http.StatusForbidden, "forbidden", "Analytics access denied.")
			return actorContext{}, false
		}
		if mutation && role == "viewer" {
			writeAnalyticsError(w, http.StatusForbidden, "read_only", "This Workspace role is read-only.")
			return actorContext{}, false
		}
		return actorContext{ActorID: actorID, Role: role}, true
	}

	// Preserve the predecessor P07 test-only adapter for its isolated tests.
	if !a.testAuthEnabled {
		writeAnalyticsError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is not available in this implementation stage.")
		return actorContext{}, false
	}
	actorID := strings.TrimSpace(r.Header.Get("X-GoJet-Test-Actor"))
	role := strings.ToLower(strings.TrimSpace(r.Header.Get("X-GoJet-Test-Workspace-Role")))
	headerWorkspace := strings.TrimSpace(r.Header.Get("X-GoJet-Test-Workspace"))
	permission := strings.ToLower(strings.TrimSpace(r.Header.Get("X-GoJet-Test-Analytics-Permission")))
	if actorID == "" || headerWorkspace == "" || headerWorkspace != workspaceID || permission != "allow" {
		writeAnalyticsError(w, http.StatusForbidden, "forbidden", "Analytics access denied.")
		return actorContext{}, false
	}
	if role != "owner" && role != "admin" && role != "member" && role != "viewer" {
		writeAnalyticsError(w, http.StatusForbidden, "forbidden", "Analytics access denied.")
		return actorContext{}, false
	}
	if mutation && role == "viewer" {
		writeAnalyticsError(w, http.StatusForbidden, "read_only", "This Workspace role is read-only.")
		return actorContext{}, false
	}
	return actorContext{ActorID: actorID, Role: role}, true
}

func (a *API) overview(w http.ResponseWriter, r *http.Request) {
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
	if _, ok := a.authenticate(w, r, workspaceID, false); !ok {
		return
	}
	query, err := queryFromRequest(r, workspaceID, nil)
	if err != nil {
		writeAnalyticsError(w, http.StatusBadRequest, "invalid_query", "Analytics query is invalid.")
		return
	}
	report, err := a.store.QueryReport(r.Context(), query)
	if err != nil {
		writeAnalyticsStoreError(w, err)
		return
	}
	writeAnalyticsJSON(w, http.StatusOK, report)
}

func (a *API) linkReport(w http.ResponseWriter, r *http.Request) {
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
	if _, ok := a.authenticate(w, r, workspaceID, false); !ok {
		return
	}
	linkID, err := strconv.ParseUint(strings.TrimSpace(r.PathValue("linkId")), 10, 64)
	if err != nil || linkID == 0 {
		writeAnalyticsError(w, http.StatusNotFound, "not_found", "Analytics resource not found.")
		return
	}
	query, err := queryFromRequest(r, workspaceID, &linkID)
	if err != nil {
		writeAnalyticsError(w, http.StatusBadRequest, "invalid_query", "Analytics query is invalid.")
		return
	}
	report, err := a.store.QueryReport(r.Context(), query)
	if err != nil {
		writeAnalyticsStoreError(w, err)
		return
	}
	writeAnalyticsJSON(w, http.StatusOK, report)
}

func (a *API) recordConversion(w http.ResponseWriter, r *http.Request) {
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
	if _, ok := a.authenticate(w, r, workspaceID, true); !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request conversionRequest
	if err := decoder.Decode(&request); err != nil {
		writeAnalyticsError(w, http.StatusBadRequest, "invalid_request", "Conversion request is invalid.")
		return
	}
	if request.OccurredAt.IsZero() {
		request.OccurredAt = time.Now().UTC()
	}
	inserted, err := a.store.RecordConversion(r.Context(), Conversion{
		WorkspaceID:  workspaceID,
		ConversionID: request.ConversionID,
		CampaignID:   request.CampaignID,
		LinkID:       request.LinkID,
		OccurredAt:   request.OccurredAt,
	})
	if err != nil {
		writeAnalyticsStoreError(w, err)
		return
	}
	status := http.StatusCreated
	if !inserted {
		status = http.StatusOK
	}
	writeAnalyticsJSON(w, status, map[string]any{
		"conversion_id":        strings.TrimSpace(request.ConversionID),
		"recorded":             inserted,
		"idempotent_duplicate": !inserted,
	})
}

func queryFromRequest(r *http.Request, workspaceID string, linkID *uint64) (Query, error) {
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -7)
	to := now
	var err error
	if raw := strings.TrimSpace(r.URL.Query().Get("from")); raw != "" {
		from, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return Query{}, err
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("to")); raw != "" {
		to, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return Query{}, err
		}
	}
	return Query{
		WorkspaceID:    workspaceID,
		LinkID:         linkID,
		From:           from,
		To:             to,
		Timezone:       r.URL.Query().Get("timezone"),
		Granularity:    r.URL.Query().Get("granularity"),
		CountryCode:    r.URL.Query().Get("country"),
		Device:         r.URL.Query().Get("device"),
		Language:       r.URL.Query().Get("language"),
		SourceHostname: r.URL.Query().Get("source"),
		CampaignID:     r.URL.Query().Get("campaign"),
		Now:            now,
	}, nil
}

func writeAnalyticsStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidQuery), errors.Is(err, ErrInvalidEvent):
		writeAnalyticsError(w, http.StatusBadRequest, "invalid_query", "Analytics request is invalid.")
	case errors.Is(err, ErrAnalyticsNotFound):
		writeAnalyticsError(w, http.StatusNotFound, "not_found", "Analytics resource not found.")
	case errors.Is(err, ErrCampaignAssociation):
		writeAnalyticsError(w, http.StatusConflict, "campaign_not_measured", "Campaign conversion is not associated with measured link activity.")
	default:
		writeAnalyticsError(w, http.StatusServiceUnavailable, "analytics_unavailable", "Analytics is temporarily unavailable.")
	}
}

func writeAnalyticsError(w http.ResponseWriter, status int, code, message string) {
	writeAnalyticsJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":           code,
			"message":        message,
			"correlation_id": fmt.Sprintf("p07-%d", time.Now().UTC().UnixNano()),
		},
	})
}

func writeAnalyticsJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
