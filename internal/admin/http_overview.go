package admin

import "net/http"

type AdminOverview struct {
	Administrators      int64 `json:"administrators"`
	Users               int64 `json:"users"`
	Workspaces          int64 `json:"workspaces"`
	AuditEvents         int64 `json:"audit_events"`
	RetryOrFailedJobs   int64 `json:"retry_or_failed_jobs"`
}

func (a *HTTPAPI) overview(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	if err := a.service.Require(p, PermissionPlatformRead); err != nil {
		writeError(w, err)
		return
	}
	var result AdminOverview
	err := a.service.db.QueryRowContext(r.Context(), `
SELECT
  (SELECT COUNT(*) FROM admin_administrators),
  (SELECT COUNT(*) FROM auth_users),
  (SELECT COUNT(*) FROM workspaces),
  (SELECT COUNT(*) FROM admin_audit_events),
  (SELECT COUNT(*) FROM destination_risk_scans WHERE status IN ('retry','failed'))`).Scan(
		&result.Administrators,
		&result.Users,
		&result.Workspaces,
		&result.AuditEvents,
		&result.RetryOrFailedJobs,
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"overview": result})
}
