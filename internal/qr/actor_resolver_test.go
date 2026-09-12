package qrcodes

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProductionActorResolverIgnoresTestHeaders(t *testing.T) {
	api := NewAPIWithActorResolver(nil, nil, nil, func(r *http.Request, workspaceID string) (Actor, error) {
		if workspaceID != "real-workspace" {
			t.Fatalf("unexpected workspace %q", workspaceID)
		}
		if r.Header.Get("X-GoJet-Test-Actor") != "spoofed" {
			t.Fatal("test precondition missing spoofed header")
		}
		return Actor{ActorID: "real-user", Role: "owner"}, nil
	})
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/real-workspace/qr-codes", nil)
	req.Header.Set("X-GoJet-Test-Actor", "spoofed")
	req.Header.Set("X-GoJet-Test-Workspace", "real-workspace")
	req.Header.Set("X-GoJet-Test-Workspace-Role", "viewer")
	w := httptest.NewRecorder()

	actor, ok := api.authenticate(w, req, "real-workspace", true)
	if !ok {
		t.Fatalf("production resolver rejected valid actor: status=%d body=%s", w.Code, w.Body.String())
	}
	if actor.ActorID != "real-user" || actor.Role != "owner" {
		t.Fatalf("test headers influenced production actor: %#v", actor)
	}
}

func TestProductionActorResolverMapsAuthenticationRequired(t *testing.T) {
	api := NewAPIWithActorResolver(nil, nil, nil, func(*http.Request, string) (Actor, error) {
		return Actor{}, ErrAuthenticationRequired
	})
	w := httptest.NewRecorder()
	_, ok := api.authenticate(w, httptest.NewRequest(http.MethodGet, "/", nil), "workspace", false)
	if ok || w.Code != http.StatusUnauthorized {
		t.Fatalf("authentication-required mapping: ok=%v status=%d body=%s", ok, w.Code, w.Body.String())
	}
}

func TestProductionActorResolverPreservesViewerReadOnly(t *testing.T) {
	api := NewAPIWithActorResolver(nil, nil, nil, func(*http.Request, string) (Actor, error) {
		return Actor{ActorID: "viewer-user", Role: "viewer"}, nil
	})
	w := httptest.NewRecorder()
	_, ok := api.authenticate(w, httptest.NewRequest(http.MethodPost, "/", nil), "workspace", true)
	if ok || w.Code != http.StatusForbidden || !containsErrorCode(w.Body.String(), "read_only") {
		t.Fatalf("viewer mutation mapping: ok=%v status=%d body=%s", ok, w.Code, w.Body.String())
	}
}

func TestPredecessorTestAuthAdapterRemainsAvailable(t *testing.T) {
	api := NewAPI(nil, nil, nil, true)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-GoJet-Test-Actor", "p08-user")
	req.Header.Set("X-GoJet-Test-Workspace", "p08-workspace")
	req.Header.Set("X-GoJet-Test-Workspace-Role", "member")
	w := httptest.NewRecorder()
	actor, ok := api.authenticate(w, req, "p08-workspace", true)
	if !ok || actor.ActorID != "p08-user" || actor.Role != "member" {
		t.Fatalf("predecessor adapter changed: ok=%v actor=%#v status=%d body=%s", ok, actor, w.Code, w.Body.String())
	}
}

func TestProductionResolverUnavailableFailsClosed(t *testing.T) {
	api := NewAPIWithActorResolver(nil, nil, nil, func(*http.Request, string) (Actor, error) {
		return Actor{}, errors.New("backend failed")
	})
	w := httptest.NewRecorder()
	_, ok := api.authenticate(w, httptest.NewRequest(http.MethodGet, "/", nil), "workspace", false)
	if ok || w.Code != http.StatusServiceUnavailable || !containsErrorCode(w.Body.String(), "auth_dependency_unavailable") {
		t.Fatalf("unavailable resolver did not fail closed: ok=%v status=%d body=%s", ok, w.Code, w.Body.String())
	}
}

func containsErrorCode(body, code string) bool {
	return len(body) > 0 && (body == `{"error":{"code":"`+code+`"}}` || contains(body, `"code":"`+code+`"`))
}

func contains(value, fragment string) bool {
	if fragment == "" {
		return true
	}
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
