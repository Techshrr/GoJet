package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/Techshrr/GoJet/internal/analytics"
	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/redis/go-redis/v9"
)

type analyticsSessionAuthority struct {
	resolvePrincipal func(*http.Request) (workspace.Principal, error)
	lookupRole       func(context.Context, string, string) (string, error)
}

func buildAnalyticsSessionAuthority(db *sql.DB, redisClient *redis.Client) (*analyticsSessionAuthority, error) {
	if db == nil || redisClient == nil {
		return nil, analytics.ErrAuthenticationUnavailable
	}
	workspaceAuthority, err := buildWorkspaceSessionAuthority(db, redisClient)
	if err != nil {
		return nil, err
	}
	return &analyticsSessionAuthority{
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

func (a *analyticsSessionAuthority) resolve(request *http.Request, workspaceID string) (analytics.Actor, error) {
	if a == nil || a.resolvePrincipal == nil || a.lookupRole == nil || request == nil {
		return analytics.Actor{}, analytics.ErrAuthenticationUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return analytics.Actor{}, analytics.ErrForbidden
	}

	principal, err := a.resolvePrincipal(request)
	if err != nil {
		return analytics.Actor{}, analyticsAuthorityError(err)
	}
	userID := strings.TrimSpace(principal.UserID)
	if userID == "" {
		return analytics.Actor{}, analytics.ErrAuthenticationUnavailable
	}

	role, err := a.lookupRole(request.Context(), workspaceID, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return analytics.Actor{}, analytics.ErrForbidden
		}
		return analytics.Actor{}, analytics.ErrAuthenticationUnavailable
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "owner" && role != "admin" && role != "member" && role != "viewer" {
		return analytics.Actor{}, analytics.ErrForbidden
	}
	return analytics.Actor{ActorID: userID, Role: role}, nil
}

func analyticsAuthorityError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrAuthenticationRequired):
		return analytics.ErrAuthenticationRequired
	case errors.Is(err, workspace.ErrForbidden):
		return analytics.ErrForbidden
	default:
		return analytics.ErrAuthenticationUnavailable
	}
}
