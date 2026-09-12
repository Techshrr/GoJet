package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/Techshrr/GoJet/internal/links"
	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/redis/go-redis/v9"
)

type linksSessionAuthority struct {
	resolvePrincipal func(*http.Request) (workspace.Principal, error)
	lookupRole       func(context.Context, string, string) (string, error)
}

func buildLinksSessionAuthority(db *sql.DB, redisClient *redis.Client) (*linksSessionAuthority, error) {
	if db == nil || redisClient == nil {
		return nil, links.ErrAuthenticationUnavailable
	}
	workspaceAuthority, err := buildWorkspaceSessionAuthority(db, redisClient)
	if err != nil {
		return nil, err
	}
	return &linksSessionAuthority{
		resolvePrincipal: workspaceAuthority.resolve,
		lookupRole: func(ctx context.Context, workspaceID, userID string) (string, error) {
			var role string
			err := db.QueryRowContext(ctx,
				"SELECT role FROM workspace_memberships WHERE workspace_id=? AND user_id=?",
				workspaceID, userID,
			).Scan(&role)
			return role, err
		},
	}, nil
}

func (a *linksSessionAuthority) resolve(request *http.Request, workspaceID string) (links.Actor, error) {
	if a == nil || a.resolvePrincipal == nil || a.lookupRole == nil || request == nil {
		return links.Actor{}, links.ErrAuthenticationUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return links.Actor{}, links.ErrForbidden
	}

	principal, err := a.resolvePrincipal(request)
	if err != nil {
		return links.Actor{}, linksAuthorityError(err)
	}
	userID := strings.TrimSpace(principal.UserID)
	if userID == "" {
		return links.Actor{}, links.ErrAuthenticationUnavailable
	}

	role, err := a.lookupRole(request.Context(), workspaceID, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return links.Actor{}, links.ErrForbidden
		}
		return links.Actor{}, links.ErrAuthenticationUnavailable
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "owner" && role != "admin" && role != "member" && role != "viewer" {
		return links.Actor{}, links.ErrForbidden
	}
	return links.Actor{ActorID: userID, Role: role}, nil
}

func linksAuthorityError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrAuthenticationRequired):
		return links.ErrAuthenticationRequired
	case errors.Is(err, workspace.ErrForbidden):
		return links.ErrForbidden
	default:
		return links.ErrAuthenticationUnavailable
	}
}
