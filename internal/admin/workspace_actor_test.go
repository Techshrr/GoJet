package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkspaceDeveloperHandlersUseResolver(t *testing.T) {
	for _, kind := range []string{"api-key", "webhook"} {
		t.Run(kind, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", nil)
			resolve := WorkspaceActorResolver(func(got *http.Request) (string, error) {
				if got != request {
					t.Fatal("resolver did not receive the original request")
				}
				return "customer-session-owner", nil
			})
			var actor func(http.ResponseWriter, *http.Request) (string, bool)
			if kind == "api-key" {
				api, err := NewWorkspaceAPIKeyHTTPAPIWithActorResolver(&WorkspaceAPIKeyAuthority{}, resolve)
				if err != nil {
					t.Fatal(err)
				}
				actor = api.actor
			} else {
				api, err := NewWorkspaceWebhookHTTPAPIWithActorResolver(&WorkspaceWebhookAuthority{}, resolve)
				if err != nil {
					t.Fatal(err)
				}
				actor = api.actor
			}
			identity, ok := actor(httptest.NewRecorder(), request)
			if !ok || identity != "customer-session-owner" {
				t.Fatal("production handler did not use customer session identity")
			}
		})
	}
}

func TestWorkspaceDeveloperActorPreservesDenials(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"session", ErrUnauthorized, 401, "authentication_required"},
		{"unsafe request", ErrForbidden, 403, "forbidden"},
		{"dependency", ErrWorkspaceAuthenticationUnavailable, 503, "auth_dependency_unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRecorder()
			identity, ok := workspaceDeveloperActor(r, httptest.NewRequest(http.MethodPost, "/", nil), func(*http.Request) (string, error) {
				return "", test.err
			}, false)
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if ok || identity != "" || r.Code != test.status || body.Error.Code != test.code {
				t.Fatalf("denial changed: status=%d code=%s", r.Code, body.Error.Code)
			}
		})
	}
}
