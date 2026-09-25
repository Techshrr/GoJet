package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/redis/go-redis/v9"
)

type domainsSessionAuthority struct {
	resolvePrincipal func(*http.Request) (workspace.Principal, error)
	lookupRole       func(context.Context, string, string) (string, error)
}

func buildDomainsSessionAuthority(db *sql.DB, redisClient *redis.Client) (*domainsSessionAuthority, error) {
	if db == nil || redisClient == nil {
		return nil, domains.ErrAuthenticationUnavailable
	}
	workspaceAuthority, err := buildWorkspaceSessionAuthority(db, redisClient)
	if err != nil {
		return nil, err
	}
	return &domainsSessionAuthority{
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

func (a *domainsSessionAuthority) resolve(request *http.Request, workspaceID string) (domains.Actor, error) {
	if a == nil || a.resolvePrincipal == nil || a.lookupRole == nil || request == nil {
		return domains.Actor{}, domains.ErrAuthenticationUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return domains.Actor{}, domains.ErrForbidden
	}
	principal, err := a.resolvePrincipal(request)
	if err != nil {
		return domains.Actor{}, domainsAuthorityError(err)
	}
	userID := strings.TrimSpace(principal.UserID)
	if userID == "" {
		return domains.Actor{}, domains.ErrAuthenticationUnavailable
	}
	role, err := a.lookupRole(request.Context(), workspaceID, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domains.Actor{}, domains.ErrForbidden
		}
		return domains.Actor{}, domains.ErrAuthenticationUnavailable
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "owner" && role != "admin" && role != "member" && role != "viewer" {
		return domains.Actor{}, domains.ErrForbidden
	}
	return domains.Actor{ActorID: userID, Role: role}, nil
}

func domainsAuthorityError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrAuthenticationRequired):
		return domains.ErrAuthenticationRequired
	case errors.Is(err, workspace.ErrForbidden):
		return domains.ErrForbidden
	default:
		return domains.ErrAuthenticationUnavailable
	}
}
