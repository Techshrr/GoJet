package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/Techshrr/GoJet/internal/support"
	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/redis/go-redis/v9"
)

// supportSessionPrincipalResolver adapts the established P15 customer session
// boundary to the P14 requester Support principal contract. Workspace
// membership/role authority remains inside the Support API and is re-resolved
// against the ticket/requested Workspace; this resolver supplies identity only.
type supportSessionPrincipalResolver struct {
	resolvePrincipal func(*http.Request) (workspace.Principal, error)
}

func buildSupportSessionPrincipalResolver(db *sql.DB, redisClient *redis.Client) (*supportSessionPrincipalResolver, error) {
	if db == nil || redisClient == nil {
		return nil, support.ErrAuthenticationUnavailable
	}
	workspaceAuthority, err := buildWorkspaceSessionAuthority(db, redisClient)
	if err != nil {
		return nil, err
	}
	return &supportSessionPrincipalResolver{resolvePrincipal: workspaceAuthority.resolve}, nil
}

func (r *supportSessionPrincipalResolver) ResolvePrincipal(req *http.Request) (support.RequestPrincipal, error) {
	if r == nil || r.resolvePrincipal == nil || req == nil {
		return support.RequestPrincipal{}, support.ErrAuthenticationUnavailable
	}
	principal, err := r.resolvePrincipal(req)
	if err != nil {
		return support.RequestPrincipal{}, supportRequesterAuthorityError(err)
	}
	userID := strings.TrimSpace(principal.UserID)
	email := strings.TrimSpace(principal.Email)
	if userID == "" || email == "" {
		return support.RequestPrincipal{}, support.ErrAuthenticationUnavailable
	}
	return support.RequestPrincipal{
		UserID:      userID,
		Email:       email,
		DisplayName: strings.TrimSpace(principal.DisplayName),
	}, nil
}

func supportRequesterAuthorityError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrAuthenticationRequired):
		return support.ErrAuthenticationRequired
	case errors.Is(err, workspace.ErrForbidden):
		// The P14 PrincipalResolver contract does not expose a separate CSRF/Origin
		// error type. Treat an unsafe-request authorization denial as failed
		// requester authentication rather than dependency unavailability. The
		// established workspace authority has already enforced Origin and one-time
		// CSRF/replay semantics before this mapping occurs.
		return support.ErrAuthenticationRequired
	default:
		return support.ErrAuthenticationUnavailable
	}
}
