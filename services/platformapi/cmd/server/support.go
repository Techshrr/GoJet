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

	principalResolver := supportPrincipalResolver{testAuth: testAuth}
	requesterAPI, err := support.NewAPI(
		store,
		workspaceStore,
		domainStore,
		workspaceStore,
		principalResolver,
		verifier,
		guard,
		guard,
	)
	if err != nil {
		return nil, false, err
	}

	// P17 owns the administrator/permission lifecycle. P14 only consumes the
	// tickets.manage boundary. Production therefore receives no synthetic
	// permission resolver and Admin Tickets fails closed until P17 is wired.
	// CI may explicitly opt one server-owned actor into tickets.manage; no
	// client-supplied role/permission header is authoritative.
	var adminPermissions support.AdminPermissionResolver
	if testAuth && os.Getenv("GOJET_TEST_SUPPORT_TICKETS_ADMIN_ENABLED") == "1" {
		actorID := strings.TrimSpace(os.Getenv("GOJET_TEST_SUPPORT_TICKETS_ADMIN_ACTOR"))
		if actorID == "" {
			return nil, false, support.ErrAuthenticationUnavailable
		}
		adminPermissions = supportTestTicketAdminPermissionResolver{actorID: actorID}
	}
	adminAPI, err := support.NewAdminAPI(store, principalResolver, adminPermissions, workspaceStore)
	if err != nil {
		return nil, false, err
	}

	requesterHandler := requesterAPI.Handler()
	adminHandler := adminAPI.Handler()
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
	return combined, true, nil
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
	}
	for _, pattern := range patterns {
		root.Handle(pattern, handler)
	}
}
