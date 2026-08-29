package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *HTTPAPI) ExtendedGovernanceHandler(operations *OperationsGovernance) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/links", a.managedLinks)
	mux.HandleFunc("GET /api/admin/links/{linkId}", a.managedLink)
	mux.HandleFunc("GET /api/admin/domains", a.managedDomains)
	mux.HandleFunc("GET /api/admin/domains/{domainId}", a.managedDomain)
	mux.HandleFunc("GET /api/admin/resources/{resourceKind}", a.managedContentResources)
	mux.HandleFunc("GET /api/admin/resources/{resourceKind}/{resourceId}", a.managedContentResource)
	mux.HandleFunc("GET /api/admin/files", a.managedFiles)
	mux.HandleFunc("GET /api/admin/files/{fileId}", a.managedFile)
	mux.HandleFunc("POST /api/admin/files/{fileId}/quarantine", a.quarantineManagedFile)
	mux.HandleFunc("POST /api/admin/files/{fileId}/rescan", a.rescanManagedFile)
	mux.HandleFunc("POST /api/admin/files/{fileId}/restore", a.restoreManagedFile)
	mux.HandleFunc("POST /api/admin/files/{fileId}/delete", a.deleteManagedFile)
	mux.HandleFunc("POST /api/admin/files/{fileId}/expiry", a.expireManagedFile)
	if operations != nil {
		mux.HandleFunc("GET /api/admin/operations/jobs", func(w http.ResponseWriter, r *http.Request) { a.operationJobs(w, r, operations) })
		mux.HandleFunc("POST /api/admin/operations/jobs/{jobId}/requeue", func(w http.ResponseWriter, r *http.Request) { a.requeueOperationJob(w, r, operations) })
		mux.HandleFunc("GET /api/admin/operations/services", func(w http.ResponseWriter, r *http.Request) { a.runtimeServices(w, r, operations) })
		mux.HandleFunc("POST /api/admin/operations/services/{serviceId}/restart", func(w http.ResponseWriter, r *http.Request) { a.restartRuntimeService(w, r, operations) })
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { adminHeaders(w.Header()); mux.ServeHTTP(w, r) })
}

func adminLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 100, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, ErrInvalid
	}
	return normalizeAdminEnumerationLimit(value)
}

func pathUint64(r *http.Request, name string) (uint64, error) {
	value, err := strconv.ParseUint(strings.TrimSpace(r.PathValue(name)), 10, 64)
	if err != nil || value == 0 {
		return 0, ErrInvalid
	}
	return value, nil
}

