package bio

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBioActorResolverPrefersServerAuthorityOverTestHeaders(t *testing.T) {
	api := &API{actorResolver: func(r *http.Request, workspaceID string) (Actor, error) {
		if workspaceID != "real-workspace" {
			t.Fatalf("unexpected workspace %q", workspaceID)
		}
		if r.Header.Get("X-GoJet-Test-Actor") != "spoofed" {
			t.Fatal("test precondition missing spoofed header")
		}
		return Actor{ActorID: "real-user", Role: "owner"}, nil
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/real-workspace/bio-pages", nil)
	req.Header.Set("X-GoJet-Test-Actor", "spoofed")
	req.Header.Set("X-GoJet-Test-Workspace-Role", "viewer")
	req.Header.Set("X-GoJet-Test-Workspace", "real-workspace")
	recorder := httptest.NewRecorder()

	actor, ok := api.authenticate(recorder, req, "real-workspace", true)
	if !ok {
		t.Fatalf("resolver authority rejected: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if actor.ActorID != "real-user" || actor.Role != "owner" {
		t.Fatalf("test headers influenced authoritative actor: %#v", actor)
	}
}

func TestBioActorResolverPreservesViewerReadOnly(t *testing.T) {
	api := &API{actorResolver: func(*http.Request, string) (Actor, error) {
		return Actor{ActorID: "real-user", Role: "viewer"}, nil
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/real-workspace/bio-pages", nil)
	recorder := httptest.NewRecorder()
	if _, ok := api.authenticate(recorder, req, "real-workspace", true); ok {
		t.Fatal("viewer mutation unexpectedly authorized")
	}
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"read_only"`) {
		t.Fatalf("unexpected viewer denial: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestBioActorResolverAllowsViewerRead(t *testing.T) {
	api := &API{actorResolver: func(*http.Request, string) (Actor, error) {
		return Actor{ActorID: "real-user", Role: "viewer"}, nil
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/real-workspace/bio-pages", nil)
	recorder := httptest.NewRecorder()
	actor, ok := api.authenticate(recorder, req, "real-workspace", false)
	if !ok || actor.ActorID != "real-user" || actor.Role != "viewer" {
		t.Fatalf("viewer read authority rejected: ok=%v actor=%#v status=%d body=%s", ok, actor, recorder.Code, recorder.Body.String())
	}
}

func TestBioActorResolverMapsAuthorityErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
		code string
	}{
		{"required", ErrAuthenticationRequired, http.StatusUnauthorized, "authentication_required"},
		{"forbidden", ErrForbidden, http.StatusForbidden, "forbidden"},
		{"unavailable", ErrAuthenticationUnavailable, http.StatusServiceUnavailable, "auth_dependency_unavailable"},
		{"unexpected", errors.New("backend failure"), http.StatusServiceUnavailable, "auth_dependency_unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &API{actorResolver: func(*http.Request, string) (Actor, error) { return Actor{}, tt.err }}
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/workspaces/real-workspace/bio-pages", nil)
			if _, ok := api.authenticate(recorder, req, "real-workspace", false); ok {
				t.Fatal("authority error unexpectedly authorized")
			}
			if recorder.Code != tt.want || !strings.Contains(recorder.Body.String(), `"code":"`+tt.code+`"`) {
				t.Fatalf("unexpected authority mapping: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestBioActorResolverRejectsUnknownRole(t *testing.T) {
	api := &API{actorResolver: func(*http.Request, string) (Actor, error) {
		return Actor{ActorID: "real-user", Role: "superuser"}, nil
	}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/real-workspace/bio-pages", nil)
	if _, ok := api.authenticate(recorder, req, "real-workspace", false); ok {
		t.Fatal("unknown role unexpectedly authorized")
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("unknown role status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
