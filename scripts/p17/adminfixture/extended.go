package adminfixture

import (
	"net/http"
	"net/http/httptest"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
)

// NewExtendedHTTPServer mounts the established P17 access routes plus the
// resource/file/operations governance contribution under the production
// HTTPAPI handlers. It is evidence infrastructure only.
func NewExtendedHTTPServer(service *adminaccess.Service, operations *adminaccess.OperationsGovernance) (*httptest.Server, error) {
	api, err := adminaccess.NewHTTPAPI(service)
	if err != nil {
		return nil, err
	}
	root := http.NewServeMux()
	root.Handle("/", api.Handler())
	extended := api.ExtendedGovernanceHandler(operations)
	for _, pattern := range []string{
		"GET /api/admin/links",
		"GET /api/admin/links/{linkId}",
		"GET /api/admin/domains",
		"GET /api/admin/domains/{domainId}",
		"GET /api/admin/resources/{resourceKind}",
		"GET /api/admin/resources/{resourceKind}/{resourceId}",
		"GET /api/admin/files",
		"GET /api/admin/files/{fileId}",
		"POST /api/admin/files/{fileId}/quarantine",
		"POST /api/admin/files/{fileId}/rescan",
		"POST /api/admin/files/{fileId}/restore",
		"POST /api/admin/files/{fileId}/delete",
		"POST /api/admin/files/{fileId}/expiry",
		"GET /api/admin/operations/jobs",
		"POST /api/admin/operations/jobs/{jobId}/requeue",
		"GET /api/admin/operations/services",
		"POST /api/admin/operations/services/{serviceId}/restart",
	} {
		root.Handle(pattern, extended)
	}
	return httptest.NewServer(root), nil
}
