package workspace

import (
	"net/http"
	"strconv"
	"strings"
)

func (a *API) listNotifications(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, false)
	if !ok {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 100 {
			writeWorkspaceError(w, r, http.StatusBadRequest, "invalid_limit", "Invalid list limit.")
			return
		}
		limit = parsed
	}
	page, err := a.store.ListNotifications(r.Context(), actor.WorkspaceID, p.UserID, r.URL.Query().Get("category"), limit)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *API) readNotification(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	id, ok := pathUint64(w, r, "notificationId")
	if !ok {
		return
	}
	err := a.store.SetNotificationRead(r.Context(), actor.WorkspaceID, p.UserID, id, true)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "read": true})
}

func (a *API) unreadNotification(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	id, ok := pathUint64(w, r, "notificationId")
	if !ok {
		return
	}
	err := a.store.SetNotificationRead(r.Context(), actor.WorkspaceID, p.UserID, id, false)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "read": false})
}

func (a *API) readAllNotifications(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	count, err := a.store.MarkAllNotificationsRead(r.Context(), actor.WorkspaceID, p.UserID)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": count})
}
