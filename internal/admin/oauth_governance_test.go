package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
)

func TestOAuthGovernanceRejectsCustomerSession(t *testing.T) {
	api, err := NewHTTPAPI(&Service{})
	if err != nil {
		t.Fatal(err)
	}
	handler := api.OAuthGovernanceHandler(nil)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/oauth/providers", nil)
	request.AddCookie(&http.Cookie{Name: authn.SessionCookieName, Value: "customer-session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("customer session must not authorize admin OAuth: %d", response.Code)
	}
	for _, path := range []string{"/api/admin/oauth/providers/google", "/api/admin/oauth/providers/google/test"} {
		method := http.MethodPatch
		if path == "/api/admin/oauth/providers/google/test" {
			method = http.MethodPost
		}
		request := httptest.NewRequest(method, path, nil)
		request.Header.Set("Origin", "https://foreign.example")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("foreign origin accepted: %d", response.Code)
		}
	}
}

func TestOAuthGovernanceRequiresSettingsPermission(t *testing.T) {
	service := &Service{}
	_, _, err := service.UpdateOAuthProvider(context.Background(), Principal{}, nil, authn.OAuthProviderUpdate{}, 1, MutationAuthority{}, time.Now())
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("missing permission reached configuration write: %v", err)
	}
}
