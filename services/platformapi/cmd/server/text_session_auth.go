package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	textdomain "github.com/Techshrr/GoJet/internal/text"
	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/redis/go-redis/v9"
)

type textSessionAuthority struct {
	resolvePrincipal func(*http.Request) (workspace.Principal, error)
	lookupRole       func(context.Context, string, string) (string, error)
}

func buildTextSessionAuthority(db *sql.DB, redisClient *redis.Client) (*textSessionAuthority, error) {
	if db == nil || redisClient == nil {
		return nil, textdomain.ErrAuthenticationUnavailable
	}
	workspaceAuthority, err := buildWorkspaceSessionAuthority(db, redisClient)
	if err != nil {
		return nil, err
	}
	return &textSessionAuthority{
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

func (a *textSessionAuthority) resolve(request *http.Request, workspaceID string) (textdomain.Actor, error) {
	if a == nil || a.resolvePrincipal == nil || a.lookupRole == nil || request == nil {
		return textdomain.Actor{}, textdomain.ErrAuthenticationUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return textdomain.Actor{}, textdomain.ErrForbidden
	}

	principal, err := a.resolvePrincipal(request)
	if err != nil {
		return textdomain.Actor{}, textAuthorityError(err)
	}
	userID := strings.TrimSpace(principal.UserID)
	if userID == "" {
		return textdomain.Actor{}, textdomain.ErrAuthenticationUnavailable
	}

	role, err := a.lookupRole(request.Context(), workspaceID, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return textdomain.Actor{}, textdomain.ErrForbidden
		}
		return textdomain.Actor{}, textdomain.ErrAuthenticationUnavailable
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "owner" && role != "admin" && role != "member" && role != "viewer" {
		return textdomain.Actor{}, textdomain.ErrForbidden
	}
	return textdomain.Actor{ActorID: userID, Role: role}, nil
}

func textAuthorityError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrAuthenticationRequired):
		return textdomain.ErrAuthenticationRequired
	case errors.Is(err, workspace.ErrForbidden):
		return textdomain.ErrForbidden
	default:
		return textdomain.ErrAuthenticationUnavailable
	}
}
