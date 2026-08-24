package billing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeAPIStore struct {
	callbackCalls int
	callback      CallbackResult
	callbackErr   error
	order         Order
}

func (f *fakeAPIStore) ListPublicPlans(context.Context) ([]Plan, error) { return []Plan{}, nil }
func (f *fakeAPIStore) CreateOrder(context.Context, CreateOrderInput) (Order, bool, error) {
	return f.order, true, nil
}
func (f *fakeAPIStore) GetOrder(context.Context, string, string) (Order, error) { return f.order, nil }
func (f *fakeAPIStore) ResolveWorkspaceEntitlement(context.Context, string, string, time.Time) (ResolvedEntitlement, error) {
	return ResolvedEntitlement{Capability: "links", Allowed: true, LimitValue: 10, SourceType: SourceBilling, SourceID: "private-source", ActiveGrants: []EntitlementGrant{{SourceID: "private-source"}}}, nil
}
func (f *fakeAPIStore) ApplyAuthenticatedCallback(context.Context, CallbackCommand) (CallbackResult, error) {
	f.callbackCalls++
	return f.callback, f.callbackErr
}

type fakePrincipal struct{ err error }

func (f fakePrincipal) ResolvePrincipal(*http.Request) (RequestPrincipal, error) {
	if f.err != nil {
		return RequestPrincipal{}, f.err
	}
	return RequestPrincipal{UserID: "actor"}, nil
}

type fakeMembership struct {
	role string
	err  error
}

func (f fakeMembership) ResolveWorkspaceRole(context.Context, string, string) (string, error) {
	return f.role, f.err
}

type fakeCallbackVerifier struct {
	cmd   CallbackCommand
	err   error
	calls int
}

func (f *fakeCallbackVerifier) VerifyAndNormalize(*http.Request, Provider) (CallbackCommand, error) {
	f.calls++
	return f.cmd, f.err
}

func TestCallbackVerificationPrecedesMutation(t *testing.T) {
	store := &fakeAPIStore{}
	verifier := &fakeCallbackVerifier{err: ErrCallbackUnauthorized}
	api := NewAPI(store, fakePrincipal{}, fakeMembership{role: "owner"}, verifier)
	r := httptest.NewRequest(http.MethodPost, "/api/payments/callbacks/stripe", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || verifier.calls != 1 || store.callbackCalls != 0 {
		t.Fatalf("status=%d verifier=%d store=%d", w.Code, verifier.calls, store.callbackCalls)
	}
}

func TestCallbackSafeAck(t *testing.T) {
	store := &fakeAPIStore{callback: CallbackResult{Duplicate: true, Order: Order{ID: "secret-order"}, Transaction: Transaction{ProviderTransactionID: "provider-secret"}}}
	verifier := &fakeCallbackVerifier{cmd: CallbackCommand{Provider: ProviderStripe}}
	api := NewAPI(store, fakePrincipal{}, fakeMembership{role: "owner"}, verifier)
	r := httptest.NewRequest(http.MethodPost, "/api/payments/callbacks/stripe", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, r)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `"duplicate":true`) || strings.Contains(body, "secret-order") || strings.Contains(body, "provider-secret") {
		t.Fatalf("status=%d body=%s", w.Code, body)
	}
}

func TestOwnerOnlyOrderMutation(t *testing.T) {
	for _, role := range []string{"admin", "member", "viewer"} {
		store := &fakeAPIStore{}
		api := NewAPI(store, fakePrincipal{}, fakeMembership{role: role}, nil)
		r := httptest.NewRequest(http.MethodPost, "/api/workspaces/ws/orders", strings.NewReader(`{"plan_id":1,"kind":"new"}`))
		r.Header.Set("Idempotency-Key", "0123456789abcdef")
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("role=%s status=%d", role, w.Code)
		}
	}
}

func TestEntitlementResponseDoesNotExposeProvenance(t *testing.T) {
	store := &fakeAPIStore{}
	api := NewAPI(store, fakePrincipal{}, fakeMembership{role: "viewer"}, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/workspaces/ws/billing/entitlements/links", nil)
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "private-source") || strings.Contains(w.Body.String(), "active_grants") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAuthenticationUnavailableFailsClosed(t *testing.T) {
	api := NewAPI(&fakeAPIStore{}, fakePrincipal{err: ErrAuthenticationUnavailable}, fakeMembership{role: "owner"}, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/workspaces/ws/orders/ord", nil)
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestWorkspaceMembershipFailureIsForbidden(t *testing.T) {
	api := NewAPI(&fakeAPIStore{}, fakePrincipal{}, fakeMembership{err: errors.New("missing")}, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/workspaces/foreign/orders/ord", nil)
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d", w.Code)
	}
}
