package main

import (
	"context"
	"errors"
	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/internal/billing"
	"github.com/Techshrr/GoJet/internal/trust"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func nativeAuthorityFixture() *nativeAdminAuthority {
	return &nativeAdminAuthority{
		authenticate: func(_ context.Context, token string, _ time.Time) (adminaccess.Principal, error) {
			if token == "revoked" {
				return adminaccess.Principal{}, adminaccess.ErrUnauthorized
			}
			if token == "unavailable" {
				return adminaccess.Principal{}, errors.New("dependency failed")
			}
			return adminaccess.Principal{Administrator: adminaccess.Administrator{ID: "adm_a", Email: "admin@example.test"}, Session: adminaccess.Session{ID: "ads_a"}, Permissions: map[string]struct{}{adminaccess.PermissionSecurityManage: {}, adminaccess.PermissionBillingManage: {}}}, nil
		},
		validateOrigin: func(s string) bool { return s == "https://admin.example.test" },
		validateCSRF:   func(_ adminaccess.Principal, s string) bool { return s == "csrf-current" },
	}
}

func TestNativeAdminAuthorityBoundary(t *testing.T) {
	a := nativeAuthorityFixture()
	for _, tc := range []struct {
		name, method, cookieName, token, origin, csrf string
		want                                          error
	}{
		{"read", "GET", adminaccess.AdminSessionCookie, "valid", "", "", nil},
		{"mutation", "POST", adminaccess.AdminSessionCookie, "valid", "https://admin.example.test", "csrf-current", nil},
		{"workspace-cookie", "GET", "__Host-gojet_session", "valid", "", "", adminaccess.ErrUnauthorized},
		{"missing-cookie", "GET", "", "", "", "", adminaccess.ErrUnauthorized},
		{"revoked", "GET", adminaccess.AdminSessionCookie, "revoked", "", "", adminaccess.ErrUnauthorized},
		{"wrong-origin", "POST", adminaccess.AdminSessionCookie, "valid", "https://other.test", "csrf-current", adminaccess.ErrForbidden},
		{"missing-csrf", "POST", adminaccess.AdminSessionCookie, "valid", "https://admin.example.test", "", adminaccess.ErrForbidden},
		{"stale-csrf", "POST", adminaccess.AdminSessionCookie, "valid", "https://admin.example.test", "old", adminaccess.ErrForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "https://admin.example.test/api/admin/plans", nil)
			if tc.cookieName != "" {
				r.AddCookie(&http.Cookie{Name: tc.cookieName, Value: tc.token})
			}
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-CSRF-Token", tc.csrf)
			r.Header.Set("X-GoJet-Test-Actor", "forged-admin")
			p, err := a.resolve(r)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
			if err == nil {
				if p.Administrator.ID != "adm_a" {
					t.Fatal("client actor trusted")
				}
				if a.Authorize(r.Context(), "adm_a", trust.SecurityManagePermission) != nil {
					t.Fatal("security denied")
				}
				if a.Authorize(r.Context(), "adm_a", trust.DomainsRiskManagePermission) == nil {
					t.Fatal("permission widened")
				}
				if a.Authorize(r.Context(), "other", trust.SecurityManagePermission) == nil {
					t.Fatal("actor mismatch accepted")
				}
				ok, err := a.HasPermission(r.Context(), billing.RequestPrincipal{UserID: "adm_a"}, billing.BillingManagePermission)
				if !ok || err != nil {
					t.Fatal("billing permission lost")
				}
			}
		})
	}
	if a.Authorize(context.Background(), "adm_a", trust.SecurityManagePermission) == nil {
		t.Fatal("request authority leaked")
	}
	r := httptest.NewRequest("GET", "/api/admin/plans", nil)
	r.AddCookie(&http.Cookie{Name: adminaccess.AdminSessionCookie, Value: "unavailable"})
	if _, err := a.ResolvePrincipal(r); !errors.Is(err, billing.ErrAuthenticationUnavailable) {
		t.Fatal("dependency failure not distinguished")
	}
}
