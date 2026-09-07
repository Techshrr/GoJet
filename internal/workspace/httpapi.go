package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type PrincipalResolver func(*http.Request) (Principal, error)

type API struct {
	store             *Store
	testAuthEnabled   bool
	principalResolver PrincipalResolver
}

func NewAPI(store *Store, testAuthEnabled bool) *API {
	return &API{store: store, testAuthEnabled: testAuthEnabled}
}

func NewAPIWithPrincipalResolver(store *Store, resolver PrincipalResolver) *API {
	return &API{store: store, principalResolver: resolver}
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/workspaces", a.listWorkspaces)
	mux.HandleFunc("POST /api/workspaces", a.createWorkspace)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}", a.getWorkspace)
	mux.HandleFunc("PATCH /api/workspaces/{workspaceId}", a.patchWorkspace)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/overview", a.overview)

	mux.HandleFunc("GET /api/workspaces/{workspaceId}/members", a.listMembers)
	mux.HandleFunc("PATCH /api/workspaces/{workspaceId}/members/{memberId}", a.patchMember)
	mux.HandleFunc("DELETE /api/workspaces/{workspaceId}/members/{memberId}", a.deleteMember)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/invitations", a.listInvitations)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/invitations", a.createInvitation)
	mux.HandleFunc("DELETE /api/workspaces/{workspaceId}/invitations/{invitationId}", a.revokeInvitation)
	mux.HandleFunc("GET /api/invitations/{token}", a.inspectInvitation)
	mux.HandleFunc("POST /api/invitations/accept", a.acceptInvitation)
	mux.HandleFunc("POST /api/invitations/reject", a.rejectInvitation)

	mux.HandleFunc("GET /api/workspaces/{workspaceId}/organization", a.getOrganization)
	mux.HandleFunc("PATCH /api/workspaces/{workspaceId}/organization", a.patchOrganization)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/campaigns", a.listCampaigns)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/campaigns", a.createCampaign)
	mux.HandleFunc("PATCH /api/workspaces/{workspaceId}/campaigns/{campaignId}", a.patchCampaign)
	mux.HandleFunc("DELETE /api/workspaces/{workspaceId}/campaigns/{campaignId}", a.deleteCampaign)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/tags", a.listTags)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/tags", a.createTag)
	mux.HandleFunc("PATCH /api/workspaces/{workspaceId}/tags/{tagId}", a.patchTag)
	mux.HandleFunc("DELETE /api/workspaces/{workspaceId}/tags/{tagId}", a.deleteTag)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/folders", a.listFolders)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/folders", a.createFolder)
	mux.HandleFunc("PATCH /api/workspaces/{workspaceId}/folders/{folderId}", a.patchFolder)
	mux.HandleFunc("DELETE /api/workspaces/{workspaceId}/folders/{folderId}", a.deleteFolder)
	mux.HandleFunc("PATCH /api/workspaces/{workspaceId}/links/organization", a.patchLinkOrganization)

	mux.HandleFunc("GET /api/workspaces/{workspaceId}/notifications", a.listNotifications)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/notifications/{notificationId}/read", a.readNotification)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/notifications/{notificationId}/unread", a.unreadNotification)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/notifications/read-all", a.readAllNotifications)
	return workspaceSecurityHeaders(mux)
}

func workspaceSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (a *API) principal(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	if a.principalResolver != nil {
		p, err := a.principalResolver(r)
		if err != nil {
			switch {
			case errors.Is(err, ErrAuthenticationRequired):
				writeWorkspaceError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			case errors.Is(err, ErrForbidden):
				writeWorkspaceError(w, r, http.StatusForbidden, "forbidden", "Workspace access denied.")
			default:
				writeWorkspaceError(w, r, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is unavailable.")
			}
			return Principal{}, false
		}
		p.UserID = strings.TrimSpace(p.UserID)
		p.Email = normalizeEmail(p.Email)
		p.DisplayName = strings.TrimSpace(p.DisplayName)
		if p.UserID == "" || p.Email == "" {
			writeWorkspaceError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return Principal{}, false
		}
		return p, true
	}
	if !a.testAuthEnabled {
		writeWorkspaceError(w, r, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is not available in this implementation stage.")
		return Principal{}, false
	}
	p := Principal{
		UserID:      strings.TrimSpace(r.Header.Get("X-GoJet-Test-Actor")),
		Email:       normalizeEmail(r.Header.Get("X-GoJet-Test-Email")),
		DisplayName: strings.TrimSpace(r.Header.Get("X-GoJet-Test-Display-Name")),
	}
	if p.UserID == "" || p.Email == "" {
		writeWorkspaceError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return Principal{}, false
	}
	return p, true
}

func (a *API) workspaceActor(w http.ResponseWriter, r *http.Request, mutation bool) (Principal, Membership, bool) {
	p, ok := a.principal(w, r)
	if !ok {
		return Principal{}, Membership{}, false
	}
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
	if workspaceID == "" {
		writeWorkspaceError(w, r, http.StatusBadRequest, "invalid_workspace", "Invalid Workspace.")
		return Principal{}, Membership{}, false
	}
	m, err := a.store.GetMembership(r.Context(), workspaceID, p.UserID)
	if err != nil {
		if mutation {
			_ = a.store.WriteAudit(r.Context(), AuditEvent{
				WorkspaceID: workspaceID, ActorID: p.UserID, Action: "workspace.authorization",
				ResourceType: "workspace", ResourceID: workspaceID, RequestCorrelationID: requestCorrelationID(r),
				Result: "denied", MetadataJSON: "{}",
			})
		}
		writeWorkspaceStoreError(w, r, err)
		return Principal{}, Membership{}, false
	}
	return p, m, true
}

func requireRole(w http.ResponseWriter, r *http.Request, m Membership, allowed ...string) bool {
	for _, role := range allowed {
		if m.Role == role {
			return true
		}
	}
	writeWorkspaceError(w, r, http.StatusForbidden, "forbidden", "Workspace access denied.")
	return false
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeWorkspaceError(w, r, http.StatusBadRequest, "invalid_json", "Invalid request body.")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeWorkspaceError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":           code,
			"message":        message,
			"correlation_id": requestCorrelationID(r),
		},
	})
}

func writeWorkspaceStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		writeWorkspaceError(w, r, http.StatusBadRequest, "invalid_request", "Invalid request.")
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrAccountMatch):
		writeWorkspaceError(w, r, http.StatusForbidden, "forbidden", "Workspace access denied.")
	case errors.Is(err, ErrNotFound):
		writeWorkspaceError(w, r, http.StatusNotFound, "not_found", "Resource not found.")
	case errors.Is(err, ErrInviteExpired):
		writeWorkspaceError(w, r, http.StatusGone, "invitation_expired", "Invitation has expired.")
	case errors.Is(err, ErrConflict):
		writeWorkspaceError(w, r, http.StatusConflict, "conflict", "Resource changed or already exists.")
	case errors.Is(err, ErrLastOwner):
		writeWorkspaceError(w, r, http.StatusConflict, "last_owner_protected", "The last Workspace owner cannot be removed or demoted.")
	case errors.Is(err, ErrInUse):
		writeWorkspaceError(w, r, http.StatusConflict, "resource_in_use", "Resource is currently in use.")
	case errors.Is(err, ErrInviteState):
		writeWorkspaceError(w, r, http.StatusConflict, "invitation_not_pending", "Invitation is no longer pending.")
	default:
		writeWorkspaceError(w, r, http.StatusInternalServerError, "server_error", "Request could not be completed.")
	}
}

func requestCorrelationID(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if value != "" && len(value) <= 128 {
		return value
	}
	return fmt.Sprintf("p12-%d", time.Now().UTC().UnixNano())
}

func auditReason(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	value = redactNotificationText(value)
	if len(value) > 500 {
		value = value[:500]
	}
	return &value
}

func auditResultFor(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, ErrConflict), errors.Is(err, ErrLastOwner), errors.Is(err, ErrInUse), errors.Is(err, ErrInviteState):
		return "conflict"
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrAccountMatch):
		return "denied"
	default:
		return "failed"
	}
}

