package billing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeAdminPermission struct {
	allowed bool
	err     error
	calls   int
}

func (f *fakeAdminPermission) HasPermission(_ context.Context, principal RequestPrincipal, permission string) (bool, error) {
	f.calls++
	if principal.UserID == "" || permission != BillingManagePermission {
		return false, errors.New("unexpected permission input")
	}
	return f.allowed, f.err
}

type fakeAdminCommerceStore struct {
	fakeAPIStore
	planCalls int
	plans     []Plan
}

func (f *fakeAdminCommerceStore) ListAdminPlans(context.Context) ([]Plan, error) {
	f.planCalls++
	return f.plans, nil
}
func (f *fakeAdminCommerceStore) GetAdminPlan(context.Context, uint64) (Plan, error) {
	return Plan{}, nil
}
func (f *fakeAdminCommerceStore) CreateAdminPlan(context.Context, CreatePlanInput) (Plan, error) {
	return Plan{}, nil
}
func (f *fakeAdminCommerceStore) UpdateAdminPlan(context.Context, UpdatePlanInput) (Plan, error) {
	return Plan{}, nil
}

type fakeFXCommerceStore struct {
	fakeAdminCommerceStore
	fxMutations int
}

func (f *fakeFXCommerceStore) ListFXRates(context.Context) ([]FXRate, error) { return []FXRate{}, nil }
func (f *fakeFXCommerceStore) UpsertFXRate(context.Context, UpsertFXRateInput) (FXRate, error) {
	f.fxMutations++
	return FXRate{}, nil
}
func (f *fakeFXCommerceStore) MarkFXProviderError(context.Context, MarkFXProviderErrorInput) (FXRate, error) {
	f.fxMutations++
	return FXRate{}, nil
}

type fakeWorkspaceFinancialStore struct {
	fakeAPIStore
	calls int
}

func (f *fakeWorkspaceFinancialStore) ListWorkspaceInvoices(context.Context, string, int) ([]Invoice, error) {
	f.calls++
	return []Invoice{}, nil
}
func (f *fakeWorkspaceFinancialStore) ListWorkspacePayments(context.Context, string, int) ([]PaymentRecord, error) {
	f.calls++
	return []PaymentRecord{}, nil
}

func TestAdminCommerceFailsClosedWithoutPermissionResolver(t *testing.T) {
	store := &fakeAdminCommerceStore{}
	api := NewAPI(store, fakePrincipal{}, fakeMembership{role: "owner"}, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/admin/plans", nil)
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || store.planCalls != 0 {
		t.Fatalf("status=%d plan_calls=%d body=%s", w.Code, store.planCalls, w.Body.String())
	}
}

func TestAdminCommerceRequiresBillingManage(t *testing.T) {
	store := &fakeAdminCommerceStore{}
	permissions := &fakeAdminPermission{allowed: false}
	api := NewAPI(store, fakePrincipal{}, fakeMembership{role: "owner"}, nil).SetAdminPermissionResolver(permissions)
	r := httptest.NewRequest(http.MethodGet, "/api/admin/plans", nil)
	r.Header.Set("X-GoJet-Test-Role", "super-admin")
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || permissions.calls != 1 || store.planCalls != 0 {
		t.Fatalf("status=%d permission_calls=%d plan_calls=%d", w.Code, permissions.calls, store.planCalls)
	}
}

func TestAdminCommerceAuthorizedResponsesArePrivate(t *testing.T) {
	store := &fakeAdminCommerceStore{plans: []Plan{{ID: 1, Code: "pro", Status: PlanActive}}}
	permissions := &fakeAdminPermission{allowed: true}
	api := NewAPI(store, fakePrincipal{}, fakeMembership{role: "owner"}, nil).SetAdminPermissionResolver(permissions)
	r := httptest.NewRequest(http.MethodGet, "/api/admin/plans", nil)
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK || store.planCalls != 1 {
		t.Fatalf("status=%d plan_calls=%d body=%s", w.Code, store.planCalls, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Robots-Tag") != "noindex, nofollow" {
		t.Fatalf("admin response missing no-store/noindex policy: %#v", w.Header())
	}
	if !strings.Contains(w.Body.String(), `"code":"pro"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestWorkspaceFinancialReadRequiresOwner(t *testing.T) {
	for _, role := range []string{"admin", "member", "viewer"} {
		store := &fakeWorkspaceFinancialStore{}
		api := NewAPI(store, fakePrincipal{}, fakeMembership{role: role}, nil)
		r := httptest.NewRequest(http.MethodGet, "/api/workspaces/ws/payments", nil)
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusForbidden || store.calls != 0 {
			t.Fatalf("role=%s status=%d calls=%d", role, w.Code, store.calls)
		}
	}
}

func TestPlanValidationAndTransitions(t *testing.T) {
	items, err := normalizePlanEntitlements([]PlanEntitlement{
		{Capability: "custom_domains", LimitValue: 5},
		{Capability: "links", LimitValue: 100, Unit: "count"},
	})
	if err != nil || len(items) != 2 || items[0].Capability != "custom_domains" || items[0].Unit != "count" {
		t.Fatalf("normalized entitlements=%#v err=%v", items, err)
	}
	if _, err := normalizePlanEntitlements([]PlanEntitlement{{Capability: "links", LimitValue: 1}, {Capability: "links", LimitValue: 2}}); err == nil {
		t.Fatal("duplicate capability must fail")
	}
	if !planStatusTransitionAllowed(PlanDraft, PlanActive) || !planStatusTransitionAllowed(PlanActive, PlanArchived) {
		t.Fatal("expected forward plan lifecycle transitions")
	}
	if planStatusTransitionAllowed(PlanArchived, PlanActive) || planStatusTransitionAllowed(PlanActive, PlanDraft) {
		t.Fatal("archived/reactivation or active-to-draft must fail")
	}
}

func TestFXValidationPreservesDecimalAuthority(t *testing.T) {
	valid := []string{"1", "0.000000000001", "1234567890123456.123456789012"}
	for _, value := range valid {
		if !validPositiveFXRate(value) {
			t.Fatalf("valid FX rate rejected: %s", value)
		}
	}
	invalid := []string{"0", "-1", "1.0000000000001", "12345678901234567", "NaN", "1e3"}
	for _, value := range invalid {
		if validPositiveFXRate(value) {
			t.Fatalf("invalid FX rate accepted: %s", value)
		}
	}
	if !validFXPair("USD", "SGD") || validFXPair("USD", "USD") || validFXPair("usd", "SGD") {
		t.Fatal("FX pair validation mismatch")
	}
}

func TestAdminFXRejectsInvalidAsOfBeforeStoreMutation(t *testing.T) {
	store := &fakeFXCommerceStore{}
	permissions := &fakeAdminPermission{allowed: true}
	api := NewAPI(store, fakePrincipal{}, fakeMembership{role: "owner"}, nil).SetAdminPermissionResolver(permissions)
	r := httptest.NewRequest(http.MethodPut, "/api/admin/fx/USD/SGD", strings.NewReader(`{"rate":"1.3","source":"fixture","as_of":"not-time","status":"current","override_reason":""}`))
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || store.fxMutations != 0 {
		t.Fatalf("status=%d fx_mutations=%d body=%s", w.Code, store.fxMutations, w.Body.String())
	}
}
