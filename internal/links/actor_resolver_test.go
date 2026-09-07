package links

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProductionActorResolverIgnoresSpoofedTestHeaders(t *testing.T) {
	api := NewAPIWithActorResolver(nil, func(r *http.Request, workspaceID string) (Actor, error) {
		if workspaceID != "ws-real" {
			t.Fatalf("unexpected workspace: %q", workspaceID)
		}
		return Actor{ActorID: "user-real", Role: "owner"}, nil
	})
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/ws-real/links", nil)
	req.Header.Set("X-GoJet-Test-Actor", "user-spoof")
	req.Header.Set("X-GoJet-Test-Workspace", "ws-real")
	req.Header.Set("X-GoJet-Test-Workspace-Role", "viewer")
	res := httptest.NewRecorder()

	actor, ok := api.authenticate(res, req, "ws-real", true)
	if !ok || res.Code != http.StatusOK {
		t.Fatalf("production resolver rejected authoritative actor: ok=%v status=%d", ok, res.Code)
	}
	if actor.ActorID != "user-real" || actor.Role != "owner" {
		t.Fatalf("spoofed test headers influenced production actor: %#v", actor)
	}
}

func TestProductionActorResolverErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "authentication required", err: ErrAuthenticationRequired, want: http.StatusUnauthorized},
		{name: "forbidden", err: ErrForbidden, want: http.StatusForbidden},
		{name: "unavailable", err: ErrAuthenticationUnavailable, want: http.StatusServiceUnavailable},
		{name: "unknown fails unavailable", err: errors.New("backend failure"), want: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := NewAPIWithActorResolver(nil, func(*http.Request, string) (Actor, error) {
				return Actor{}, tt.err
			})
			res := httptest.NewRecorder()
			_, ok := api.authenticate(res, httptest.NewRequest(http.MethodGet, "/", nil), "ws-real", false)
			if ok || res.Code != tt.want {
				t.Fatalf("unexpected result: ok=%v status=%d want=%d", ok, res.Code, tt.want)
			}
		})
	}
}

func TestProductionViewerRemainsReadOnly(t *testing.T) {
	api := NewAPIWithActorResolver(nil, func(*http.Request, string) (Actor, error) {
		return Actor{ActorID: "user-real", Role: "viewer"}, nil
	})
	res := httptest.NewRecorder()
	_, ok := api.authenticate(res, httptest.NewRequest(http.MethodPatch, "/", nil), "ws-real", true)
	if ok || res.Code != http.StatusForbidden {
		t.Fatalf("viewer mutation was not denied: ok=%v status=%d", ok, res.Code)
	}
}

func TestProductionResolverRejectsIncompleteOrUnknownActor(t *testing.T) {
	tests := []Actor{
		{ActorID: "", Role: "owner"},
		{ActorID: "user-real", Role: "superuser"},
	}
	for _, candidate := range tests {
		api := NewAPIWithActorResolver(nil, func(*http.Request, string) (Actor, error) { return candidate, nil })
		res := httptest.NewRecorder()
		_, ok := api.authenticate(res, httptest.NewRequest(http.MethodGet, "/", nil), "ws-real", false)
		if ok {
			t.Fatalf("invalid production actor accepted: %#v", candidate)
		}
	}
}

func TestPredecessorTestAuthAdapterRemainsAvailable(t *testing.T) {
	api := NewAPI(nil, true)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-GoJet-Test-Actor", "p05-user")
	req.Header.Set("X-GoJet-Test-Workspace", "p05-workspace")
	req.Header.Set("X-GoJet-Test-Workspace-Role", "member")
	res := httptest.NewRecorder()

	actor, ok := api.authenticate(res, req, "p05-workspace", true)
	if !ok || actor.ActorID != "p05-user" || actor.Role != "member" {
		t.Fatalf("predecessor adapter regressed: ok=%v actor=%#v status=%d", ok, actor, res.Code)
	}
}
