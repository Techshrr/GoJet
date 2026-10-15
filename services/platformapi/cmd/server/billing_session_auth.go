package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/Techshrr/GoJet/internal/billing"
	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/redis/go-redis/v9"
)

// billingSessionPrincipalResolver adapts the established P15 customer session
// boundary to the P13 Billing principal contract. Workspace membership and role
// authority remain inside Billing and are re-resolved against the requested
// Workspace; this resolver supplies authenticated customer identity only.
type billingSessionPrincipalResolver struct {
	resolvePrincipal func(*http.Request) (workspace.Principal, error)
}

func buildBillingSessionPrincipalResolver(db *sql.DB, redisClient *redis.Client) (*billingSessionPrincipalResolver, error) {
	if db == nil || redisClient == nil {
		return nil, billing.ErrAuthenticationUnavailable
	}
	workspaceAuthority, err := buildWorkspaceSessionAuthority(db, redisClient)
	if err != nil {
		return nil, err
	}
	return &billingSessionPrincipalResolver{resolvePrincipal: workspaceAuthority.resolve}, nil
}

func (r *billingSessionPrincipalResolver) ResolvePrincipal(req *http.Request) (billing.RequestPrincipal, error) {
	if r == nil || r.resolvePrincipal == nil || req == nil {
		return billing.RequestPrincipal{}, billing.ErrAuthenticationUnavailable
	}
	principal, err := r.resolvePrincipal(req)
	if err != nil {
		return billing.RequestPrincipal{}, billingSessionAuthorityError(err)
	}
	userID := strings.TrimSpace(principal.UserID)
	email := strings.TrimSpace(principal.Email)
	if userID == "" || email == "" {
		return billing.RequestPrincipal{}, billing.ErrAuthenticationUnavailable
	}
	return billing.RequestPrincipal{
		UserID:      userID,
		Email:       email,
		DisplayName: strings.TrimSpace(principal.DisplayName),
	}, nil
}

func billingSessionAuthorityError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrAuthenticationRequired):
		return billing.ErrAuthenticationRequired
	case errors.Is(err, workspace.ErrForbidden):
		// Billing's PrincipalResolver contract does not expose a separate
		// Origin/CSRF error. The shared P15/Workspace authority has already
		// enforced the unsafe-request policy, so surface the denial as failed
		// customer authentication rather than dependency unavailability.
		return billing.ErrAuthenticationRequired
	default:
		return billing.ErrAuthenticationUnavailable
	}
}
