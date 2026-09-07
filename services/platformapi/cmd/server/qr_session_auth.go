package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	qrcodes "github.com/Techshrr/GoJet/internal/qr"
	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/redis/go-redis/v9"
)

type qrSessionAuthority struct {
	resolvePrincipal func(*http.Request) (workspace.Principal, error)
	lookupRole       func(context.Context, string, string) (string, error)
}

func buildQRSessionAuthority(db *sql.DB, redisClient *redis.Client) (*qrSessionAuthority, error) {
	if db == nil || redisClient == nil {
		return nil, qrcodes.ErrAuthenticationUnavailable
	}
	workspaceAuthority, err := buildWorkspaceSessionAuthority(db, redisClient)
	if err != nil {
		return nil, err
	}
	return &qrSessionAuthority{
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

func (a *qrSessionAuthority) resolve(request *http.Request, workspaceID string) (qrcodes.Actor, error) {
	if a == nil || a.resolvePrincipal == nil || a.lookupRole == nil || request == nil {
		return qrcodes.Actor{}, qrcodes.ErrAuthenticationUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return qrcodes.Actor{}, qrcodes.ErrForbidden
	}

	principal, err := a.resolvePrincipal(request)
	if err != nil {
		return qrcodes.Actor{}, qrAuthorityError(err)
	}
	userID := strings.TrimSpace(principal.UserID)
	if userID == "" {
		return qrcodes.Actor{}, qrcodes.ErrAuthenticationUnavailable
	}

	role, err := a.lookupRole(request.Context(), workspaceID, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return qrcodes.Actor{}, qrcodes.ErrForbidden
		}
		return qrcodes.Actor{}, qrcodes.ErrAuthenticationUnavailable
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "owner" && role != "admin" && role != "member" && role != "viewer" {
		return qrcodes.Actor{}, qrcodes.ErrForbidden
	}
	return qrcodes.Actor{ActorID: userID, Role: role}, nil
}

func qrAuthorityError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrAuthenticationRequired):
		return qrcodes.ErrAuthenticationRequired
	case errors.Is(err, workspace.ErrForbidden):
		return qrcodes.ErrForbidden
	default:
		return qrcodes.ErrAuthenticationUnavailable
	}
}
