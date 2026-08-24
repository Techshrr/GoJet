package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	ErrAuthenticationUnavailable = errors.New("authentication unavailable")
	ErrAuthenticationRequired    = errors.New("authentication required")
	ErrWorkspaceForbidden        = errors.New("workspace forbidden")
	ErrCallbackUnavailable       = errors.New("callback verifier unavailable")
	ErrCallbackUnauthorized      = errors.New("callback unauthorized")
)

type RequestPrincipal struct {
	UserID      string
	Email       string
	DisplayName string
}

type PrincipalResolver interface {
	ResolvePrincipal(*http.Request) (RequestPrincipal, error)
}

type WorkspaceRoleResolver interface {
	ResolveWorkspaceRole(context.Context, string, string) (string, error)
}

type CallbackRequestVerifier interface {
	VerifyAndNormalize(*http.Request, Provider) (CallbackCommand, error)
}

type APIStore interface {
	ListPublicPlans(context.Context) ([]Plan, error)
	CreateOrder(context.Context, CreateOrderInput) (Order, bool, error)
	GetOrder(context.Context, string, string) (Order, error)
	ResolveWorkspaceEntitlement(context.Context, string, string, time.Time) (ResolvedEntitlement, error)
	ApplyAuthenticatedCallback(context.Context, CallbackCommand) (CallbackResult, error)
}

type DowngradeStore interface {
	ScheduleDowngrade(context.Context, ScheduleDowngradeInput) (DowngradeSchedule, bool, error)
}

type API struct {
	store            APIStore
	principals       PrincipalResolver
	memberships      WorkspaceRoleResolver
	callbacks        CallbackRequestVerifier
	adminPermissions AdminPermissionResolver
	now              func() time.Time
}

func NewAPI(store APIStore, principals PrincipalResolver, memberships WorkspaceRoleResolver, callbacks CallbackRequestVerifier) *API {
	return &API{store: store, principals: principals, memberships: memberships, callbacks: callbacks, now: time.Now}
}

func (a *API) SetAdminPermissionResolver(resolver AdminPermissionResolver) *API {
	a.adminPermissions = resolver
	return a
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/public/plans", a.listPublicPlans)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/orders", a.createOrder)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/orders/{orderId}", a.getOrder)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/billing/entitlements/{capability}", a.getEntitlement)
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/billing/downgrade", a.scheduleDowngrade)
	mux.HandleFunc("POST /api/payments/callbacks/{provider}", a.paymentCallback)
	a.registerCommerceRoutes(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/workspaces/") || strings.HasPrefix(r.URL.Path, "/api/admin/") {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		mux.ServeHTTP(w, r)
	})
}

func (a *API) listPublicPlans(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeBillingError(w, http.StatusServiceUnavailable, "billing_unavailable", "Billing is unavailable.")
		return
	}
	plans, err := a.store.ListPublicPlans(r.Context())
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	writeBillingJSON(w, http.StatusOK, map[string]any{"items": plans})
}

