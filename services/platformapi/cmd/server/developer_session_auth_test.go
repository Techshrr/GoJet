package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/internal/workspace"
)

func TestDeveloperSessionActorPreservesWorkspaceAuthority(t *testing.T) {
	for _, test := range []struct {
		name      string
		principal workspace.Principal
		err       error
		wantID    string
		wantErr   error
	}{
		{name: "active verified customer", principal: workspace.Principal{UserID: "usr_owner", Email: "owner@example.test"}, wantID: "usr_owner"},
		{name: "incomplete identity", principal: workspace.Principal{UserID: "usr_owner"}, wantErr: adminaccess.ErrWorkspaceAuthenticationUnavailable},
		{name: "missing session", err: workspace.ErrAuthenticationRequired, wantErr: adminaccess.ErrUnauthorized},
		{name: "CSRF denied", err: workspace.ErrForbidden, wantErr: adminaccess.ErrForbidden},
		{name: "session dependency unavailable", err: workspace.ErrAuthenticationUnavailable, wantErr: adminaccess.ErrWorkspaceAuthenticationUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/workspaces/ws/api-keys", nil)
			called := false
			resolve := developerSessionActor(func(got *http.Request) (workspace.Principal, error) {
				called = true
				if got != request {
					t.Fatal("shared authority did not receive the original request")
				}
				return test.principal, test.err
			})
			id, err := resolve(request)
			if !called || id != test.wantID || !errors.Is(err, test.wantErr) {
				t.Fatalf("identity=%q error=%v; want identity=%q error=%v", id, err, test.wantID, test.wantErr)
			}
		})
	}
}