func (a *API) audit(r *http.Request, workspaceID, actorID, action, resourceType, resourceID, reason string, err error) {
	if workspaceID == "" || actorID == "" {
		return
	}
	_ = a.store.WriteAudit(r.Context(), AuditEvent{
		WorkspaceID:          workspaceID,
		ActorID:              actorID,
		Action:               action,
		ResourceType:         resourceType,
		ResourceID:           resourceID,
		Reason:               auditReason(reason),
		RequestCorrelationID: requestCorrelationID(r),
		Result:               auditResultFor(err),
		MetadataJSON:         "{}",
	})
}

func pathUint64(w http.ResponseWriter, r *http.Request, name string) (uint64, bool) {
	value, err := strconv.ParseUint(strings.TrimSpace(r.PathValue(name)), 10, 64)
	if err != nil || value == 0 {
		writeWorkspaceError(w, r, http.StatusBadRequest, "invalid_id", "Invalid resource identifier.")
		return 0, false
	}
	return value, true
}

func (a *API) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	items, err := a.store.ListWorkspaces(r.Context(), p.UserID)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) createWorkspace(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	ws, membership, err := a.store.CreateWorkspace(r.Context(), p, input.Name)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	a.audit(r, ws.ID, p.UserID, "workspace.create", "workspace", ws.ID, "", nil)
	writeJSON(w, http.StatusCreated, map[string]any{"workspace": ws, "membership": membership})
}

func (a *API) getWorkspace(w http.ResponseWriter, r *http.Request) {
	_, m, ok := a.workspaceActor(w, r, false)
	if !ok {
		return
	}
	ws, err := a.store.GetWorkspace(r.Context(), m.WorkspaceID)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspace": ws, "membership": m})
}

func (a *API) patchWorkspace(w http.ResponseWriter, r *http.Request) {
	p, m, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, m, RoleOwner, RoleAdmin) {
		a.audit(r, m.WorkspaceID, p.UserID, "workspace.update", "workspace", m.WorkspaceID, "", ErrForbidden)
		return
	}
	var input struct {
		Name            string `json:"name"`
		ExpectedVersion uint64 `json:"expected_version"`
		Reason          string `json:"reason"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	ws, err := a.store.UpdateWorkspace(r.Context(), m.WorkspaceID, input.Name, input.ExpectedVersion)
	a.audit(r, m.WorkspaceID, p.UserID, "workspace.update", "workspace", m.WorkspaceID, input.Reason, err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ws)
}

func (a *API) overview(w http.ResponseWriter, r *http.Request) {
	p, m, ok := a.workspaceActor(w, r, false)
	if !ok {
		return
	}
	ws, err := a.store.GetWorkspace(r.Context(), m.WorkspaceID)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	var members, campaigns, tags, folders, unread uint64
	queries := []struct {
		q string
		v *uint64
	}{
		{"SELECT COUNT(*) FROM workspace_memberships WHERE workspace_id=?", &members},
		{"SELECT COUNT(*) FROM workspace_campaigns WHERE workspace_id=? AND status='active'", &campaigns},
		{"SELECT COUNT(*) FROM workspace_tags WHERE workspace_id=?", &tags},
		{"SELECT COUNT(*) FROM workspace_folders WHERE workspace_id=?", &folders},
		{"SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id=? AND recipient_user_id=? AND read_at IS NULL", &unread},
	}
	for i, item := range queries {
		var err error
		if i == len(queries)-1 {
			err = a.store.db.QueryRowContext(r.Context(), item.q, m.WorkspaceID, p.UserID).Scan(item.v)
		} else {
			err = a.store.db.QueryRowContext(r.Context(), item.q, m.WorkspaceID).Scan(item.v)
		}
		if err != nil {
			writeWorkspaceStoreError(w, r, err)
			return
		}
	}
	state, err := a.store.GetNotificationState(r.Context(), m.WorkspaceID)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workspace":  ws,
		"membership": m,
		"counts": map[string]uint64{
			"members": members, "campaigns": campaigns, "tags": tags, "folders": folders, "unread_notifications": unread,
		},
		"notification_state": state,
	})
}
