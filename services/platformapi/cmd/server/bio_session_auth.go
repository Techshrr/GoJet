package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	biodomain "github.com/Techshrr/GoJet/internal/bio"
	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/redis/go-redis/v9"
)

type bioSessionAuthority struct {
	resolvePrincipal func(*http.Request) (workspace.Principal, error)
	lookupRole       func(context.Context, string, string) (string, error)
}

func buildBioSessionAuthority(db *sql.DB, redisClient *redis.Client) (*bioSessionAuthority, error) {
	if db == nil || redisClient == nil {
		return nil, biodomain.ErrAuthenticationUnavailable
	}
	workspaceAuthority, err := buildWorkspaceSessionAuthority(db, redisClient)
	if err != nil {
		return nil, err
	}
	return &bioSessionAuthority{
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

func (a *bioSessionAuthority) resolve(request *http.Request, workspaceID string) (biodomain.Actor, error) {
	if a == nil || a.resolvePrincipal == nil || a.lookupRole == nil || request == nil {
		return biodomain.Actor{}, biodomain.ErrAuthenticationUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return biodomain.Actor{}, biodomain.ErrForbidden
	}

	principal, err := a.resolvePrincipal(request)
	if err != nil {
		return biodomain.Actor{}, bioAuthorityError(err)
	}
	userID := strings.TrimSpace(principal.UserID)
	if userID == "" {
		return biodomain.Actor{}, biodomain.ErrAuthenticationUnavailable
	}

	role, err := a.lookupRole(request.Context(), workspaceID, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return biodomain.Actor{}, biodomain.ErrForbidden
		}
		return biodomain.Actor{}, biodomain.ErrAuthenticationUnavailable
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "owner" && role != "admin" && role != "member" && role != "viewer" {
		return biodomain.Actor{}, biodomain.ErrForbidden
	}
	return biodomain.Actor{ActorID: userID, Role: role}, nil
}

func bioAuthorityError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrAuthenticationRequired):
		return biodomain.ErrAuthenticationRequired
	case errors.Is(err, workspace.ErrForbidden):
		return biodomain.ErrForbidden
	default:
		return biodomain.ErrAuthenticationUnavailable
	}
}
