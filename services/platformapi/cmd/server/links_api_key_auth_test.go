package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/links"
	"github.com/Techshrr/GoJet/internal/workspace"
)

func TestLinksAPIKeyConsumer(t *testing.T) {
	for _, tc := range []struct {
		name         string
		method       string
		role         string
		keyWorkspace string
		status       string
		authErr      error
		want         error
	}{
		{"read", http.MethodGet, "viewer", "ws", authn.UserStatusActive, nil, nil},
		{"write", http.MethodPost, "member", "ws", authn.UserStatusActive, nil, nil},
		{"current read-only role", http.MethodPatch, "viewer", "ws", authn.UserStatusActive, nil, links.ErrForbidden},
		{"workspace mismatch", http.MethodGet, "owner", "other", authn.UserStatusActive, nil, links.ErrForbidden},
		{"disabled creator", http.MethodGet, "owner", "ws", "disabled", nil, links.ErrAuthenticationRequired},
		{"invalid credential", http.MethodGet, "owner", "ws", authn.UserStatusActive, adminaccess.ErrUnauthorized, links.ErrAuthenticationRequired},
		{"missing scope", http.MethodGet, "owner", "ws", authn.UserStatusActive, adminaccess.ErrForbidden, links.ErrForbidden},
		{"rate exhausted", http.MethodGet, "owner", "ws", authn.UserStatusActive, adminaccess.ErrRateLimited, links.ErrRateLimited},
		{"dependency unavailable", http.MethodGet, "owner", "ws", authn.UserStatusActive, errors.New("unavailable"), links.ErrAuthenticationUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			a := &linksSessionAuthority{
				resolvePrincipal: func(*http.Request) (workspace.Principal, error) {
					t.Fatal("explicit API key fell back to session authority")
					return workspace.Principal{}, nil
				},
				authenticateKey: func(_ context.Context, secret, scope string, _ time.Time) (adminaccess.WorkspaceAPIKey, error) {
					wantScope := "links:read"
					if tc.method != http.MethodGet {
						wantScope = "links:write"
					}
					if scope != wantScope || secret != "gak_unit_test" {
						t.Fatal("incorrect credential or route scope binding")
					}
					return adminaccess.WorkspaceAPIKey{ID: "key", CreatedBy: "user", WorkspaceID: tc.keyWorkspace}, tc.authErr
				},
				getKeyUser: func(context.Context, string) (authn.User, error) {
					return authn.User{ID: "user", Email: "user@example.test", Status: tc.status, EmailVerifiedAt: &now}, nil
				},
				lookupRole: func(context.Context, string, string) (string, error) { return tc.role, nil },
			}
			r := httptest.NewRequest(tc.method, "/api/workspaces/ws/links", nil)
			r.Header.Set("Authorization", "Bearer gak_unit_test")
			actor, err := a.resolve(r, "ws")
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if err == nil && (actor.ActorID != "user" || actor.Role != tc.role) {
				t.Fatal("incorrect authoritative actor")
			}
		})
	}
}
