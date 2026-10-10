package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	filedomain "github.com/Techshrr/GoJet/internal/files"
	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/redis/go-redis/v9"
)

type filesSessionAuthority struct {
	resolvePrincipal func(*http.Request) (workspace.Principal, error)
	lookupRole       func(context.Context, string, string) (string, error)
}

func buildFilesSessionAuthority(db *sql.DB, redisClient *redis.Client) (*filesSessionAuthority, error) {
	if db == nil || redisClient == nil {
		return nil, filedomain.ErrAuthenticationUnavailable
	}
	workspaceAuthority, err := buildWorkspaceSessionAuthority(db, redisClient)
	if err != nil {
		return nil, err
	}
	return &filesSessionAuthority{
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

func (a *filesSessionAuthority) resolve(request *http.Request, workspaceID string) (filedomain.Actor, error) {
	if a == nil || a.resolvePrincipal == nil || a.lookupRole == nil || request == nil {
		return filedomain.Actor{}, filedomain.ErrAuthenticationUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return filedomain.Actor{}, filedomain.ErrForbidden
	}

	principal, err := a.resolvePrincipal(request)
	if err != nil {
		return filedomain.Actor{}, filesAuthorityError(err)
	}
	userID := strings.TrimSpace(principal.UserID)
	if userID == "" {
		return filedomain.Actor{}, filedomain.ErrAuthenticationUnavailable
	}

	role, err := a.lookupRole(request.Context(), workspaceID, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return filedomain.Actor{}, filedomain.ErrForbidden
		}
		return filedomain.Actor{}, filedomain.ErrAuthenticationUnavailable
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "owner" && role != "admin" && role != "member" && role != "viewer" {
		return filedomain.Actor{}, filedomain.ErrForbidden
	}
	return filedomain.Actor{ActorID: userID, Role: role}, nil
}

func filesAuthorityError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrAuthenticationRequired):
		return filedomain.ErrAuthenticationRequired
	case errors.Is(err, workspace.ErrForbidden):
		return filedomain.ErrForbidden
	default:
		return filedomain.ErrAuthenticationUnavailable
	}
}
