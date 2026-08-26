package runnerutil

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/securetoken"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

const AllowedOrigin = "https://app.gojet.test"

func OpenMySQL(ctx context.Context) (*sql.DB, string, error) {
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	if dsn == "" {
		return nil, "", fmt.Errorf("GOJET_MYSQL_DSN is required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, "", err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, "", err
	}
	var version string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		db.Close()
		return nil, "", err
	}
	return db, version, nil
}

func OpenRedis(ctx context.Context) (*redis.Client, error) {
	addr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	if addr == "" {
		return nil, fmt.Errorf("GOJET_REDIS_ADDR is required")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

func GrantKey() (securetoken.Key, error) {
	keyID := strings.TrimSpace(os.Getenv("GOJET_AUTH_GRANT_KEY_ID"))
	keyHex := strings.TrimSpace(os.Getenv("GOJET_AUTH_GRANT_KEY_HEX"))
	if keyID == "" || keyHex == "" {
		return securetoken.Key{}, fmt.Errorf("grant-key runtime configuration is required")
	}
	raw, err := hex.DecodeString(keyHex)
	if err != nil {
		return securetoken.Key{}, err
	}
	return securetoken.NewKey(keyID, raw)
}

func OAuthCrypto() (*auth.OAuthCrypto, error) {
	keyID := strings.TrimSpace(os.Getenv("GOJET_OAUTH_KEY_ID"))
	keyHex := strings.TrimSpace(os.Getenv("GOJET_OAUTH_KEY_HEX"))
	if keyID == "" || keyHex == "" {
		return nil, fmt.Errorf("OAuth key runtime configuration is required")
	}
	raw, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, err
	}
	return auth.NewOAuthCrypto(keyID, raw)
}

func ActivateUser(ctx context.Context, db *sql.DB, email, displayName string, now time.Time) (auth.User, error) {
	store := auth.NewStore(db)
	user, err := store.CreateUser(ctx, auth.CreateUserInput{Email: email, DisplayName: displayName})
	if err != nil {
		return auth.User{}, err
	}
	when := now.UTC().Truncate(time.Microsecond)
	res, err := db.ExecContext(ctx, `
UPDATE auth_users SET status='active',email_verified_at=?,version=version+1,updated_at=?
WHERE id=? AND status='pending_verification'`, when, when, user.ID)
	if err != nil {
		return auth.User{}, err
	}
	rows, err := res.RowsAffected()
	if err != nil || rows != 1 {
		return auth.User{}, fmt.Errorf("activate user rows=%d err=%v", rows, err)
	}
	user.Status = auth.UserStatusActive
	user.EmailVerifiedAt = &when
	user.Version++
	user.UpdatedAt = when
	return user, nil
}

func CreateSession(ctx context.Context, db *sql.DB, userID, correlationID string, ttl time.Duration) (auth.SessionSecret, error) {
	return auth.NewStore(db).CreateSession(ctx, userID, ttl, correlationID)
}

func MutationAuthority(ctx context.Context, redisClient *redis.Client, session auth.Session, method, origin string, now time.Time) (*auth.UnsafeMutationAuthority, error) {
	return AuthorizeMutationRequest(ctx, redisClient, session, method, origin, true, now)
}

func AuthorizeMutationRequest(ctx context.Context, redisClient *redis.Client, session auth.Session, method, origin string, includeCSRF bool, now time.Time) (*auth.UnsafeMutationAuthority, error) {
	keyHex := strings.TrimSpace(os.Getenv("GOJET_AUTH_CSRF_KEY_HEX"))
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("valid GOJET_AUTH_CSRF_KEY_HEX is required")
	}
	replay, err := auth.NewRedisDigestReplayStore(redisClient, "p15:account:csrf", 20*time.Minute)
	if err != nil {
		return nil, err
	}
	csrf, err := auth.NewCSRFManager(key, 10*time.Minute, replay)
	if err != nil {
		return nil, err
	}
	token, err := csrf.Issue(session.ID, now)
	if err != nil {
		return nil, err
	}
	origins, err := auth.NewOriginPolicy(AllowedOrigin)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequest(method, AllowedOrigin+"/api/me/security", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Origin", origin)
	if includeCSRF {
		request.Header.Set(auth.CSRFHeaderName, token)
	}
	return auth.AuthorizeUnsafeMutation(ctx, request, session, origins, csrf, now)
}

func Count(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var count int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

type PermissionRecorder struct {
	Allowed bool
	Seen    string
}

func (p *PermissionRecorder) Authorize(_ context.Context, _ string, permission string) error {
	p.Seen = permission
	if !p.Allowed {
		return auth.ErrForbidden
	}
	return nil
}

type DeterministicOAuthAdapter struct {
	ExpectedProvider     string
	ExpectedCode         string
	ExpectedClientID     string
	ExpectedClientSecret string
	ExpectedRedirectURI  string
	ExpectedPKCEVerifier string
	Claim                auth.OAuthProviderClaim
	Calls                int
}

func (a *DeterministicOAuthAdapter) Exchange(_ context.Context, request auth.OAuthProviderExchangeRequest) (auth.OAuthProviderClaim, error) {
	if a == nil {
		return auth.OAuthProviderClaim{}, auth.ErrForbidden
	}
	a.Calls++
	if request.Provider != a.ExpectedProvider || request.Code != a.ExpectedCode || request.ClientID != a.ExpectedClientID || request.ClientSecret != a.ExpectedClientSecret || request.RedirectURI != a.ExpectedRedirectURI || request.PKCEVerifier != a.ExpectedPKCEVerifier {
		return auth.OAuthProviderClaim{}, auth.ErrForbidden
	}
	return a.Claim, nil
}
