package support

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

const MailManagePermission = "mail.manage"

type AdminMailStore interface {
	ListAdminMailQueue(ctx context.Context, limit int) ([]AdminMailQueueItem, error)
	ListAdminMailTemplates(ctx context.Context) ([]AdminMailTemplateView, error)
	GetAdminMailSettings(ctx context.Context) (AdminMailSettings, error)
	UpdateAdminMailSettings(ctx context.Context, expectedVersion uint64, enabled bool) (AdminMailSettings, error)
	EnqueueAdminTestMail(ctx context.Context, input AdminMailTestInput) (AdminMailQueueItem, bool, error)
}

type AdminMailAPI struct {
	store       AdminMailStore
	principals  PrincipalResolver
	permissions AdminPermissionResolver
}

func NewAdminMailAPI(store AdminMailStore, principals PrincipalResolver, permissions AdminPermissionResolver) (*AdminMailAPI, error) {
	if store == nil || principals == nil {
		return nil, ErrInvalidInput
	}
	return &AdminMailAPI{store: store, principals: principals, permissions: permissions}, nil
}

func (a *AdminMailAPI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/mail/queue", a.listQueue)
	mux.HandleFunc("GET /api/admin/mail/templates", a.listTemplates)
	mux.HandleFunc("GET /api/admin/mail/settings", a.getSettings)
	mux.HandleFunc("PATCH /api/admin/mail/settings", a.patchSettings)
	mux.HandleFunc("POST /api/admin/mail/test", a.testSend)
	return supportSecurityHeaders(mux)
}

func (a *AdminMailAPI) listQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.mailAdminActor(w, r); !ok {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 100 {
			writeSupportError(w, r, http.StatusBadRequest, "invalid_limit", "Invalid list limit.")
			return
		}
		limit = parsed
	}
	items, err := a.store.ListAdminMailQueue(r.Context(), limit)
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	writeSupportJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *AdminMailAPI) listTemplates(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.mailAdminActor(w, r); !ok {
		return
	}
	items, err := a.store.ListAdminMailTemplates(r.Context())
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	writeSupportJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *AdminMailAPI) getSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.mailAdminActor(w, r); !ok {
		return
	}
	settings, err := a.store.GetAdminMailSettings(r.Context())
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	writeSupportJSON(w, http.StatusOK, map[string]any{
		"settings":           settings,
		"credentials_masked": true,
		"credential_source":  "runtime",
	})
}

type adminMailSettingsPatch struct {
	Enabled         *bool  `json:"enabled"`
	ExpectedVersion uint64 `json:"expected_version"`
}

func (a *AdminMailAPI) patchSettings(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.mailAdminActor(w, r)
	if !ok {
		return
	}
	var body adminMailSettingsPatch
	if !decodeSupportJSON(w, r, &body) {
		return
	}
	if body.Enabled == nil || body.ExpectedVersion == 0 {
		writeSupportError(w, r, http.StatusBadRequest, "invalid_request", "Invalid mail settings mutation.")
		return
	}
	storeCtx := WithSupportAuditActor(r.Context(), principal.UserID)
	settings, err := a.store.UpdateAdminMailSettings(storeCtx, body.ExpectedVersion, *body.Enabled)
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	writeSupportJSON(w, http.StatusOK, map[string]any{
		"settings":           settings,
		"credentials_masked": true,
		"credential_source":  "runtime",
	})
}

type adminMailTestRequest struct {
	Recipient string `json:"recipient"`
}

func (a *AdminMailAPI) testSend(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.mailAdminActor(w, r)
	if !ok {
		return
	}
	var body adminMailTestRequest
	if !decodeSupportJSON(w, r, &body) {
		return
	}
	body.Recipient = strings.TrimSpace(body.Recipient)
	if body.Recipient == "" {
		writeSupportError(w, r, http.StatusBadRequest, "invalid_request", "A test recipient is required.")
		return
	}
	rawKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if rawKey == "" {
		writeSupportError(w, r, http.StatusBadRequest, "idempotency_required", "Idempotency-Key is required for test delivery.")
		return
	}
	hash, err := adminOperationIdempotencyHash("mail-test", "primary", principal.UserID, rawKey)
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	storeCtx := WithSupportAuditActor(r.Context(), principal.UserID)
	job, created, err := a.store.EnqueueAdminTestMail(storeCtx, AdminMailTestInput{
		ActorID: principal.UserID, Recipient: body.Recipient,
		CorrelationID: supportRequestCorrelationID(r), IdempotencyKeyHash: hash,
	})
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	// AdminMailQueueItem deliberately has no recipient_value or credential fields.
	writeSupportJSON(w, status, map[string]any{"job": job, "created": created})
}

func (a *AdminMailAPI) mailAdminActor(w http.ResponseWriter, r *http.Request) (RequestPrincipal, bool) {
	if a == nil || a.principals == nil || a.permissions == nil {
		writeSupportError(w, r, http.StatusServiceUnavailable, "admin_permission_unavailable", "Admin permission authority is unavailable.")
		return RequestPrincipal{}, false
	}
	principal, err := a.principals.ResolvePrincipal(r)
	if err != nil || strings.TrimSpace(principal.UserID) == "" {
		if errors.Is(err, ErrAuthenticationUnavailable) {
			writeSupportError(w, r, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is unavailable.")
		} else {
			writeSupportError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		}
		return RequestPrincipal{}, false
	}
	allowed, err := a.permissions.HasPermission(r.Context(), principal, MailManagePermission)
	if err != nil {
		writeSupportError(w, r, http.StatusServiceUnavailable, "admin_permission_unavailable", "Admin permission authority is unavailable.")
		return RequestPrincipal{}, false
	}
	if !allowed {
		writeSupportError(w, r, http.StatusForbidden, "forbidden", "Admin mail permission denied.")
		return RequestPrincipal{}, false
	}
	return principal, true
}
