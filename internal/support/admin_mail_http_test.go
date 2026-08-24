package support

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeAdminMailStore struct {
	queueCalls     int
	templateCalls  int
	settingsCalls  int
	updateCalls    int
	testCalls      int
	settings       AdminMailSettings
	updateSettings AdminMailSettings
	testJob        AdminMailQueueItem
	testCreated    bool
	lastTestInput  AdminMailTestInput
}

func (s *fakeAdminMailStore) ListAdminMailQueue(_ context.Context, _ int) ([]AdminMailQueueItem, error) {
	s.queueCalls++
	return []AdminMailQueueItem{}, nil
}

func (s *fakeAdminMailStore) ListAdminMailTemplates(_ context.Context) ([]AdminMailTemplateView, error) {
	s.templateCalls++
	return []AdminMailTemplateView{}, nil
}

func (s *fakeAdminMailStore) GetAdminMailSettings(_ context.Context) (AdminMailSettings, error) {
	s.settingsCalls++
	return s.settings, nil
}

func (s *fakeAdminMailStore) UpdateAdminMailSettings(_ context.Context, _ uint64, _ bool) (AdminMailSettings, error) {
	s.updateCalls++
	return s.updateSettings, nil
}

func (s *fakeAdminMailStore) EnqueueAdminTestMail(_ context.Context, input AdminMailTestInput) (AdminMailQueueItem, bool, error) {
	s.testCalls++
	s.lastTestInput = input
	return s.testJob, s.testCreated, nil
}

func TestAdminMailAllSurfacesRequireMailManageBeforeStore(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	store := &fakeAdminMailStore{settings: AdminMailSettings{Enabled: true, Version: 1, UpdatedAt: now}}
	permissions := &recordingAdminPermissionResolver{allowed: false}
	api, err := NewAdminMailAPI(store, fixedAdminPrincipalResolver{principal: RequestPrincipal{UserID: "admin-1"}}, permissions)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/admin/mail/queue", ""},
		{http.MethodGet, "/api/admin/mail/templates", ""},
		{http.MethodGet, "/api/admin/mail/settings", ""},
		{http.MethodPatch, "/api/admin/mail/settings", `{"enabled":false,"expected_version":1}`},
		{http.MethodPost, "/api/admin/mail/test", `{"recipient":"admin@example.test"}`},
	}
	for _, tc := range tests {
		res := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		api.Handler().ServeHTTP(res, req)
		if res.Code != http.StatusForbidden {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, res.Code, res.Body.String())
		}
	}
	if store.queueCalls != 0 || store.templateCalls != 0 || store.settingsCalls != 0 || store.updateCalls != 0 || store.testCalls != 0 {
		t.Fatalf("store called before permission: %+v", store)
	}
	if len(permissions.seen) != len(tests) {
		t.Fatalf("permission calls=%v", permissions.seen)
	}
	for _, permission := range permissions.seen {
		if permission != MailManagePermission {
			t.Fatalf("permission=%q", permission)
		}
	}
}

func TestAdminMailNilPermissionAuthorityFailsClosed(t *testing.T) {
	store := &fakeAdminMailStore{}
	api, err := NewAdminMailAPI(store, fixedAdminPrincipalResolver{principal: RequestPrincipal{UserID: "admin-1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	api.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/admin/mail/queue", nil))
	if res.Code != http.StatusServiceUnavailable || store.queueCalls != 0 {
		t.Fatalf("status=%d queueCalls=%d", res.Code, store.queueCalls)
	}
}

func TestAdminMailSettingsExposeOnlyRuntimeMaskedCredentialState(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	store := &fakeAdminMailStore{settings: AdminMailSettings{Enabled: true, Version: 3, UpdatedAt: now}}
	permissions := &recordingAdminPermissionResolver{allowed: true}
	api, err := NewAdminMailAPI(store, fixedAdminPrincipalResolver{principal: RequestPrincipal{UserID: "admin-1"}}, permissions)
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	api.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/admin/mail/settings", nil))
	body := res.Body.String()
	if res.Code != http.StatusOK || !strings.Contains(body, `"credentials_masked":true`) || !strings.Contains(body, `"credential_source":"runtime"`) {
		t.Fatalf("status=%d body=%s", res.Code, body)
	}
	lower := strings.ToLower(body)
	for _, forbidden := range []string{"password", "username", "smtp_addr", "smtp_from"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("settings response leaked credential field %q: %s", forbidden, body)
		}
	}
}

