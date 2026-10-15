package main

import (
	"errors"
	"net/http"
	"strings"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/internal/workspace"
)

// Adapt the shared P15 session and one-time CSRF authority without changing
// P17 Workspace membership or API-key/webhook lifecycle semantics.
func developerSessionActor(resolve func(*http.Request) (workspace.Principal, error)) adminaccess.WorkspaceActorResolver {
	return func(r *http.Request) (string, error) {
		if resolve == nil || r == nil {
			return "", adminaccess.ErrWorkspaceAuthenticationUnavailable
		}
		principal, err := resolve(r)
		if err != nil {
			switch {
			case errors.Is(err, workspace.ErrAuthenticationRequired):
				return "", adminaccess.ErrUnauthorized
			case errors.Is(err, workspace.ErrForbidden):
				return "", adminaccess.ErrForbidden
			default:
				return "", adminaccess.ErrWorkspaceAuthenticationUnavailable
			}
		}
		if strings.TrimSpace(principal.UserID) == "" || strings.TrimSpace(principal.Email) == "" {
			return "", adminaccess.ErrWorkspaceAuthenticationUnavailable
		}
		return principal.UserID, nil
	}
}
