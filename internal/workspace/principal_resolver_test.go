package workspace

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrincipalResolverTakesAuthorityOverTestHeaders(t *testing.T) {
	t.Parallel()
	api := NewAPIWithPrincipalResolver(nil, func(*http.Request) (Principal, error) {
		return Principal{UserID: "usr_real", Email: "real@example.test", DisplayName: "Real"}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	req.Header.Set("X-GoJet-Test-Actor", "usr_spoof")
	req.Header.Set("X-GoJet-Test-Email", "spoof@example.test")
	recorder := httptest.NewRecorder()
	principal, ok := api.principal(recorder, req)
	if !ok {
		t.Fatalf("resolver principal rejected: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if principal.UserID != "usr_real" || principal.Email != "real@example.test" {
		t.Fatalf("test headers substituted production resolver authority: %+v", principal)
	}
}

func TestPrincipalResolverErrorMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"required", ErrAuthenticationRequired, http.StatusUnauthorized, "authentication_required"},
		{"forbidden", ErrForbidden, http.StatusForbidden, "forbidden"},
		{"unavailable", ErrAuthenticationUnavailable, http.StatusServiceUnavailable, "auth_dependency_unavailable"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			api := NewAPIWithPrincipalResolver(nil, func(*http.Request) (Principal, error) { return Principal{}, test.err })
			recorder := httptest.NewRecorder()
			_, ok := api.principal(recorder, httptest.NewRequest(http.MethodGet, "/api/workspaces", nil))
			if ok {
				t.Fatal("resolver error unexpectedly accepted")
			}
			if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("status/body = %d %s; want %d %s", recorder.Code, recorder.Body.String(), test.status, test.code)
			}
		})
	}
}

func TestPrincipalResolverRejectsIncompletePrincipal(t *testing.T) {
	t.Parallel()
	api := NewAPIWithPrincipalResolver(nil, func(*http.Request) (Principal, error) {
		return Principal{UserID: "usr_real"}, nil
	})
	recorder := httptest.NewRecorder()
	_, ok := api.principal(recorder, httptest.NewRequest(http.MethodGet, "/api/workspaces", nil))
	if ok || recorder.Code != http.StatusUnauthorized {
		t.Fatalf("incomplete principal accepted: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
