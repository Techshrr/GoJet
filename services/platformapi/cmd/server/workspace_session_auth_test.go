package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/workspace"
)

func TestWorkspaceSessionAuthorityResolvesActiveVerifiedUser(t *testing.T) {
	t.Parallel()
	verified := time.Now().UTC()
	unsafeCalled := false
	a := &workspaceSessionAuthority{
		authenticate: func(context.Context, *http.Request, time.Time) (authn.Session, error) {
			return authn.Session{ID: "ses_test", UserID: "usr_real", Status: authn.SessionStatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)}, nil
		},
		getUser: func(context.Context, string) (authn.User, error) {
			return authn.User{ID: "usr_real", Email: "real@example.test", DisplayName: "Real User", Status: authn.UserStatusActive, EmailVerifiedAt: &verified}, nil
		},
		authorizeUnsafe: func(context.Context, *http.Request, authn.Session, time.Time) error {
			unsafeCalled = true
			return nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	req.Header.Set("X-GoJet-Test-Actor", "usr_spoof")
	principal, err := a.resolve(req)
	if err != nil {
		t.Fatal(err)
	}
	if principal.UserID != "usr_real" || principal.Email != "real@example.test" || principal.DisplayName != "Real User" {
		t.Fatalf("unexpected principal: %+v", principal)
	}
	if unsafeCalled {
		t.Fatal("safe GET unexpectedly consumed unsafe mutation authority")
	}
}

func TestWorkspaceSessionAuthorityRejectsMissingRevokedAndExpiredSessions(t *testing.T) {
	t.Parallel()
	for name, authErr := range map[string]error{
		"missing": authn.ErrUnauthorized,
		"revoked": authn.ErrRevoked,
		"expired": authn.ErrExpired,
	} {
		t.Run(name, func(t *testing.T) {
			a := &workspaceSessionAuthority{
				authenticate: func(context.Context, *http.Request, time.Time) (authn.Session, error) { return authn.Session{}, authErr },
				getUser: func(context.Context, string) (authn.User, error) { return authn.User{}, errors.New("must not run") },
				authorizeUnsafe: func(context.Context, *http.Request, authn.Session, time.Time) error { return errors.New("must not run") },
			}
			_, err := a.resolve(httptest.NewRequest(http.MethodGet, "/api/workspaces", nil))
			if !errors.Is(err, workspace.ErrAuthenticationRequired) {
				t.Fatalf("got %v, want authentication required", err)
			}
		})
	}
}

func TestWorkspaceSessionAuthorityRequiresOriginAndOneTimeCSRFOnUnsafeMethods(t *testing.T) {
	t.Parallel()
	verified := time.Now().UTC()
	for name, authorityErr := range map[string]error{
		"bad-origin": authn.ErrForbidden,
		"replayed-csrf": authn.ErrReplay,
		"expired-csrf": authn.ErrExpired,
	} {
		t.Run(name, func(t *testing.T) {
			unsafeCalled := false
			a := &workspaceSessionAuthority{
				authenticate: func(context.Context, *http.Request, time.Time) (authn.Session, error) {
					return authn.Session{ID: "ses_test", UserID: "usr_real", Status: authn.SessionStatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)}, nil
				},
				getUser: func(context.Context, string) (authn.User, error) {
					return authn.User{ID: "usr_real", Email: "real@example.test", Status: authn.UserStatusActive, EmailVerifiedAt: &verified}, nil
				},
				authorizeUnsafe: func(context.Context, *http.Request, authn.Session, time.Time) error {
					unsafeCalled = true
					return authorityErr
				},
			}
			_, err := a.resolve(httptest.NewRequest(http.MethodPost, "/api/workspaces", nil))
			if !unsafeCalled {
				t.Fatal("unsafe authority was not evaluated")
			}
			if !errors.Is(err, workspace.ErrForbidden) {
				t.Fatalf("got %v, want forbidden", err)
			}
		})
	}
}

func TestWorkspaceSessionAuthorityRejectsInactiveOrUnverifiedUser(t *testing.T) {
	t.Parallel()
	for name, user := range map[string]authn.User{
		"disabled": {ID: "usr_real", Email: "real@example.test", Status: authn.UserStatusDisabled},
		"unverified": {ID: "usr_real", Email: "real@example.test", Status: authn.UserStatusActive},
	} {
		t.Run(name, func(t *testing.T) {
			a := &workspaceSessionAuthority{
				authenticate: func(context.Context, *http.Request, time.Time) (authn.Session, error) {
					return authn.Session{ID: "ses_test", UserID: "usr_real", Status: authn.SessionStatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)}, nil
				},
				getUser: func(context.Context, string) (authn.User, error) { return user, nil },
				authorizeUnsafe: func(context.Context, *http.Request, authn.Session, time.Time) error { return nil },
			}
			_, err := a.resolve(httptest.NewRequest(http.MethodGet, "/api/workspaces", nil))
			if !errors.Is(err, workspace.ErrAuthenticationRequired) {
				t.Fatalf("got %v, want authentication required", err)
			}
		})
	}
}
