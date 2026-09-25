package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	qrcodes "github.com/Techshrr/GoJet/internal/qr"
	"github.com/Techshrr/GoJet/internal/workspace"
)

func TestQRSessionAuthorityResolvesPrincipalAndMembership(t *testing.T) {
	authority := &qrSessionAuthority{
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
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/real-workspace/qr-codes", nil)
	req.Header.Set("X-GoJet-Test-Actor", "spoofed")
	req.Header.Set("X-GoJet-Test-Workspace-Role", "viewer")

	actor, err := authority.resolve(req, "real-workspace")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if actor.ActorID != "real-user" || actor.Role != "owner" {
		t.Fatalf("test headers influenced authoritative actor: %#v", actor)
	}
}

func TestQRSessionAuthorityRequiresMembership(t *testing.T) {
	authority := &qrSessionAuthority{
		resolvePrincipal: func(*http.Request) (workspace.Principal, error) {
			return workspace.Principal{UserID: "real-user"}, nil
		},
		lookupRole: func(context.Context, string, string) (string, error) {
			return "", sql.ErrNoRows
		},
	}
	_, err := authority.resolve(httptest.NewRequest(http.MethodGet, "/", nil), "workspace-without-membership")
	if !errors.Is(err, qrcodes.ErrForbidden) {
		t.Fatalf("missing membership did not fail forbidden: %v", err)
	}
}

func TestQRSessionAuthorityFailsUnavailableOnMembershipBackendError(t *testing.T) {
	authority := &qrSessionAuthority{
		resolvePrincipal: func(*http.Request) (workspace.Principal, error) {
			return workspace.Principal{UserID: "real-user"}, nil
		},
		lookupRole: func(context.Context, string, string) (string, error) {
			return "", errors.New("mysql unavailable")
		},
	}
	_, err := authority.resolve(httptest.NewRequest(http.MethodGet, "/", nil), "real-workspace")
	if !errors.Is(err, qrcodes.ErrAuthenticationUnavailable) {
		t.Fatalf("membership backend failure did not fail unavailable: %v", err)
	}
}

func TestQRSessionAuthorityRejectsUnknownRole(t *testing.T) {
	authority := &qrSessionAuthority{
		resolvePrincipal: func(*http.Request) (workspace.Principal, error) {
			return workspace.Principal{UserID: "real-user"}, nil
		},
		lookupRole: func(context.Context, string, string) (string, error) {
			return "superuser", nil
		},
	}
	_, err := authority.resolve(httptest.NewRequest(http.MethodGet, "/", nil), "real-workspace")
	if !errors.Is(err, qrcodes.ErrForbidden) {
		t.Fatalf("unknown role was not rejected: %v", err)
	}
}

func TestQRAuthorityErrorMapping(t *testing.T) {
	tests := []struct {
		input error
		want  error
	}{
		{workspace.ErrAuthenticationRequired, qrcodes.ErrAuthenticationRequired},
		{workspace.ErrForbidden, qrcodes.ErrForbidden},
		{workspace.ErrAuthenticationUnavailable, qrcodes.ErrAuthenticationUnavailable},
		{errors.New("unexpected"), qrcodes.ErrAuthenticationUnavailable},
	}
	for _, tt := range tests {
		if got := qrAuthorityError(tt.input); !errors.Is(got, tt.want) {
			t.Fatalf("mapping %v: got %v want %v", tt.input, got, tt.want)
		}
	}
}
