package main

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Techshrr/GoJet/internal/billing"
	"github.com/Techshrr/GoJet/internal/workspace"
)

func TestBillingSessionPrincipalResolverMapsRealPrincipal(t *testing.T) {
	resolver := &billingSessionPrincipalResolver{
		resolvePrincipal: func(*http.Request) (workspace.Principal, error) {
			return workspace.Principal{UserID: "usr_real", Email: "real@example.test", DisplayName: "Real User"}, nil
		},
	}
	req, _ := http.NewRequest(http.MethodPost, "http://localhost/api/workspaces/ws_real/orders", nil)
	req.Header.Set("X-GoJet-Test-Actor", "spoofed-test-actor")
	principal, err := resolver.ResolvePrincipal(req)
	if err != nil {
		t.Fatalf("ResolvePrincipal() error = %v", err)
	}
	if principal.UserID != "usr_real" || principal.Email != "real@example.test" || principal.DisplayName != "Real User" {
		t.Fatalf("unexpected principal: %#v", principal)
	}
}

func TestBillingSessionPrincipalResolverErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		in   error
		want error
	}{
		{name: "authentication required", in: workspace.ErrAuthenticationRequired, want: billing.ErrAuthenticationRequired},
		{name: "unsafe request forbidden", in: workspace.ErrForbidden, want: billing.ErrAuthenticationRequired},
		{name: "dependency unavailable", in: workspace.ErrAuthenticationUnavailable, want: billing.ErrAuthenticationUnavailable},
		{name: "unknown dependency failure", in: errors.New("boom"), want: billing.ErrAuthenticationUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &billingSessionPrincipalResolver{
				resolvePrincipal: func(*http.Request) (workspace.Principal, error) { return workspace.Principal{}, tt.in },
			}
			req, _ := http.NewRequest(http.MethodPost, "http://localhost/api/workspaces/ws_real/orders", nil)
			_, err := resolver.ResolvePrincipal(req)
			if !errors.Is(err, tt.want) {
				t.Fatalf("ResolvePrincipal() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestBillingSessionPrincipalResolverRejectsIncompleteAuthority(t *testing.T) {
	resolver := &billingSessionPrincipalResolver{
		resolvePrincipal: func(*http.Request) (workspace.Principal, error) {
			return workspace.Principal{UserID: "usr_real"}, nil
		},
	}
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/api/workspaces/ws_real/orders/ord_real", nil)
	_, err := resolver.ResolvePrincipal(req)
	if !errors.Is(err, billing.ErrAuthenticationUnavailable) {
		t.Fatalf("ResolvePrincipal() error = %v, want auth unavailable", err)
	}
}
