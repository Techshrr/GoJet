package files

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

type API struct {
	store            *ResourceStore
	storage          *NativeStorage
	policy           *TypePolicy
	testAuthEnabled  bool
	maxUploadBytes   int64
	publicAuthSecret []byte
}

type actorContext struct {
	ActorID string
	Role    string
}

type changeRequest struct {
	ChangeReason string `json:"change_reason"`
}

type policyPatchRequest struct {
	Password            *string    `json:"password"`
	ClearPassword       bool       `json:"clear_password"`
	ExpiresAt           *time.Time `json:"expires_at"`
	ClearExpiresAt      bool       `json:"clear_expires_at"`
	RetentionUntil      *time.Time `json:"retention_until"`
	ClearRetentionUntil bool       `json:"clear_retention_until"`
	DownloadLimit       *uint64    `json:"download_limit"`
	ClearDownloadLimit  bool       `json:"clear_download_limit"`
	ChangeReason        string     `json:"change_reason"`
}

func NewAPI(store *ResourceStore, storage *NativeStorage, policy *TypePolicy, testAuthEnabled bool, maxUploadBytes int64, publicAuthSecret []byte) (*API, error) {
	if store == nil || storage == nil || policy == nil || maxUploadBytes <= 0 || len(publicAuthSecret) < 32 {
		return nil, ErrInvalidInput
	}
	return &API{
		store: store, storage: storage, policy: policy, testAuthEnabled: testAuthEnabled,
		maxUploadBytes: maxUploadBytes, publicAuthSecret: append([]byte(nil), publicAuthSecret...),
	}, nil
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/files", a.list)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/files", a.create)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/files/{fileId}", a.get)
	mux.HandleFunc("PATCH /api/workspaces/{workspaceId}/files/{fileId}", a.patch)
	mux.HandleFunc("DELETE /api/workspaces/{workspaceId}/files/{fileId}", a.delete)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/files/{fileId}/publish", a.publish)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/files/{fileId}/rescan", a.rescan)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/files/{fileId}/download", a.download)
	mux.HandleFunc("GET /f/{slug}", a.publicPageGet)
	mux.HandleFunc("POST /f/{slug}", a.publicPagePost)
	mux.HandleFunc("GET /api/public/files/{slug}", a.publicDownload)
	return fileSecurityHeaders(mux)
}

func fileSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (a *API) authenticate(w http.ResponseWriter, r *http.Request, workspaceID string, mutation bool) (actorContext, bool) {
	if !a.testAuthEnabled {
		writeFileAPIError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is not available in this implementation stage.")
		return actorContext{}, false
	}
	actorID := strings.TrimSpace(r.Header.Get("X-GoJet-Test-Actor"))
	role := strings.ToLower(strings.TrimSpace(r.Header.Get("X-GoJet-Test-Workspace-Role")))
	headerWorkspace := strings.TrimSpace(r.Header.Get("X-GoJet-Test-Workspace"))
	if actorID == "" || role == "" || headerWorkspace == "" || headerWorkspace != workspaceID {
		writeFileAPIError(w, http.StatusForbidden, "forbidden", "Workspace access denied.")
		return actorContext{}, false
	}
	if role != "owner" && role != "admin" && role != "member" && role != "viewer" {
		writeFileAPIError(w, http.StatusForbidden, "forbidden", "Workspace access denied.")
		return actorContext{}, false
	}
	if mutation && role == "viewer" {
		writeFileAPIError(w, http.StatusForbidden, "read_only", "This Workspace role is read-only.")
		return actorContext{}, false
	}
	return actorContext{ActorID: actorID, Role: role}, true
}

func fileCorrelationID(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if value != "" && len(value) <= 128 {
		return value
	}
	return fmt.Sprintf("p09-%d", time.Now().UTC().UnixNano())
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	if _, ok := a.authenticate(w, r, workspaceID, false); !ok {
		return
	}
	limit, err := parseOptionalNonNegativeInt(r.URL.Query().Get("limit"), 50)
	if err != nil {
		writeFileAPIError(w, http.StatusBadRequest, "invalid_limit", "Invalid list limit.")
		return
	}
	offset, err := parseOptionalNonNegativeInt(r.URL.Query().Get("offset"), 0)
	if err != nil {
		writeFileAPIError(w, http.StatusBadRequest, "invalid_offset", "Invalid list offset.")
		return
	}
	items, total, err := a.store.List(r.Context(), workspaceID, limit, offset)
	if err != nil {
		writeFileStoreError(w, err)
		return
	}
	writeFileJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}
