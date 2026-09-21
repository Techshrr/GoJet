package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/internal/support"
)

func testSupportAdminAuthority(principal adminaccess.Principal) *supportAdminAuthority {
	return &supportAdminAuthority{
		authenticate: func(context.Context, string, time.Time) (adminaccess.Principal, error) {
			return principal, nil
		},
		validateOrigin: func(origin string) bool { return origin == "https://admin.example.test" },
		validateCSRF:   func(adminaccess.Principal, string) bool { return true },
		now:            func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
	}
}

func TestSupportAdminAuthorityMapsP17PrincipalAndPermissions(t *testing.T) {
	p17 := adminaccess.Principal{
		Administrator: adminaccess.Administrator{ID: "adm_real", Email: "admin@example.test", DisplayName: "Real Admin"},
		Permissions: map[string]struct{}{
			adminaccess.PermissionTicketsManage: {},
			adminaccess.PermissionMailManage:    {},
		},
	}
	authority := testSupportAdminAuthority(p17)
	req, _ := http.NewRequest(http.MethodPost, "http://localhost/api/admin/support/tickets/t1/replies", nil)
	req.AddCookie(&http.Cookie{Name: adminaccess.AdminSessionCookie, Value: "gas_real"})
	req.Header.Set("Origin", "https://admin.example.test")
	req.Header.Set("X-CSRF-Token", "csrf")

	principal, err := authority.ResolvePrincipal(req)
	if err != nil {
		t.Fatalf("ResolvePrincipal() error = %v", err)
	}
	if principal.UserID != "adm_real" || principal.Email != "admin@example.test" || principal.DisplayName != "Real Admin" {
		t.Fatalf("unexpected Support principal: %#v", principal)
	}
	for _, permission := range []string{support.TicketsManagePermission, support.MailManagePermission} {
		allowed, err := authority.HasPermission(req.Context(), principal, permission)
		if err != nil || !allowed {
			t.Fatalf("HasPermission(%q) = %v, %v; want true, nil", permission, allowed, err)
		}
	}
}

func TestSupportAdminAuthorityDeniesMissingPermission(t *testing.T) {
	p17 := adminaccess.Principal{
		Administrator: adminaccess.Administrator{ID: "adm_real", Email: "admin@example.test"},
		Permissions:   map[string]struct{}{adminaccess.PermissionTicketsManage: {}},
	}
	authority := testSupportAdminAuthority(p17)
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/api/admin/mail/queue", nil)
	req.AddCookie(&http.Cookie{Name: adminaccess.AdminSessionCookie, Value: "gas_real"})
	principal, err := authority.ResolvePrincipal(req)
	if err != nil {
		t.Fatalf("ResolvePrincipal() error = %v", err)
	}
	allowed, err := authority.HasPermission(req.Context(), principal, support.MailManagePermission)
	if err != nil || allowed {
		t.Fatalf("HasPermission(mail.manage) = %v, %v; want false, nil", allowed, err)
	}
}

func TestSupportAdminAuthorityRequiresP17Cookie(t *testing.T) {
	authority := testSupportAdminAuthority(adminaccess.Principal{})
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/api/admin/support/tickets", nil)
	_, err := authority.ResolvePrincipal(req)
	if !errors.Is(err, support.ErrAuthenticationRequired) {
		t.Fatalf("ResolvePrincipal() error = %v, want authentication required", err)
	}
}

func TestSupportAdminAuthorityEnforcesOriginAndCSRFOnUnsafeRequest(t *testing.T) {
	p17 := adminaccess.Principal{Administrator: adminaccess.Administrator{ID: "adm_real", Email: "admin@example.test"}}
	authority := testSupportAdminAuthority(p17)
	authority.validateCSRF = func(adminaccess.Principal, string) bool { return false }
	req, _ := http.NewRequest(http.MethodPost, "http://localhost/api/admin/support/tickets/t1/replies", nil)
	req.AddCookie(&http.Cookie{Name: adminaccess.AdminSessionCookie, Value: "gas_real"})
	req.Header.Set("Origin", "https://admin.example.test")
	req.Header.Set("X-CSRF-Token", "bad")
	_, err := authority.ResolvePrincipal(req)
	if !errors.Is(err, support.ErrAuthenticationRequired) {
		t.Fatalf("ResolvePrincipal() CSRF error = %v, want authentication required", err)
	}

	authority.validateCSRF = func(adminaccess.Principal, string) bool { return true }
	req.Header.Set("Origin", "https://wrong.example.test")
	_, err = authority.ResolvePrincipal(req)
	if !errors.Is(err, support.ErrAuthenticationRequired) {
		t.Fatalf("ResolvePrincipal() Origin error = %v, want authentication required", err)
	}
}

func TestSupportAdminAuthorityMapsP17AuthenticationFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "unauthorized", err: adminaccess.ErrUnauthorized, want: support.ErrAuthenticationRequired},
		{name: "forbidden", err: adminaccess.ErrForbidden, want: support.ErrAuthenticationRequired},
		{name: "dependency", err: errors.New("db unavailable"), want: support.ErrAuthenticationUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authority := testSupportAdminAuthority(adminaccess.Principal{})
			authority.authenticate = func(context.Context, string, time.Time) (adminaccess.Principal, error) {
				return adminaccess.Principal{}, tt.err
			}
			req, _ := http.NewRequest(http.MethodGet, "http://localhost/api/admin/support/tickets", nil)
			req.AddCookie(&http.Cookie{Name: adminaccess.AdminSessionCookie, Value: "gas_real"})
			_, err := authority.ResolvePrincipal(req)
			if !errors.Is(err, tt.want) {
				t.Fatalf("ResolvePrincipal() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSupportAdminAuthorityDoesNotReusePermissionAcrossRequests(t *testing.T) {
	authority := testSupportAdminAuthority(adminaccess.Principal{})
	principal := support.RequestPrincipal{UserID: "adm_real", Email: "admin@example.test"}
	allowed, err := authority.HasPermission(context.Background(), principal, support.TicketsManagePermission)
	if allowed || !errors.Is(err, support.ErrAuthenticationUnavailable) {
		t.Fatalf("HasPermission() = %v, %v; want false, auth unavailable", allowed, err)
	}
}
