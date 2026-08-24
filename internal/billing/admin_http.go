package billing

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const BillingManagePermission = "billing.manage"

type AdminPermissionResolver interface {
	HasPermission(context.Context, RequestPrincipal, string) (bool, error)
}

type WorkspaceFinancialStore interface {
	ListWorkspaceInvoices(context.Context, string, int) ([]Invoice, error)
	ListWorkspacePayments(context.Context, string, int) ([]PaymentRecord, error)
}

type AdminPlanStore interface {
	ListAdminPlans(context.Context) ([]Plan, error)
	GetAdminPlan(context.Context, uint64) (Plan, error)
	CreateAdminPlan(context.Context, CreatePlanInput) (Plan, error)
	UpdateAdminPlan(context.Context, UpdatePlanInput) (Plan, error)
}

type AdminFinancialStore interface {
	ListAdminPayments(context.Context, int) ([]PaymentRecord, error)
	GetAdminPayment(context.Context, uint64) (PaymentRecord, error)
	ListAdminInvoices(context.Context, int) ([]Invoice, error)
}

type AdminFXStore interface {
	ListFXRates(context.Context) ([]FXRate, error)
	UpsertFXRate(context.Context, UpsertFXRateInput) (FXRate, error)
	MarkFXProviderError(context.Context, MarkFXProviderErrorInput) (FXRate, error)
}

type planEntitlementPayload struct {
	Capability string `json:"capability"`
	LimitValue uint64 `json:"limit_value"`
	Unit       string `json:"unit"`
}

var correlationPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func (a *API) registerCommerceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/invoices", a.listWorkspaceInvoices)
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/payments", a.listWorkspacePayments)
	mux.HandleFunc("GET /api/admin/plans", a.listAdminPlans)
	mux.HandleFunc("POST /api/admin/plans", a.createAdminPlan)
	mux.HandleFunc("GET /api/admin/plans/{planId}", a.getAdminPlan)
	mux.HandleFunc("PUT /api/admin/plans/{planId}", a.updateAdminPlan)
	mux.HandleFunc("GET /api/admin/payments", a.listAdminPayments)
	mux.HandleFunc("GET /api/admin/payments/{paymentId}", a.getAdminPayment)
	mux.HandleFunc("GET /api/admin/invoices", a.listAdminInvoices)
	mux.HandleFunc("GET /api/admin/fx", a.listAdminFX)
	mux.HandleFunc("PUT /api/admin/fx/{base}/{quote}", a.upsertAdminFX)
	mux.HandleFunc("POST /api/admin/fx/{base}/{quote}/provider-error", a.markAdminFXProviderError)
}

func (a *API) listWorkspaceInvoices(w http.ResponseWriter, r *http.Request) {
	_, role, ok := a.workspaceActor(w, r)
	if !ok {
		return
	}
	if role != "owner" {
		writeBillingError(w, http.StatusForbidden, "forbidden", "Financial ledger access requires a Workspace owner.")
		return
	}
	store, ok := a.store.(WorkspaceFinancialStore)
	if !ok {
		writeBillingError(w, http.StatusServiceUnavailable, "billing_unavailable", "Financial ledger is unavailable.")
		return
	}
	limit, ok := parseListLimit(w, r)
	if !ok {
		return
	}
	items, err := store.ListWorkspaceInvoices(r.Context(), r.PathValue("workspaceId"), limit)
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	writeBillingJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) listWorkspacePayments(w http.ResponseWriter, r *http.Request) {
	_, role, ok := a.workspaceActor(w, r)
	if !ok {
		return
	}
	if role != "owner" {
		writeBillingError(w, http.StatusForbidden, "forbidden", "Financial ledger access requires a Workspace owner.")
		return
	}
	store, ok := a.store.(WorkspaceFinancialStore)
	if !ok {
		writeBillingError(w, http.StatusServiceUnavailable, "billing_unavailable", "Financial ledger is unavailable.")
		return
	}
	limit, ok := parseListLimit(w, r)
	if !ok {
		return
	}
	items, err := store.ListWorkspacePayments(r.Context(), r.PathValue("workspaceId"), limit)
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	writeBillingJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) listAdminPlans(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminActor(w, r); !ok {
		return
	}
	store, ok := a.store.(AdminPlanStore)
	if !ok {
		writeBillingError(w, http.StatusServiceUnavailable, "billing_unavailable", "Admin plans are unavailable.")
		return
	}
	items, err := store.ListAdminPlans(r.Context())
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	writeBillingJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) getAdminPlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminActor(w, r); !ok {
		return
	}
	store, ok := a.store.(AdminPlanStore)
	if !ok {
		writeBillingError(w, http.StatusServiceUnavailable, "billing_unavailable", "Admin plans are unavailable.")
		return
	}
	planID, ok := parseUintPath(w, r.PathValue("planId"), "invalid_plan")
	if !ok {
		return
	}
	plan, err := store.GetAdminPlan(r.Context(), planID)
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	writeBillingJSON(w, http.StatusOK, map[string]any{"plan": plan})
}

