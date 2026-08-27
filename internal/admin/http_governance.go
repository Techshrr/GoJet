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
	if err := a.service.Require(p, PermissionPlatformRead); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": append([]string(nil), PermissionCatalog...)})
}

func (a *HTTPAPI) roles(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	roles, err := a.service.ListRoles(r.Context(), p)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

func (a *HTTPAPI) createRole(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok || !a.mutationGuard(w, r, p) {
		return
	}
	var input CreateRoleInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, err)
		return
	}
	reason := strings.TrimSpace(r.Header.Get("X-Admin-Reason"))
	if input.Reason != "" && reason != "" && strings.TrimSpace(input.Reason) != reason {
		writeError(w, ErrInvalid)
		return
	}
	if reason == "" {
		reason = strings.TrimSpace(input.Reason)
	}
	role, replayed, err := a.service.CreateRole(r.Context(), p, input, MutationAuthority{Reason: reason, CorrelationID: r.Header.Get("X-Correlation-ID"), IdempotencyKey: r.Header.Get("Idempotency-Key")}, a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"role": role, "replayed": replayed})
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
	writeJSON(w, http.StatusOK, map[string]any{"administrators": items})
}

func (a *HTTPAPI) createAdministrator(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok || !a.mutationGuard(w, r, p) {
		return
	}
	var input CreateAdministratorInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, err)
		return
	}
	reason := strings.TrimSpace(r.Header.Get("X-Admin-Reason"))
	if input.Reason != "" && reason != "" && strings.TrimSpace(input.Reason) != reason {
		writeError(w, ErrInvalid)
		return
	}
	if reason == "" {
		reason = strings.TrimSpace(input.Reason)
	}
	administrator, replayed, err := a.service.CreateAdministrator(r.Context(), p, input, MutationAuthority{Reason: reason, CorrelationID: r.Header.Get("X-Correlation-ID"), IdempotencyKey: r.Header.Get("Idempotency-Key")}, a.now())
	input.Password = ""
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"administrator": administrator, "replayed": replayed})
}

func (a *HTTPAPI) audit(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 500 {
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
	writeJSON(w, http.StatusOK, map[string]any{"events": redactAuditEventsForResponse(items)})
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
