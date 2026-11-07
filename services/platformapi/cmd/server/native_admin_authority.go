package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/internal/billing"
	"github.com/Techshrr/GoJet/internal/trust"
	"github.com/redis/go-redis/v9"
)

var errNativeAdminUnavailable = errors.New("administrator authority unavailable")

type nativeAdminContextKey struct{}

// Native modules consume the dedicated administrator session. Authority is
// bound to this request, never to a shared handler or client-supplied actor.
type nativeAdminAuthority struct {
	authenticate   func(context.Context, string, time.Time) (adminaccess.Principal, error)
	validateOrigin func(string) bool
	validateCSRF   func(adminaccess.Principal, string) bool
	rotateCSRF     func(context.Context, adminaccess.Principal, time.Time) (string, error)
}

func buildNativeAdminAuthority(db *sql.DB, redisClient *redis.Client) (*nativeAdminAuthority, error) {
	service, _, enabled, err := buildAdminAccessService(db, redisClient)
	if err != nil {
		return nil, err
	}
	if !enabled || service == nil {
		return nil, errNativeAdminUnavailable
	}
	return &nativeAdminAuthority{service.Authenticate, service.ValidateOrigin, service.ValidateCSRF, service.RotateCSRF}, nil
}

func (a *nativeAdminAuthority) resolve(r *http.Request) (adminaccess.Principal, error) {
	if a == nil || a.authenticate == nil || r == nil {
		return adminaccess.Principal{}, errNativeAdminUnavailable
	}
	cookie, err := r.Cookie(adminaccess.AdminSessionCookie)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return adminaccess.Principal{}, adminaccess.ErrUnauthorized
	}
	p, err := a.authenticate(r.Context(), cookie.Value, time.Now().UTC())
	if err != nil {
		return adminaccess.Principal{}, err
	}
	if p.Administrator.ID == "" || p.Session.ID == "" {
		return adminaccess.Principal{}, errNativeAdminUnavailable
	}
	if supportAdminUnsafeMethod(r.Method) {
		if a.validateOrigin == nil || a.validateCSRF == nil {
			return adminaccess.Principal{}, errNativeAdminUnavailable
		}
		if !a.validateOrigin(r.Header.Get("Origin")) || !a.validateCSRF(p, r.Header.Get("X-CSRF-Token")) {
			return adminaccess.Principal{}, adminaccess.ErrForbidden
		}
	}
	*r = *r.WithContext(context.WithValue(r.Context(), nativeAdminContextKey{}, p))
	return p, nil
}

func nativeAdminPrincipal(ctx context.Context, actor string) (adminaccess.Principal, bool) {
	if ctx == nil {
		return adminaccess.Principal{}, false
	}
	p, ok := ctx.Value(nativeAdminContextKey{}).(adminaccess.Principal)
	return p, ok && actor != "" && actor == p.Administrator.ID
}

func (a *nativeAdminAuthority) Authorize(ctx context.Context, actor, permission string) error {
	p, ok := nativeAdminPrincipal(ctx, actor)
	if !ok {
		return trust.ErrUnauthorized
	}
	return adminaccess.NewP16TrustAuthorizer(p).Authorize(ctx, actor, permission)
}

// Billing's workspace API keeps its own resolver. This adapter is used only
// by the separately mounted /api/admin/ billing API.
func (a *nativeAdminAuthority) ResolvePrincipal(r *http.Request) (billing.RequestPrincipal, error) {
	p, err := a.resolve(r)
	if err != nil {
		if errors.Is(err, adminaccess.ErrUnauthorized) || errors.Is(err, adminaccess.ErrForbidden) || errors.Is(err, adminaccess.ErrLocked) || errors.Is(err, adminaccess.ErrMFARequired) || errors.Is(err, adminaccess.ErrMFAInvalid) {
			return billing.RequestPrincipal{}, billing.ErrAuthenticationRequired
		}
		return billing.RequestPrincipal{}, billing.ErrAuthenticationUnavailable
	}
	return billing.RequestPrincipal{UserID: p.Administrator.ID, Email: p.Administrator.Email, DisplayName: p.Administrator.DisplayName}, nil
}

func (a *nativeAdminAuthority) HasPermission(ctx context.Context, p billing.RequestPrincipal, permission string) (bool, error) {
	authority, ok := nativeAdminPrincipal(ctx, p.UserID)
	if !ok {
		return false, billing.ErrAuthenticationUnavailable
	}
	return permission == billing.BillingManagePermission && authority.Has(adminaccess.PermissionBillingManage), nil
}

func writeNativeAdminError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "auth_dependency_unavailable"
	switch {
	case errors.Is(err, adminaccess.ErrUnauthorized), errors.Is(err, adminaccess.ErrLocked), errors.Is(err, adminaccess.ErrMFAInvalid):
		status, code = http.StatusUnauthorized, "authentication_required"
	case errors.Is(err, adminaccess.ErrForbidden):
		status, code = http.StatusForbidden, "forbidden"
	case errors.Is(err, adminaccess.ErrMFARequired):
		status, code = http.StatusPreconditionRequired, "totp_required"
	}
	writeAuthJSON(w, status, map[string]any{"error": code})
}