func (a *API) createAdminPlan(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.adminActor(w, r)
	if !ok {
		return
	}
	store, ok := a.store.(AdminPlanStore)
	if !ok {
		writeBillingError(w, http.StatusServiceUnavailable, "billing_unavailable", "Admin plans are unavailable.")
		return
	}
	var body struct {
		Code          string                   `json:"code"`
		Name          string                   `json:"name"`
		Status        PlanStatus               `json:"status"`
		Currency      string                   `json:"currency"`
		AmountMinor   int64                    `json:"amount_minor"`
		BillingPeriod BillingPeriod            `json:"billing_period"`
		Entitlements  []planEntitlementPayload `json:"entitlements"`
	}
	if !decodeBillingJSON(w, r, &body) {
		return
	}
	correlationID, ok := requestCorrelationID(w, r)
	if !ok {
		return
	}
	plan, err := store.CreateAdminPlan(r.Context(), CreatePlanInput{
		Code: body.Code, Name: body.Name, Status: body.Status,
		Money: Money{Currency: strings.TrimSpace(body.Currency), AmountMinor: body.AmountMinor}, BillingPeriod: body.BillingPeriod,
		Entitlements: payloadEntitlements(body.Entitlements), ActorID: principal.UserID, CorrelationID: correlationID,
	})
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	writeBillingJSON(w, http.StatusCreated, map[string]any{"plan": plan})
}

func (a *API) updateAdminPlan(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.adminActor(w, r)
	if !ok {
		return
	}
	store, ok := a.store.(AdminPlanStore)
	if !ok {
		writeBillingError(w, http.StatusServiceUnavailable, "billing_unavailable", "Admin plans are unavailable.")
		return
	}
	planID, ok := parseUintPath(w, r.PathValue("planId"), "invalid_plan")
	if !ok {
		return
	}
	var body struct {
		Name            string                   `json:"name"`
		Status          PlanStatus               `json:"status"`
		Currency        string                   `json:"currency"`
		AmountMinor     int64                    `json:"amount_minor"`
		BillingPeriod   BillingPeriod            `json:"billing_period"`
		Entitlements    []planEntitlementPayload `json:"entitlements"`
		ExpectedVersion uint64                   `json:"expected_version"`
	}
	if !decodeBillingJSON(w, r, &body) {
		return
	}
	correlationID, ok := requestCorrelationID(w, r)
	if !ok {
		return
	}
	plan, err := store.UpdateAdminPlan(r.Context(), UpdatePlanInput{
		PlanID: planID, Name: body.Name, Status: body.Status,
		Money: Money{Currency: strings.TrimSpace(body.Currency), AmountMinor: body.AmountMinor}, BillingPeriod: body.BillingPeriod,
		Entitlements: payloadEntitlements(body.Entitlements), ExpectedVersion: body.ExpectedVersion,
		ActorID: principal.UserID, CorrelationID: correlationID,
	})
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	writeBillingJSON(w, http.StatusOK, map[string]any{"plan": plan})
}

func (a *API) listAdminPayments(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminActor(w, r); !ok {
		return
	}
	store, ok := a.store.(AdminFinancialStore)
	if !ok {
		writeBillingError(w, http.StatusServiceUnavailable, "billing_unavailable", "Admin payments are unavailable.")
		return
	}
	limit, ok := parseListLimit(w, r)
	if !ok {
		return
	}
	items, err := store.ListAdminPayments(r.Context(), limit)
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	writeBillingJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) getAdminPayment(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminActor(w, r); !ok {
		return
	}
	store, ok := a.store.(AdminFinancialStore)
	if !ok {
		writeBillingError(w, http.StatusServiceUnavailable, "billing_unavailable", "Admin payments are unavailable.")
		return
	}
	paymentID, ok := parseUintPath(w, r.PathValue("paymentId"), "invalid_payment")
	if !ok {
		return
	}
	item, err := store.GetAdminPayment(r.Context(), paymentID)
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	writeBillingJSON(w, http.StatusOK, map[string]any{"payment": item})
}