func TestAdminMailSettingsRejectUnknownCredentialMutation(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	store := &fakeAdminMailStore{updateSettings: AdminMailSettings{Enabled: false, Version: 2, UpdatedAt: now}}
	permissions := &recordingAdminPermissionResolver{allowed: true}
	api, err := NewAdminMailAPI(store, fixedAdminPrincipalResolver{principal: RequestPrincipal{UserID: "admin-1"}}, permissions)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/mail/settings", strings.NewReader(`{"enabled":false,"expected_version":1,"smtp_password":"do-not-store"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	api.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest || store.updateCalls != 0 {
		t.Fatalf("status=%d updateCalls=%d body=%s", res.Code, store.updateCalls, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "do-not-store") {
		t.Fatal("rejected credential value echoed in response")
	}
}

func TestAdminMailSettingsRequireOptimisticVersion(t *testing.T) {
	store := &fakeAdminMailStore{}
	permissions := &recordingAdminPermissionResolver{allowed: true}
	api, err := NewAdminMailAPI(store, fixedAdminPrincipalResolver{principal: RequestPrincipal{UserID: "admin-1"}}, permissions)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/mail/settings", strings.NewReader(`{"enabled":false,"expected_version":0}`))
	res := httptest.NewRecorder()
	api.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest || store.updateCalls != 0 {
		t.Fatalf("status=%d updateCalls=%d", res.Code, store.updateCalls)
	}
}

func TestAdminMailTestSendIsIdempotencyRequiredAndResponseSafe(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	store := &fakeAdminMailStore{testCreated: true, testJob: AdminMailQueueItem{
		ID: "mail-1", TemplateKey: "mail-test", TemplateVersion: 1, RecipientKind: "admin_test",
		ResourceType: "mail_test", ResourceID: "test_deadbeef", Status: MailQueued, CreatedAt: now, UpdatedAt: now,
	}}
	permissions := &recordingAdminPermissionResolver{allowed: true}
	api, err := NewAdminMailAPI(store, fixedAdminPrincipalResolver{principal: RequestPrincipal{UserID: "admin-1"}}, permissions)
	if err != nil {
		t.Fatal(err)
	}
	missingKey := httptest.NewRecorder()
	api.Handler().ServeHTTP(missingKey, httptest.NewRequest(http.MethodPost, "/api/admin/mail/test", strings.NewReader(`{"recipient":"safe@example.test"}`)))
	if missingKey.Code != http.StatusBadRequest || store.testCalls != 0 {
		t.Fatalf("missing-key status=%d testCalls=%d", missingKey.Code, store.testCalls)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/mail/test", strings.NewReader(`{"recipient":"safe@example.test"}`))
	req.Header.Set("Idempotency-Key", "mail-test-1")
	res := httptest.NewRecorder()
	api.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated || store.testCalls != 1 {
		t.Fatalf("status=%d testCalls=%d body=%s", res.Code, store.testCalls, res.Body.String())
	}
	if store.lastTestInput.IdempotencyKeyHash == ([32]byte{}) {
		t.Fatal("test send did not hash idempotency material")
	}
	if strings.Contains(res.Body.String(), "safe@example.test") || strings.Contains(res.Body.String(), "mail-test-1") {
		t.Fatalf("test response leaked recipient or raw idempotency key: %s", res.Body.String())
	}
}
