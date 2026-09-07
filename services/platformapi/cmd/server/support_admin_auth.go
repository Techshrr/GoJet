package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/internal/support"
	"github.com/redis/go-redis/v9"
)

type supportAdminAuthorityContextKey struct{}

// supportAdminAuthority adapts the established P17 administrator
// session/permission boundary to P14 Support Admin contracts. The
// authenticated P17 principal is carried only in the current request
// context so permission checks cannot leak or be reused across requests.
type supportAdminAuthority struct {
	authenticate   func(context.Context, string, time.Time) (adminaccess.Principal, error)
	validateOrigin func(string) bool
	validateCSRF   func(adminaccess.Principal, string) bool
	now            func() time.Time
}

func buildSupportAdminAuthority(db *sql.DB, redisClient *redis.Client) (*supportAdminAuthority, bool, error) {
	service, _, enabled, err := buildAdminAccessService(db, redisClient)
	if err != nil || !enabled {
		return nil, enabled, err
	}
	return newSupportAdminAuthority(service), true, nil
}

func newSupportAdminAuthority(service *adminaccess.Service) *supportAdminAuthority {
	if service == nil {
		return nil
	}
	return &supportAdminAuthority{
		authenticate:   service.Authenticate,
		validateOrigin: service.ValidateOrigin,
		validateCSRF:   service.ValidateCSRF,
		now:            func() time.Time { return time.Now().UTC() },
	}
}

func (a *supportAdminAuthority) ResolvePrincipal(req *http.Request) (support.RequestPrincipal, error) {
	if a == nil || a.authenticate == nil || a.validateOrigin == nil || a.validateCSRF == nil || a.now == nil || req == nil {
		return support.RequestPrincipal{}, support.ErrAuthenticationUnavailable
	}
	cookie, err := req.Cookie(adminaccess.AdminSessionCookie)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return support.RequestPrincipal{}, support.ErrAuthenticationRequired
	}
	principal, err := a.authenticate(req.Context(), cookie.Value, a.now())
	if err != nil {
		return support.RequestPrincipal{}, supportAdminAuthorityError(err)
	}
	if supportAdminUnsafeMethod(req.Method) {
		if !a.validateOrigin(req.Header.Get("Origin")) || !a.validateCSRF(principal, req.Header.Get("X-CSRF-Token")) {
			return support.RequestPrincipal{}, support.ErrAuthenticationRequired
		}
	}
	adminID := strings.TrimSpace(principal.Administrator.ID)
	email := strings.TrimSpace(principal.Administrator.Email)
	if adminID == "" || email == "" {
		return support.RequestPrincipal{}, support.ErrAuthenticationUnavailable
	}
	*req = *req.WithContext(context.WithValue(req.Context(), supportAdminAuthorityContextKey{}, principal))
	return support.RequestPrincipal{
		UserID:      adminID,
		Email:       email,
		DisplayName: strings.TrimSpace(principal.Administrator.DisplayName),
	}, nil
}

func (a *supportAdminAuthority) HasPermission(ctx context.Context, principal support.RequestPrincipal, permission string) (bool, error) {
	if a == nil || ctx == nil {
		return false, support.ErrAuthenticationUnavailable
	}
	authority, ok := ctx.Value(supportAdminAuthorityContextKey{}).(adminaccess.Principal)
	if !ok || strings.TrimSpace(authority.Administrator.ID) == "" || authority.Administrator.ID != strings.TrimSpace(principal.UserID) {
		return false, support.ErrAuthenticationUnavailable
	}
	switch permission {
	case support.TicketsManagePermission:
		return authority.Has(adminaccess.PermissionTicketsManage), nil
	case support.MailManagePermission:
		return authority.Has(adminaccess.PermissionMailManage), nil
	default:
		return false, nil
	}
}

func supportAdminUnsafeMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func supportAdminAuthorityError(err error) error {
	switch {
	case errors.Is(err, adminaccess.ErrUnauthorized),
		errors.Is(err, adminaccess.ErrForbidden),
		errors.Is(err, adminaccess.ErrLocked),
		errors.Is(err, adminaccess.ErrMFARequired),
		errors.Is(err, adminaccess.ErrMFAInvalid):
		return support.ErrAuthenticationRequired
	default:
		return support.ErrAuthenticationUnavailable
	}
}