func (a *API) listAdminInvoices(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminActor(w, r); !ok {
		return
	}
	store, ok := a.store.(AdminFinancialStore)
	if !ok {
		writeBillingError(w, http.StatusServiceUnavailable, "billing_unavailable", "Admin invoices are unavailable.")
		return
	}
	limit, ok := parseListLimit(w, r)
	if !ok {
		return
	}
	items, err := store.ListAdminInvoices(r.Context(), limit)
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	writeBillingJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) listAdminFX(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminActor(w, r); !ok {
		return
	}
	store, ok := a.store.(AdminFXStore)
	if !ok {
		writeBillingError(w, http.StatusServiceUnavailable, "billing_unavailable", "FX authority is unavailable.")
		return
	}
	items, err := store.ListFXRates(r.Context())
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	writeBillingJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) upsertAdminFX(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.adminActor(w, r)
	if !ok {
		return
	}
	store, ok := a.store.(AdminFXStore)
	if !ok {
		writeBillingError(w, http.StatusServiceUnavailable, "billing_unavailable", "FX authority is unavailable.")
		return
	}
	var body struct {
		Rate           string   `json:"rate"`
		Source         string   `json:"source"`
		AsOf           string   `json:"as_of"`
		Status         FXStatus `json:"status"`
		OverrideReason string   `json:"override_reason"`
	}
	if !decodeBillingJSON(w, r, &body) {
		return
	}
	asOf, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(body.AsOf))
	if err != nil {
		writeBillingError(w, http.StatusBadRequest, "invalid_as_of", "Invalid FX as-of timestamp.")
		return
	}
	correlationID, ok := requestCorrelationID(w, r)
	if !ok {
		return
	}
	item, err := store.UpsertFXRate(r.Context(), UpsertFXRateInput{
		BaseCurrency: r.PathValue("base"), QuoteCurrency: r.PathValue("quote"), Rate: body.Rate,
		Source: body.Source, AsOf: asOf.UTC(), Status: body.Status, OverrideReason: body.OverrideReason,
		ActorID: principal.UserID, CorrelationID: correlationID,
	})
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	writeBillingJSON(w, http.StatusOK, map[string]any{"fx": item})
}

func (a *API) markAdminFXProviderError(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.adminActor(w, r)
	if !ok {
		return
	}
	store, ok := a.store.(AdminFXStore)
	if !ok {
		writeBillingError(w, http.StatusServiceUnavailable, "billing_unavailable", "FX authority is unavailable.")
		return
	}
	var body struct {
		Source string `json:"source"`
		AsOf   string `json:"as_of"`
	}
	if !decodeBillingJSON(w, r, &body) {
		return
	}
	asOf, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(body.AsOf))
	if err != nil {
		writeBillingError(w, http.StatusBadRequest, "invalid_as_of", "Invalid FX as-of timestamp.")
		return
	}
	correlationID, ok := requestCorrelationID(w, r)
	if !ok {
		return
	}
	item, err := store.MarkFXProviderError(r.Context(), MarkFXProviderErrorInput{
		BaseCurrency: r.PathValue("base"), QuoteCurrency: r.PathValue("quote"), Source: body.Source,
		AsOf: asOf.UTC(), ActorID: principal.UserID, CorrelationID: correlationID,
	})
	if err != nil {
		writeBillingStoreError(w, err)
		return
	}
	writeBillingJSON(w, http.StatusOK, map[string]any{"fx": item})
}

func (a *API) adminActor(w http.ResponseWriter, r *http.Request) (RequestPrincipal, bool) {
	if a.principals == nil || a.adminPermissions == nil {
		writeBillingError(w, http.StatusServiceUnavailable, "admin_permission_unavailable", "Admin permission authority is unavailable.")
		return RequestPrincipal{}, false
	}
	principal, err := a.principals.ResolvePrincipal(r)
	if err != nil || strings.TrimSpace(principal.UserID) == "" {
		if errors.Is(err, ErrAuthenticationUnavailable) {
			writeBillingError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is unavailable.")
		} else {
			writeBillingError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		}
		return RequestPrincipal{}, false
	}
	allowed, err := a.adminPermissions.HasPermission(r.Context(), principal, BillingManagePermission)
	if err != nil {
		writeBillingError(w, http.StatusServiceUnavailable, "admin_permission_unavailable", "Admin permission authority is unavailable.")
		return RequestPrincipal{}, false
	}
	if !allowed {
		writeBillingError(w, http.StatusForbidden, "forbidden", "Admin billing permission denied.")
		return RequestPrincipal{}, false
	}
	return principal, true
}

func requestCorrelationID(w http.ResponseWriter, r *http.Request) (string, bool) {
	value := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if value != "" {
		if !correlationPattern.MatchString(value) {
			writeBillingError(w, http.StatusBadRequest, "invalid_request_id", "Invalid request correlation ID.")
			return "", false
		}
		return value, true
	}
	generated, err := newOpaqueID("req_")
	if err != nil {
		writeBillingError(w, http.StatusInternalServerError, "server_error", "Request could not be completed.")
		return "", false
	}
	return generated, true
}

func parseListLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 50, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > 100 {
		writeBillingError(w, http.StatusBadRequest, "invalid_limit", "Invalid list limit.")
		return 0, false
	}
	return value, true
}

func parseUintPath(w http.ResponseWriter, raw, code string) (uint64, bool) {
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil || value == 0 {
		writeBillingError(w, http.StatusBadRequest, code, "Invalid resource identifier.")
		return 0, false
	}
	return value, true
}

func payloadEntitlements(items []planEntitlementPayload) []PlanEntitlement {
	out := make([]PlanEntitlement, 0, len(items))
	for _, item := range items {
		out = append(out, PlanEntitlement{Capability: item.Capability, LimitValue: item.LimitValue, Unit: item.Unit})
	}
	return out
}
