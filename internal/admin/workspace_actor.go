package admin

import (
	"errors"
	"net/http"
	"strings"
)

// WorkspaceActorResolver supplies customer identity only. Membership, role and
// resource ownership remain checked by the API-key and webhook authorities.
type WorkspaceActorResolver func(*http.Request) (string, error)

var ErrWorkspaceAuthenticationUnavailable = errors.New("workspace authentication unavailable")

func workspaceDeveloperActor(w http.ResponseWriter, r *http.Request, resolve WorkspaceActorResolver, testAuth bool) (string, bool) {
	if resolve != nil {
		actor, err := resolve(r)
		if err != nil {
			switch {
			case errors.Is(err, ErrUnauthorized):
				apiKeyWriteError(w, http.StatusUnauthorized, "authentication_required")
			case errors.Is(err, ErrForbidden):
				apiKeyWriteError(w, http.StatusForbidden, "forbidden")
			default:
				apiKeyWriteError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable")
			}
			return "", false
		}
		if strings.TrimSpace(actor) == "" {
			apiKeyWriteError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable")
			return "", false
		}
		return actor, true
	}
	if !testAuth {
		apiKeyWriteError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable")
		return "", false
	}
	actor := strings.TrimSpace(r.Header.Get("X-GoJet-Test-Actor"))
	email := strings.TrimSpace(r.Header.Get("X-GoJet-Test-Email"))
	if actor == "" || email == "" {
		apiKeyWriteError(w, http.StatusUnauthorized, "authentication_required")
		return "", false
	}
	return actor, true
}
