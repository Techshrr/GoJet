package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/redis/go-redis/v9"
)

func buildAdminAccessHandler(db *sql.DB, redisClient *redis.Client) (http.Handler, bool, error) {
	if os.Getenv("GOJET_ADMIN_ACCESS_ENABLED") != "1" {
		return nil, false, nil
	}
	if db == nil || redisClient == nil {
		return nil, false, adminaccess.ErrInvalid
	}

	keyID := strings.TrimSpace(os.Getenv("GOJET_ADMIN_TOTP_KEY_ID"))
	keyHex := strings.TrimSpace(os.Getenv("GOJET_ADMIN_TOTP_KEY_HEX"))
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 32 {
		return nil, false, adminaccess.ErrInvalid
	}
	cipher, err := adminaccess.NewSecretCipher(keyID, key)
	if err != nil {
		return nil, false, err
	}

	rawOrigins := strings.TrimSpace(os.Getenv("GOJET_ADMIN_ALLOWED_ORIGIN"))
	origins := make([]string, 0, 2)
	for _, raw := range strings.Split(rawOrigins, ",") {
		if value := strings.TrimSpace(raw); value != "" {
			origins = append(origins, value)
		}
	}
	if len(origins) == 0 {
		return nil, false, adminaccess.ErrInvalid
	}

	sessionTTL := 8 * time.Hour
	if raw := strings.TrimSpace(os.Getenv("GOJET_ADMIN_SESSION_TTL")); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil {
			return nil, false, adminaccess.ErrInvalid
		}
		sessionTTL = parsed
	}
	loginLimit := int64(10)
	if raw := strings.TrimSpace(os.Getenv("GOJET_ADMIN_LOGIN_RATE_LIMIT")); raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || parsed < 1 || parsed > 1000 {
			return nil, false, adminaccess.ErrInvalid
		}
		loginLimit = parsed
	}
	loginWindow := 5 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("GOJET_ADMIN_LOGIN_RATE_WINDOW")); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil || parsed < time.Minute || parsed > time.Hour {
			return nil, false, adminaccess.ErrInvalid
		}
		loginWindow = parsed
	}

	limiter, err := adminaccess.NewRedisLoginLimiter(redisClient, "admin:login:", loginLimit, loginWindow)
	if err != nil {
		return nil, false, err
	}
	service, err := adminaccess.NewService(db, limiter, cipher, sessionTTL, origins)
	if err != nil {
		return nil, false, err
	}
	if err := adminaccess.VerifyDomainEntitlementSchema(context.Background(), db); err != nil {
		return nil, false, err
	}
	api, err := adminaccess.NewHTTPAPI(service)
	if err != nil {
		return nil, false, err
	}

	combined := http.NewServeMux()
	combined.Handle("/", api.Handler())
	domainEntitlementHandler := api.DomainEntitlementHandler()
	for _, pattern := range []string{
		"GET /api/admin/domain-entitlements",
		"GET /api/admin/domain-entitlements/{workspaceId}",
		"POST /api/admin/domain-entitlements/{workspaceId}/decisions",
	} {
		combined.Handle(pattern, domainEntitlementHandler)
	}
	return combined, true, nil
}

func mountAdminAccessRoutes(root *http.ServeMux, handler http.Handler) {
	for _, pattern := range []string{
		"POST /api/admin/auth/login",
		"POST /api/admin/auth/logout",
		"GET /api/admin/auth/session",
		"GET /api/admin/auth/sessions",
		"POST /api/admin/auth/sessions/{sessionId}/revoke",
		"POST /api/admin/auth/totp/enroll",
		"POST /api/admin/auth/totp/confirm",
		"GET /api/admin/permissions",
		"GET /api/admin/roles",
		"POST /api/admin/roles",
		"GET /api/admin/administrators",
		"POST /api/admin/administrators",
		"GET /api/admin/users",
		"GET /api/admin/users/{userId}",
		"POST /api/admin/users/{userId}/suspend",
		"POST /api/admin/users/{userId}/restore",
		"GET /api/admin/workspaces",
		"GET /api/admin/workspaces/{workspaceId}",
		"POST /api/admin/workspaces/{workspaceId}/suspend",
		"POST /api/admin/workspaces/{workspaceId}/restore",
		"GET /api/admin/audit",
		"GET /api/admin/domain-entitlements",
		"GET /api/admin/domain-entitlements/{workspaceId}",
		"POST /api/admin/domain-entitlements/{workspaceId}/decisions",
	} {
		root.Handle(pattern, handler)
	}
}