func (a *HTTPAPI) managedLinks(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	limit, err := adminLimit(r)
	if err != nil {
		writeError(w, err)
		return
	}
	items, err := a.service.ListManagedLinks(r.Context(), p, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (a *HTTPAPI) managedLink(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	id, err := pathUint64(r, "linkId")
	if err != nil {
		writeError(w, err)
		return
	}
	item, err := a.service.GetManagedLink(r.Context(), p, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"link": item})
}
func (a *HTTPAPI) managedDomains(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	limit, err := adminLimit(r)
	if err != nil {
		writeError(w, err)
		return
	}
	items, err := a.service.ListManagedDomains(r.Context(), p, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (a *HTTPAPI) managedDomain(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	id, err := pathUint64(r, "domainId")
	if err != nil {
		writeError(w, err)
		return
	}
	item, err := a.service.GetManagedDomain(r.Context(), p, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domain": item})
}
func (a *HTTPAPI) managedContentResources(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	limit, err := adminLimit(r)
	if err != nil {
		writeError(w, err)
		return
	}
	items, err := a.service.ListManagedContentResources(r.Context(), p, r.PathValue("resourceKind"), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (a *HTTPAPI) managedContentResource(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	id, err := pathUint64(r, "resourceId")
	if err != nil {
		writeError(w, err)
		return
	}
	item, err := a.service.GetManagedContentResource(r.Context(), p, r.PathValue("resourceKind"), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resource": item})
}

func (a *HTTPAPI) managedFiles(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	limit, err := adminLimit(r)
	if err != nil {
		writeError(w, err)
		return
	}
	items, err := a.service.ListManagedFiles(r.Context(), p, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (a *HTTPAPI) managedFile(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	id, err := pathUint64(r, "fileId")
	if err != nil {
		writeError(w, err)
		return
	}
	item, err := a.service.GetManagedFile(r.Context(), p, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"file": item})
}

type adminReasonBody struct {
	Reason string `json:"reason"`
}

type adminOperationBody struct {
	Reason             string `json:"reason"`
	ImpactConfirmation string `json:"impact_confirmation"`
}

func (a *HTTPAPI) mutateFile(w http.ResponseWriter, r *http.Request, operation string) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	id, err := pathUint64(r, "fileId")
	if err != nil {
		writeError(w, err)
		return
	}
	var body adminReasonBody
	if !decodeJSON(w, r, &body) {
		return
	}
	var item ManagedFile
	var replay bool
	switch operation {
	case "quarantine":
		item, replay, err = a.service.QuarantineManagedFile(r.Context(), p, id, authority(r, body.Reason), a.now())
	case "rescan":
		item, replay, err = a.service.RescanManagedFile(r.Context(), p, id, authority(r, body.Reason), a.now())
	case "restore":
		item, replay, err = a.service.RestoreManagedFile(r.Context(), p, id, authority(r, body.Reason), a.now())
	case "delete":
		item, replay, err = a.service.DeleteManagedFile(r.Context(), p, id, authority(r, body.Reason), a.now())
	default:
		err = ErrInvalid
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"file": item, "replay": replay})
}
func (a *HTTPAPI) quarantineManagedFile(w http.ResponseWriter, r *http.Request) {
	a.mutateFile(w, r, "quarantine")
}
func (a *HTTPAPI) rescanManagedFile(w http.ResponseWriter, r *http.Request) {
	a.mutateFile(w, r, "rescan")
}
func (a *HTTPAPI) restoreManagedFile(w http.ResponseWriter, r *http.Request) {
	a.mutateFile(w, r, "restore")
}
func (a *HTTPAPI) deleteManagedFile(w http.ResponseWriter, r *http.Request) {
	a.mutateFile(w, r, "delete")
}
func (a *HTTPAPI) expireManagedFile(w http.ResponseWriter, r *http.Request) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	id, err := pathUint64(r, "fileId")
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Reason    string     `json:"reason"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	item, replay, err := a.service.SetManagedFileExpiry(r.Context(), p, id, body.ExpiresAt, authority(r, body.Reason), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"file": item, "replay": replay})
}
func (a *HTTPAPI) operationJobs(w http.ResponseWriter, r *http.Request, o *OperationsGovernance) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	limit, err := adminLimit(r)
	if err != nil {
		writeError(w, err)
		return
	}
	items, err := o.ListJobs(r.Context(), p, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (a *HTTPAPI) requeueOperationJob(w http.ResponseWriter, r *http.Request, o *OperationsGovernance) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	id, err := pathUint64(r, "jobId")
	if err != nil {
		writeError(w, err)
		return
	}
	var body adminOperationBody
	if !decodeJSON(w, r, &body) {
		return
	}
	item, replay, err := o.RequeueJob(r.Context(), p, id, body.ImpactConfirmation, authority(r, body.Reason), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": item, "replay": replay})
}
func (a *HTTPAPI) runtimeServices(w http.ResponseWriter, r *http.Request, o *OperationsGovernance) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	items, err := o.ListServices(r.Context(), p)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (a *HTTPAPI) restartRuntimeService(w http.ResponseWriter, r *http.Request, o *OperationsGovernance) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var body adminOperationBody
	if !decodeJSON(w, r, &body) {
		return
	}
	item, replay, err := o.RestartService(r.Context(), p, r.PathValue("serviceId"), body.ImpactConfirmation, authority(r, body.Reason), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"service": item, "replay": replay})
}
