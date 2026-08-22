package files

import (
	"net/http"
	"strings"
)

type HealthAPI struct {
	authority       *HealthAuthority
	testAuthEnabled bool
}

func NewHealthAPI(authority *HealthAuthority, testAuthEnabled bool) (*HealthAPI, error) {
	if authority == nil {
		return nil, ErrInvalidInput
	}
	return &HealthAPI{authority: authority, testAuthEnabled: testAuthEnabled}, nil
}

func (a *HealthAPI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/platform/storage", a.status)
	return fileSecurityHeaders(mux)
}

func (a *HealthAPI) status(w http.ResponseWriter, r *http.Request) {
	if !a.testAuthEnabled {
		writeFileAPIError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is not available in this implementation stage.")
		return
	}
	actorID := strings.TrimSpace(r.Header.Get("X-GoJet-Test-Actor"))
	role := strings.ToLower(strings.TrimSpace(r.Header.Get("X-GoJet-Test-Admin-Role")))
	if actorID == "" || role != "admin" {
		writeFileAPIError(w, http.StatusForbidden, "forbidden", "Admin access denied.")
		return
	}
	writeFileJSON(w, http.StatusOK, a.authority.Check(r.Context()))
}
