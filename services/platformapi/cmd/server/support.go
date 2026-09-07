package main

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/support"
	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/redis/go-redis/v9"
)

type supportPrincipalResolver struct {
	testAuth bool
}

func (r supportPrincipalResolver) ResolvePrincipal(req *http.Request) (support.RequestPrincipal, error) {
	if !r.testAuth {
		return support.RequestPrincipal{}, support.ErrAuthenticationUnavailable
	}
	principal := support.RequestPrincipal{
		UserID:      strings.TrimSpace(req.Header.Get("X-GoJet-Test-Actor")),
		Email:       strings.TrimSpace(req.Header.Get("X-GoJet-Test-Email")),
		DisplayName: strings.TrimSpace(req.Header.Get("X-GoJet-Test-Display-Name")),
	}
	if principal.UserID == "" || principal.Email == "" {
		return support.RequestPrincipal{}, support.ErrAuthenticationRequired
	}
	return principal, nil
}

type supportTestTicketAdminPermissionResolver struct {
	actorID string
}

func (r supportTestTicketAdminPermissionResolver) HasPermission(_ context.Context, principal support.RequestPrincipal, permission string) (bool, error) {
	return permission == support.TicketsManagePermission && strings.TrimSpace(principal.UserID) == r.actorID, nil
}

type supportTestMailAdminPermissionResolver struct {
	actorID string
}

func (r supportTestMailAdminPermissionResolver) HasPermission(_ context.Context, principal support.RequestPrincipal, permission string) (bool, error) {
	return permission == support.MailManagePermission && strings.TrimSpace(principal.UserID) == r.actorID, nil
}

