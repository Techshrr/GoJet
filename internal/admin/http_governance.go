package admin

import (
	"net/http"
	"strconv"
	"strings"
)

func (a *HTTPAPI) permissions(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	if err := a.service.Require(p, PermissionAdminsManage); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": append([]string(nil), PermissionCatalog...)})
}
func (a *HTTPAPI) roles(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	items, err := a.service.ListRoles(r.Context(), p)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (a *HTTPAPI) createRole(w http.ResponseWriter, r *http.Request) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
		Reason      string   `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	role, replayed, err := a.service.CreateRole(r.Context(), p, CreateRoleInput{Name: body.Name, Description: body.Description, Permissions: body.Permissions}, authority(r, body.Reason), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"role": role, "replayed": replayed})
}
func (a *HTTPAPI) administrators(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	items, err := a.service.ListAdministrators(r.Context(), p)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (a *HTTPAPI) createAdministrator(w http.ResponseWriter, r *http.Request) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		Email       string   `json:"email"`
		DisplayName string   `json:"display_name"`
		Password    string   `json:"password"`
		RoleIDs     []string `json:"role_ids"`
		Reason      string   `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	item, replayed, err := a.service.CreateAdministrator(r.Context(), p, CreateAdministratorInput{Email: body.Email, DisplayName: body.DisplayName, Password: body.Password, RoleIDs: body.RoleIDs}, authority(r, body.Reason), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"administrator": item, "replayed": replayed})
}
func (a *HTTPAPI) audit(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(w, ErrInvalid)
			return
		}
		limit = parsed
	}
	items, err := a.service.ListAudit(r.Context(), p, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": redactAuditEventsForResponse(items)})
}
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
