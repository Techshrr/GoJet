package billing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeDowngradeStore struct {
	fakeAPIStore
	calls    int
	input    ScheduleDowngradeInput
	schedule DowngradeSchedule
	created  bool
	err      error
}

func (f *fakeDowngradeStore) ScheduleDowngrade(_ context.Context, input ScheduleDowngradeInput) (DowngradeSchedule, bool, error) {
	f.calls++
	f.input = input
	return f.schedule, f.created, f.err
}

func TestScheduleDowngradeOwnerOnlyAndNoStore(t *testing.T) {
	now := time.Date(2026, 8, 24, 5, 30, 0, 0, time.UTC)
	store := &fakeDowngradeStore{
		created: true,
		schedule: DowngradeSchedule{
			Current:       Subscription{ID: "sub_current", WorkspaceID: "ws", PlanID: 2, Status: SubscriptionGrace, Version: 8},
			Target:        Subscription{ID: "sub_target", WorkspaceID: "ws", PlanID: 1, Status: SubscriptionPending, Version: 1},
			GraceStartsAt: now,
			EffectiveAt:   now.Add(7 * 24 * time.Hour),
		},
	}
	api := NewAPI(store, fakePrincipal{}, fakeMembership{role: "owner"}, nil)
	api.now = func() time.Time { return now }
	r := httptest.NewRequest(http.MethodPost, "/api/workspaces/ws/billing/downgrade", strings.NewReader(`{"target_plan_id":1,"expected_version":7}`))
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, r)

	if w.Code != http.StatusCreated || store.calls != 1 {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, store.calls, w.Body.String())
	}
	if store.input.WorkspaceID != "ws" || store.input.TargetPlanID != 1 || store.input.ExpectedVersion != 7 || store.input.ActorID != "actor" || !store.input.Now.Equal(now) {
		t.Fatalf("unexpected downgrade input: %+v", store.input)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Robots-Tag") != "noindex, nofollow" {
		t.Fatalf("workspace billing response missing private cache/robots policy: %#v", w.Header())
	}
	if body := w.Body.String(); !strings.Contains(body, `"created":true`) || !strings.Contains(body, `"effective_at"`) {
		t.Fatalf("unexpected downgrade response: %s", body)
	}
}

func TestScheduleDowngradeRejectsNonOwnerBeforeStoreMutation(t *testing.T) {
	for _, role := range []string{"admin", "member", "viewer"} {
		store := &fakeDowngradeStore{created: true}
		api := NewAPI(store, fakePrincipal{}, fakeMembership{role: role}, nil)
		r := httptest.NewRequest(http.MethodPost, "/api/workspaces/ws/billing/downgrade", strings.NewReader(`{"target_plan_id":1,"expected_version":7}`))
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusForbidden || store.calls != 0 {
			t.Fatalf("role=%s status=%d calls=%d", role, w.Code, store.calls)
		}
	}
}

func TestScheduleDowngradeFailsClosedWhenStoreCapabilityUnavailable(t *testing.T) {
	api := NewAPI(&fakeAPIStore{}, fakePrincipal{}, fakeMembership{role: "owner"}, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/workspaces/ws/billing/downgrade", strings.NewReader(`{"target_plan_id":1,"expected_version":7}`))
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
