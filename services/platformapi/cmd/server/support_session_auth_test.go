package main

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Techshrr/GoJet/internal/support"
	"github.com/Techshrr/GoJet/internal/workspace"
)

func TestSupportSessionPrincipalResolverMapsRealPrincipal(t *testing.T) {
	resolver := &supportSessionPrincipalResolver{
		resolvePrincipal: func(*http.Request) (workspace.Principal, error) {
			return workspace.Principal{UserID: "usr_real", Email: "real@example.test", DisplayName: "Real User"}, nil
		},
	}
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/api/support/tickets?workspace_id=ws_real", nil)
	principal, err := resolver.ResolvePrincipal(req)
	if err != nil {
		t.Fatalf("ResolvePrincipal() error = %v", err)
	}
	if principal.UserID != "usr_real" || principal.Email != "real@example.test" || principal.DisplayName != "Real User" {
		t.Fatalf("unexpected principal: %#v", principal)
	}
}

func TestSupportSessionPrincipalResolverErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		in   error
		want error
	}{
		{name: "authentication required", in: workspace.ErrAuthenticationRequired, want: support.ErrAuthenticationRequired},
		{name: "unsafe request forbidden", in: workspace.ErrForbidden, want: support.ErrAuthenticationRequired},
		{name: "dependency unavailable", in: workspace.ErrAuthenticationUnavailable, want: support.ErrAuthenticationUnavailable},
		{name: "unknown dependency failure", in: errors.New("boom"), want: support.ErrAuthenticationUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &supportSessionPrincipalResolver{
				resolvePrincipal: func(*http.Request) (workspace.Principal, error) { return workspace.Principal{}, tt.in },
			}
			req, _ := http.NewRequest(http.MethodPost, "http://localhost/api/support/tickets", nil)
			_, err := resolver.ResolvePrincipal(req)
			if !errors.Is(err, tt.want) {
				t.Fatalf("ResolvePrincipal() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSupportSessionPrincipalResolverRejectsIncompleteAuthority(t *testing.T) {
	resolver := &supportSessionPrincipalResolver{
		resolvePrincipal: func(*http.Request) (workspace.Principal, error) {
			return workspace.Principal{UserID: "usr_real"}, nil
		},
	}
	req, _ := http.NewRequest(http.MethodGet, "http://localhost/api/support/tickets?workspace_id=ws_real", nil)
	_, err := resolver.ResolvePrincipal(req)
	if !errors.Is(err, support.ErrAuthenticationUnavailable) {
		t.Fatalf("ResolvePrincipal() error = %v, want auth unavailable", err)
	}
}
