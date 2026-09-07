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
// boundary to the P13 Billing customer principal contract. Workspace membership
// and role authority remain server-authoritative inside Billing.
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
		return billing.RequestPrincipal{}, billingCustomerAuthorityError(err)
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

func billingCustomerAuthorityError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrAuthenticationRequired), errors.Is(err, workspace.ErrForbidden):
		// P15 has already enforced Origin and one-time CSRF/replay before this map.
		return billing.ErrAuthenticationRequired
	default:
		return billing.ErrAuthenticationUnavailable
	}
}
