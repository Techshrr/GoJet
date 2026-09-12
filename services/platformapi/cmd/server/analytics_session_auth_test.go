package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Techshrr/GoJet/internal/analytics"
	"github.com/Techshrr/GoJet/internal/workspace"
)

func TestAnalyticsSessionAuthorityResolvesPrincipalAndMembership(t *testing.T) {
	authority := &analyticsSessionAuthority{
		resolvePrincipal: func(r *http.Request) (workspace.Principal, error) {
			if r.Header.Get("X-GoJet-Test-Actor") != "spoofed" {
				t.Fatal("test precondition missing spoofed header")
			}
			return workspace.Principal{UserID: "real-user", Email: "real@example.test"}, nil
		},
		lookupRole: func(ctx context.Context, workspaceID, userID string) (string, error) {
			if workspaceID != "real-workspace" || userID != "real-user" {
				t.Fatalf("unexpected membership lookup %q/%q", workspaceID, userID)
			}
			return "owner", nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/real-workspace/analytics/overview", nil)
	req.Header.Set("X-GoJet-Test-Actor", "spoofed")
	req.Header.Set("X-GoJet-Test-Workspace-Role", "viewer")
	req.Header.Set("X-GoJet-Test-Analytics-Permission", "allow")

	actor, err := authority.resolve(req, "real-workspace")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if actor.ActorID != "real-user" || actor.Role != "owner" {
		t.Fatalf("test headers influenced authoritative actor: %#v", actor)
	}
}

func TestAnalyticsSessionAuthorityRequiresMembership(t *testing.T) {
	authority := &analyticsSessionAuthority{
		resolvePrincipal: func(*http.Request) (workspace.Principal, error) {
			return workspace.Principal{UserID: "real-user"}, nil
		},
		lookupRole: func(context.Context, string, string) (string, error) {
			return "", sql.ErrNoRows
		},
	}
	_, err := authority.resolve(httptest.NewRequest(http.MethodGet, "/", nil), "workspace-without-membership")
	if !errors.Is(err, analytics.ErrForbidden) {
		t.Fatalf("missing membership did not fail forbidden: %v", err)
	}
}

func TestAnalyticsSessionAuthorityFailsUnavailableOnMembershipBackendError(t *testing.T) {
	authority := &analyticsSessionAuthority{
		resolvePrincipal: func(*http.Request) (workspace.Principal, error) {
			return workspace.Principal{UserID: "real-user"}, nil
		},
		lookupRole: func(context.Context, string, string) (string, error) {
			return "", errors.New("mysql unavailable")
		},
	}
	_, err := authority.resolve(httptest.NewRequest(http.MethodGet, "/", nil), "real-workspace")
	if !errors.Is(err, analytics.ErrAuthenticationUnavailable) {
		t.Fatalf("membership backend failure did not fail unavailable: %v", err)
	}
}

func TestAnalyticsSessionAuthorityRejectsUnknownRole(t *testing.T) {
	authority := &analyticsSessionAuthority{
		resolvePrincipal: func(*http.Request) (workspace.Principal, error) {
			return workspace.Principal{UserID: "real-user"}, nil
		},
		lookupRole: func(context.Context, string, string) (string, error) {
			return "superuser", nil
		},
	}
	_, err := authority.resolve(httptest.NewRequest(http.MethodGet, "/", nil), "real-workspace")
	if !errors.Is(err, analytics.ErrForbidden) {
		t.Fatalf("unknown role was not rejected: %v", err)
	}
}

func TestAnalyticsAuthorityErrorMapping(t *testing.T) {
	tests := []struct {
		input error
		want  error
	}{
		{workspace.ErrAuthenticationRequired, analytics.ErrAuthenticationRequired},
		{workspace.ErrForbidden, analytics.ErrForbidden},
		{workspace.ErrAuthenticationUnavailable, analytics.ErrAuthenticationUnavailable},
		{errors.New("unexpected"), analytics.ErrAuthenticationUnavailable},
	}
	for _, tt := range tests {
		if got := analyticsAuthorityError(tt.input); !errors.Is(got, tt.want) {
			t.Fatalf("mapping %v: got %v want %v", tt.input, got, tt.want)
		}
	}
}
