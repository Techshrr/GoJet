package admin

import (
	"net/http"
	"strconv"
	"strings"
)

func adminListLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 100, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > maxAdminEnumerationLimit {
		return 0, ErrInvalid
	}
	return value, nil
}

func (a *HTTPAPI) managedUsers(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	limit, err := adminListLimit(r)
	if err != nil {
		writeError(w, err)
		return
	}
	items, err := a.service.ListManagedUsers(r.Context(), p, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *HTTPAPI) managedUser(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	item, err := a.service.GetManagedUser(r.Context(), p, r.PathValue("userId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": item})
}

func (a *HTTPAPI) suspendManagedUser(w http.ResponseWriter, r *http.Request) {
	a.mutateManagedUserHTTP(w, r, "suspend")
}

func (a *HTTPAPI) restoreManagedUser(w http.ResponseWriter, r *http.Request) {
	a.mutateManagedUserHTTP(w, r, "restore")
}

func (a *HTTPAPI) mutateManagedUserHTTP(w http.ResponseWriter, r *http.Request, operation string) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	var (
		item     ManagedUser
		replayed bool
		err      error
	)
	if operation == "suspend" {
		item, replayed, err = a.service.SuspendManagedUser(r.Context(), p, r.PathValue("userId"), authority(r, body.Reason), a.now())
	} else {
		item, replayed, err = a.service.RestoreManagedUser(r.Context(), p, r.PathValue("userId"), authority(r, body.Reason), a.now())
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": item, "replayed": replayed})
}

func (a *HTTPAPI) managedWorkspaces(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	limit, err := adminListLimit(r)
	if err != nil {
		writeError(w, err)
		return
	}
	items, err := a.service.ListManagedWorkspaces(r.Context(), p, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *HTTPAPI) managedWorkspace(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	item, err := a.service.GetManagedWorkspace(r.Context(), p, r.PathValue("workspaceId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspace": item})
}

func (a *HTTPAPI) suspendManagedWorkspace(w http.ResponseWriter, r *http.Request) {
	a.mutateManagedWorkspaceHTTP(w, r, "suspend")
}

func (a *HTTPAPI) restoreManagedWorkspace(w http.ResponseWriter, r *http.Request) {
	a.mutateManagedWorkspaceHTTP(w, r, "restore")
}

func (a *HTTPAPI) mutateManagedWorkspaceHTTP(w http.ResponseWriter, r *http.Request, operation string) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	var (
		item     ManagedWorkspace
		replayed bool
		err      error
	)
	if operation == "suspend" {
		item, replayed, err = a.service.SuspendManagedWorkspace(r.Context(), p, r.PathValue("workspaceId"), authority(r, body.Reason), a.now())
	} else {
		item, replayed, err = a.service.RestoreManagedWorkspace(r.Context(), p, r.PathValue("workspaceId"), authority(r, body.Reason), a.now())
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspace": item, "replayed": replayed})
}