func (a *API) createOrder(w http.ResponseWriter, r *http.Request) {
	principal, role, ok := a.workspaceActor(w, r)
	if !ok {
		return
	}
	if role != "owner" {
		writeBillingError(w, http.StatusForbidden, "forbidden", "Billing mutation requires a Workspace owner.")
		return
	}
	var input struct {
		PlanID uint64    `json:"plan_id"`
		Kind   OrderKind `json:"kind"`
	}
	if !decodeBillingJSON(w, r, &input) {
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	order, created, err := a.store.CreateOrder(r.Context(), CreateOrderInput{
		WorkspaceID: r.PathValue("workspaceId"), PlanID: input.PlanID, Kind: input.Kind,
		IdempotencyKey: key, Now: a.now().UTC(),
	})
	_ = principal
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeBillingJSON(w, status, map[string]any{"order": order, "created": created})
}

func (a *API) getOrder(w http.ResponseWriter, r *http.Request) {
	_, role, ok := a.workspaceActor(w, r)
	if !ok {
		return
	}
	if role != "owner" {
		writeBillingError(w, http.StatusForbidden, "forbidden", "Financial ledger access requires a Workspace owner.")
		return
	}
	order, err := a.store.GetOrder(r.Context(), r.PathValue("workspaceId"), strings.TrimSpace(r.PathValue("orderId")))
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	writeBillingJSON(w, http.StatusOK, map[string]any{"order": order})
}

func (a *API) getEntitlement(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.workspaceActor(w, r)
	if !ok {
		return
	}
	capability := strings.TrimSpace(r.PathValue("capability"))
	if capability == "" || len(capability) > 96 {
		writeBillingError(w, http.StatusBadRequest, "invalid_capability", "Invalid capability.")
		return
	}
	resolved, err := a.store.ResolveWorkspaceEntitlement(r.Context(), r.PathValue("workspaceId"), capability, a.now().UTC())
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	// Deliberately expose the effective decision only. Provenance internals are
	// server authority and are not required by member/viewer clients.
	writeBillingJSON(w, http.StatusOK, map[string]any{
		"capability":  resolved.Capability,
		"allowed":     resolved.Allowed,
		"limit_value": resolved.LimitValue,
		"reason":      resolved.Reason,
	})
}

func (a *API) scheduleDowngrade(w http.ResponseWriter, r *http.Request) {
	principal, role, ok := a.workspaceActor(w, r)
	if !ok {
		return
	}
	if role != "owner" {
		writeBillingError(w, http.StatusForbidden, "forbidden", "Plan downgrade requires a Workspace owner.")
		return
	}
	store, ok := a.store.(DowngradeStore)
	if !ok {
		writeBillingError(w, http.StatusServiceUnavailable, "billing_unavailable", "Billing downgrade is unavailable.")
		return
	}
	var input struct {
		TargetPlanID    uint64 `json:"target_plan_id"`
		ExpectedVersion uint64 `json:"expected_version"`
	}
	if !decodeBillingJSON(w, r, &input) {
		return
	}
	schedule, created, err := store.ScheduleDowngrade(r.Context(), ScheduleDowngradeInput{
		WorkspaceID:     r.PathValue("workspaceId"),
		TargetPlanID:    input.TargetPlanID,
		ExpectedVersion: input.ExpectedVersion,
		ActorID:         principal.UserID,
		Now:             a.now().UTC(),
	})
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeBillingJSON(w, status, map[string]any{"schedule": schedule, "created": created})
}

func (a *API) paymentCallback(w http.ResponseWriter, r *http.Request) {
	provider := Provider(strings.TrimSpace(r.PathValue("provider")))
	if !IsFrozenProvider(provider) {
		writeBillingError(w, http.StatusNotFound, "provider_not_found", "Payment provider not found.")
		return
	}
	if a.callbacks == nil {
		writeBillingError(w, http.StatusServiceUnavailable, "callback_verifier_unavailable", "Payment callback verification is unavailable.")
		return
	}
	// No store call is permitted before this verification succeeds.
	cmd, err := a.callbacks.VerifyAndNormalize(r, provider)
	if err != nil {
		if errors.Is(err, ErrCallbackUnavailable) {
			writeBillingError(w, http.StatusServiceUnavailable, "callback_verifier_unavailable", "Payment callback verification is unavailable.")
			return
		}
		writeBillingError(w, http.StatusUnauthorized, "callback_unauthorized", "Payment callback authentication failed.")
		return
	}
	if cmd.Provider != provider {
		writeBillingError(w, http.StatusUnauthorized, "callback_unauthorized", "Payment callback authentication failed.")
		return
	}
	result, err := a.store.ApplyAuthenticatedCallback(r.Context(), cmd)
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	// Provider-safe ACK only: do not echo payment, entitlement or customer data.
	writeBillingJSON(w, http.StatusOK, map[string]any{"ok": true, "duplicate": result.Duplicate})
}

func (a *API) workspaceActor(w http.ResponseWriter, r *http.Request) (RequestPrincipal, string, bool) {
	if a.store == nil || a.principals == nil || a.memberships == nil {
		writeBillingError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is unavailable.")
		return RequestPrincipal{}, "", false
	}
	principal, err := a.principals.ResolvePrincipal(r)
	if err != nil {
		if errors.Is(err, ErrAuthenticationUnavailable) {
			writeBillingError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is unavailable.")
		} else {
			writeBillingError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		}
		return RequestPrincipal{}, "", false
	}
	if strings.TrimSpace(principal.UserID) == "" {
		writeBillingError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return RequestPrincipal{}, "", false
	}
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
	if workspaceID == "" {
		writeBillingError(w, http.StatusBadRequest, "invalid_workspace", "Invalid Workspace.")
		return RequestPrincipal{}, "", false
	}
	role, err := a.memberships.ResolveWorkspaceRole(r.Context(), workspaceID, principal.UserID)
	if err != nil {
		writeBillingError(w, http.StatusForbidden, "forbidden", "Workspace access denied.")
		return RequestPrincipal{}, "", false
	}
	switch role {
	case "owner", "admin", "member", "viewer":
	default:
		writeBillingError(w, http.StatusForbidden, "forbidden", "Workspace access denied.")
		return RequestPrincipal{}, "", false
	}
	return principal, role, true
}

func decodeBillingJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeBillingError(w, http.StatusBadRequest, "invalid_json", "Invalid request body.")
		return false
	}
	return true
}

func writeBillingStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidMoney), errors.Is(err, ErrInvalidEntitlement):
		writeBillingError(w, http.StatusBadRequest, "invalid_request", "Invalid request.")
	case errors.Is(err, ErrNotFound):
		writeBillingError(w, http.StatusNotFound, "not_found", "Resource not found.")
	case errors.Is(err, ErrConflict):
		writeBillingError(w, http.StatusConflict, "conflict", "Billing state conflict.")
	default:
		writeBillingError(w, http.StatusInternalServerError, "server_error", "Request could not be completed.")
	}
}

func writeBillingError(w http.ResponseWriter, status int, code, message string) {
	writeBillingJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeBillingJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func ParseAmountMinor(value string) (int64, error) {
	amount, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || amount <= 0 {
		return 0, ErrInvalidMoney
	}
	return amount, nil
}
