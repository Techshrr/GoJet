package handoffbatch

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
)

func runT023(ctx context.Context, db *sql.DB) (map[string]bool, map[string]int, error) {
	header := make(http.Header)
	auth.ApplyPrivateAuthHeaders(header)
	cookie, err := auth.NewSessionCookie("p15-t023-session-token", time.Now().UTC().Add(time.Hour))
	if err != nil {
		return nil, nil, err
	}
	csp := header.Get("Content-Security-Policy")
	checks := map[string]bool{
		"auth_responses_are_no_store_private":                header.Get("Cache-Control") == "no-store, private" && header.Get("Pragma") == "no-cache",
		"auth_responses_are_noindex_nofollow":                header.Get("X-Robots-Tag") == "noindex, nofollow",
		"csp_is_frozen_and_turnstile_scoped":                 csp == auth.AuthContentSecurityPolicy && strings.Contains(csp, "https://challenges.cloudflare.com") && strings.Contains(csp, "frame-ancestors 'none'"),
		"browser_hardening_headers_are_present":              header.Get("Referrer-Policy") == "no-referrer" && header.Get("X-Content-Type-Options") == "nosniff" && header.Get("X-Frame-Options") == "DENY" && header.Get("Permissions-Policy") != "",
		"formal_session_cookie_is_host_secure_http_only_lax": cookie.Name == auth.SessionCookieName && cookie.Path == "/" && cookie.Secure && cookie.HttpOnly && cookie.SameSite == http.SameSiteLaxMode,
	}
	return checks, map[string]int{"security_headers": len(header)}, nil
}
