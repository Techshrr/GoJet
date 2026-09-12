package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/links"
)

// API keys are explicit non-cookie credentials. Never fall back to the P15
// cookie authority after a supplied credential fails validation.
func (a *linksSessionAuthority) resolveAPIKey(r *http.Request, workspaceID string) (links.Actor, error) {
	if a.authenticateKey == nil || a.getKeyUser == nil {
		return links.Actor{}, links.ErrAuthenticationUnavailable
	}
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return links.Actor{}, links.ErrAuthenticationRequired
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !strings.HasPrefix(parts[1], "gak_") {
		return links.Actor{}, links.ErrAuthenticationRequired
	}
	scope := "links:read"
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	case http.MethodPost, http.MethodPatch, http.MethodDelete:
		scope = "links:write"
	default:
		return links.Actor{}, links.ErrForbidden
	}
	key, err := a.authenticateKey(r.Context(), parts[1], scope, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, adminaccess.ErrUnauthorized):
			return links.Actor{}, links.ErrAuthenticationRequired
		case errors.Is(err, adminaccess.ErrForbidden):
			return links.Actor{}, links.ErrForbidden
		case errors.Is(err, adminaccess.ErrRateLimited):
			return links.Actor{}, links.ErrRateLimited
		default:
			return links.Actor{}, links.ErrAuthenticationUnavailable
		}
	}
	if key.WorkspaceID != workspaceID || strings.TrimSpace(key.CreatedBy) == "" || strings.TrimSpace(key.ID) == "" {
		return links.Actor{}, links.ErrForbidden
	}
	user, err := a.getKeyUser(r.Context(), key.CreatedBy)
	if errors.Is(err, authn.ErrNotFound) {
		return links.Actor{}, links.ErrAuthenticationRequired
	}
	if err != nil {
		return links.Actor{}, links.ErrAuthenticationUnavailable
	}
	if user.ID != key.CreatedBy || user.Status != authn.UserStatusActive || user.EmailVerifiedAt == nil || strings.TrimSpace(user.Email) == "" {
		return links.Actor{}, links.ErrAuthenticationRequired
	}
	role, err := a.lookupRole(r.Context(), workspaceID, key.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return links.Actor{}, links.ErrForbidden
	}
	if err != nil {
		return links.Actor{}, links.ErrAuthenticationUnavailable
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "owner" && role != "admin" && role != "member" && role != "viewer" {
		return links.Actor{}, links.ErrForbidden
	}
	if scope == "links:write" && role == "viewer" {
		return links.Actor{}, links.ErrForbidden
	}
	return links.Actor{ActorID: key.CreatedBy, Role: role}, nil
}