func buildSupportHandler(db *sql.DB, redisClient *redis.Client, testAuth bool) (http.Handler, bool, error) {
	if os.Getenv("GOJET_SUPPORT_ENABLED") != "1" {
		return nil, false, nil
	}
	store, err := support.NewStore(db)
	if err != nil {
		return nil, false, err
	}
	workspaceStore := workspace.NewStore(db)
	domainStore := domains.NewMySQLStore(db)
	auditedRequesterStore, err := support.NewAuditedSupportStore(store, store)
	if err != nil {
		return nil, false, err
	}
	auditedDomainProjector, err := support.NewAuditedDomainAccessProjector(domainStore, store)
	if err != nil {
		return nil, false, err
	}
	auditedAdminTicketStore, err := support.NewAuditedAdminTicketStore(store, store)
	if err != nil {
		return nil, false, err
	}
	auditedAdminMailStore, err := support.NewAuditedAdminMailStore(store, store)
	if err != nil {
		return nil, false, err
	}

	limit := int64(10)
	if raw := strings.TrimSpace(os.Getenv("GOJET_SUPPORT_RATE_LIMIT")); raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || parsed <= 0 || parsed > 1000 {
			return nil, false, support.ErrInvalidInput
		}
		limit = parsed
	}
	window := 5 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("GOJET_SUPPORT_RATE_WINDOW")); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil || parsed < time.Second || parsed > time.Hour {
			return nil, false, support.ErrInvalidInput
		}
		window = parsed
	}
	replayTTL := 10 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("GOJET_SUPPORT_TURNSTILE_REPLAY_TTL")); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil || parsed < time.Minute || parsed > 24*time.Hour {
			return nil, false, support.ErrInvalidInput
		}
		replayTTL = parsed
	}
	guard, err := support.NewRedisSubmissionGuard(redisClient, limit, window, replayTTL)
	if err != nil {
		return nil, false, err
	}

	var verifier support.TurnstileVerifier
	if testAuth && os.Getenv("GOJET_TEST_SUPPORT_TURNSTILE_ENABLED") == "1" {
		verifier, err = support.NewDeterministicTurnstileVerifier(os.Getenv("GOJET_TEST_SUPPORT_TURNSTILE_TOKEN"))
	} else {
		secret := strings.TrimSpace(os.Getenv("GOJET_TURNSTILE_SECRET"))
		if secret == "" {
			return nil, false, support.ErrTurnstileRejected
		}
		verifier, err = support.NewTurnstileHTTPVerifier(secret, nil)
	}
	if err != nil {
		return nil, false, err
	}

	var requesterPrincipalResolver support.PrincipalResolver
	if testAuth {
		requesterPrincipalResolver = supportPrincipalResolver{testAuth: true}
	} else {
		requesterPrincipalResolver, err = buildSupportSessionPrincipalResolver(db, redisClient)
		if err != nil {
			return nil, false, err
		}
	}
	requesterAPI, err := support.NewAPI(
		auditedRequesterStore,
		workspaceStore,
		auditedDomainProjector,
		workspaceStore,
		requesterPrincipalResolver,
		verifier,
		guard,
		guard,
	)
	if err != nil {
		return nil, false, err
	}

	// P17 owns the administrator identity/session/permission lifecycle. P14
	// consumes only the tickets.manage and mail.manage decisions. Test mode keeps
	// the predecessor server-owned actors; production composes the real P17
	// session, Origin/CSRF and PermissionCatalog authority without trusting any
	// client-supplied role or permission header.
	var adminPrincipalResolver support.PrincipalResolver = supportPrincipalResolver{testAuth: testAuth}
	var ticketAdminPermissions support.AdminPermissionResolver
	var mailAdminPermissions support.AdminPermissionResolver
	if testAuth {
		if os.Getenv("GOJET_TEST_SUPPORT_TICKETS_ADMIN_ENABLED") == "1" {
			actorID := strings.TrimSpace(os.Getenv("GOJET_TEST_SUPPORT_TICKETS_ADMIN_ACTOR"))
			if actorID == "" {
				return nil, false, support.ErrAuthenticationUnavailable
			}
			ticketAdminPermissions = supportTestTicketAdminPermissionResolver{actorID: actorID}
		}
		if os.Getenv("GOJET_TEST_SUPPORT_MAIL_ADMIN_ENABLED") == "1" {
			actorID := strings.TrimSpace(os.Getenv("GOJET_TEST_SUPPORT_MAIL_ADMIN_ACTOR"))
			if actorID == "" {
				return nil, false, support.ErrAuthenticationUnavailable
			}
			mailAdminPermissions = supportTestMailAdminPermissionResolver{actorID: actorID}
		}
	} else {
		adminAuthority, enabled, authorityErr := buildSupportAdminAuthority(db, redisClient)
		if authorityErr != nil {
			return nil, false, authorityErr
		}
		if enabled {
			adminPrincipalResolver = adminAuthority
			ticketAdminPermissions = adminAuthority
			mailAdminPermissions = adminAuthority
		}
	}

	adminAPI, err := support.NewAdminAPI(auditedAdminTicketStore, adminPrincipalResolver, ticketAdminPermissions, workspaceStore)
	if err != nil {
		return nil, false, err
	}
	adminMailAPI, err := support.NewAdminMailAPI(auditedAdminMailStore, adminPrincipalResolver, mailAdminPermissions)
	if err != nil {
		return nil, false, err
	}

	requesterHandler := requesterAPI.Handler()
	adminHandler := adminAPI.Handler()
	adminMailHandler := adminMailAPI.Handler()
	combined := http.NewServeMux()
	for _, pattern := range []string{
		"POST /api/public/contact",
		"GET /api/support/tickets",
		"POST /api/support/tickets",
		"GET /api/support/tickets/{ticketId}",
		"POST /api/support/tickets/{ticketId}/replies",
		"POST /api/support/tickets/{ticketId}/close",
	} {
		combined.Handle(pattern, requesterHandler)
	}
	for _, pattern := range []string{
		"GET /api/admin/support/tickets",
		"GET /api/admin/support/tickets/{ticketId}",
		"POST /api/admin/support/tickets/{ticketId}/replies",
		"PATCH /api/admin/support/tickets/{ticketId}",
	} {
		combined.Handle(pattern, adminHandler)
	}
	for _, pattern := range []string{
		"GET /api/admin/mail/queue",
		"GET /api/admin/mail/templates",
		"GET /api/admin/mail/settings",
		"PATCH /api/admin/mail/settings",
		"POST /api/admin/mail/test",
	} {
		combined.Handle(pattern, adminMailHandler)
	}
	return support.WithSupportCorrelation(combined), true, nil
}

func mountSupportRoutes(root *http.ServeMux, handler http.Handler) {
	patterns := []string{
		"POST /api/public/contact",
		"GET /api/support/tickets",
		"POST /api/support/tickets",
		"GET /api/support/tickets/{ticketId}",
		"POST /api/support/tickets/{ticketId}/replies",
		"POST /api/support/tickets/{ticketId}/close",
		"GET /api/admin/support/tickets",
		"GET /api/admin/support/tickets/{ticketId}",
		"POST /api/admin/support/tickets/{ticketId}/replies",
		"PATCH /api/admin/support/tickets/{ticketId}",
		"GET /api/admin/mail/queue",
		"GET /api/admin/mail/templates",
		"GET /api/admin/mail/settings",
		"PATCH /api/admin/mail/settings",
		"POST /api/admin/mail/test",
	}
	for _, pattern := range patterns {
		root.Handle(pattern, handler)
	}
}
