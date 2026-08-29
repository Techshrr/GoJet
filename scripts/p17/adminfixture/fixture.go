package adminfixture

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

const (
	AllowedOrigin = "https://admin.p17.test"
	FixtureKeyID  = "p17-fixture-key-v1"
)

var fixtureKey = bytes.Repeat([]byte{0x6d}, 32)

type Runtime struct {
	DB    *sql.DB
	Redis *redis.Client
}

func Open() (*Runtime, error) {
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	if dsn == "" || redisAddr == "" {
		return nil, fmt.Errorf("GOJET_MYSQL_DSN and GOJET_REDIS_ADDR are required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	redisDB := 0
	if raw := strings.TrimSpace(os.Getenv("GOJET_REDIS_DB")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 0 {
			db.Close()
			return nil, fmt.Errorf("invalid GOJET_REDIS_DB")
		}
		redisDB = parsed
	}
	client := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: os.Getenv("GOJET_REDIS_PASSWORD"),
		DB:       redisDB,
	})
	return &Runtime{DB: db, Redis: client}, nil
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}
	if r.Redis != nil {
		_ = r.Redis.Close()
	}
	if r.DB != nil {
		_ = r.DB.Close()
	}
}

func Reset(ctx context.Context, r *Runtime) error {
	if r == nil || r.DB == nil || r.Redis == nil {
		return fmt.Errorf("runtime unavailable")
	}
	// P17 audit authority is append-only at the database layer. Integration
	// cases therefore run against a freshly migrated database instead of
	// deleting durable authority between cases. Refuse a dirty fixture so a
	// test can never gain a hidden cleanup bypass around immutable audit.
	for _, table := range []string{"admin_administrators", "admin_sessions", "admin_audit_events", "admin_idempotency_records"} {
		n, err := ScalarInt(ctx, r.DB, "SELECT COUNT(*) FROM "+table)
		if err != nil {
			return fmt.Errorf("verify pristine %s: %w", table, err)
		}
		if n != 0 {
			return fmt.Errorf("P17 integration requires pristine migrated database: %s has %d rows", table, n)
		}
	}
	var cursor uint64
	for {
		keys, next, err := r.Redis.Scan(ctx, cursor, "p17:case:*", 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := r.Redis.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return nil
}

func NewService(r *Runtime, prefix string, limit int64) (*adminaccess.Service, error) {
	cipher, err := adminaccess.NewSecretCipher(FixtureKeyID, fixtureKey)
	if err != nil {
		return nil, err
	}
	limiter, err := adminaccess.NewRedisLoginLimiter(r.Redis, "p17:case:"+prefix+":", limit, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	return adminaccess.NewService(r.DB, limiter, cipher, 2*time.Hour, []string{AllowedOrigin})
}

func Bootstrap(ctx context.Context, service *adminaccess.Service, email, password string, permissions []string, now time.Time) (adminaccess.Administrator, error) {
	return service.BootstrapAdministrator(ctx, email, "P17 Root", password, permissions, "p17-bootstrap-"+strings.ReplaceAll(email, "@", "-"), now)
}

func LoginAndConfirmMFA(ctx context.Context, service *adminaccess.Service, email, password string, now time.Time) (adminaccess.Principal, adminaccess.SessionSecret, string, error) {
	login, err := service.Login(ctx, email, password, "", "p17-initial-login", now)
	if err != nil {
		return adminaccess.Principal{}, adminaccess.SessionSecret{}, "", err
	}
	principal, err := service.Authenticate(ctx, login.Token, now.Add(time.Second))
	if err != nil {
		return adminaccess.Principal{}, adminaccess.SessionSecret{}, "", err
	}
	secret, err := service.EnrollTOTP(ctx, principal, "p17-mfa-enroll", now.Add(2*time.Second))
	if err != nil {
		return adminaccess.Principal{}, adminaccess.SessionSecret{}, "", err
	}
	code, err := TOTPCode(secret, now.Add(3*time.Second))
	if err != nil {
		return adminaccess.Principal{}, adminaccess.SessionSecret{}, "", err
	}
	if err := service.ConfirmTOTP(ctx, principal, code, "p17-mfa-confirm", now.Add(3*time.Second)); err != nil {
		return adminaccess.Principal{}, adminaccess.SessionSecret{}, "", err
	}
	principal, err = service.Authenticate(ctx, login.Token, now.Add(4*time.Second))
	if err != nil {
		return adminaccess.Principal{}, adminaccess.SessionSecret{}, "", err
	}
	return principal, login, secret, nil
}

func TOTPCode(secret string, now time.Time) (string, error) {
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	counter := uint64(now.UTC().Unix() / 30)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, raw)
	_, _ = mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	binaryCode := (uint32(sum[offset])&0x7f)<<24 | (uint32(sum[offset+1])&0xff)<<16 | (uint32(sum[offset+2])&0xff)<<8 | (uint32(sum[offset+3]) & 0xff)
	return fmt.Sprintf("%06d", binaryCode%1000000), nil
}

func ScalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var n int
	return n, db.QueryRowContext(ctx, query, args...).Scan(&n)
}

func MySQLVersion(ctx context.Context, db *sql.DB) (string, error) {
	var version string
	return version, db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version)
}

func RedisVersion(ctx context.Context, client *redis.Client) (string, error) {
	info, err := client.Info(ctx, "server").Result()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "redis_version:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "redis_version:")), nil
		}
	}
	return "", fmt.Errorf("redis_version absent")
}

func AllTrue(checks map[string]bool) bool {
	if len(checks) == 0 {
		return false
	}
	for _, value := range checks {
		if !value {
			return false
		}
	}
	return true
}

type HTTPResult struct {
	Status  int
	Headers http.Header
	Raw     string
	Body    map[string]any
	Cookie  string
}

func NewHTTPServer(service *adminaccess.Service) (*httptest.Server, error) {
	api, err := adminaccess.NewHTTPAPI(service)
	if err != nil {
		return nil, err
	}
	return httptest.NewServer(api.Handler()), nil
}

func Request(ctx context.Context, server *httptest.Server, method, path, origin, cookie, csrf, idempotency, correlation string, body any) (HTTPResult, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return HTTPResult{}, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, server.URL+path, reader)
	if err != nil {
		return HTTPResult{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if cookie != "" {
		req.Header.Set("Cookie", "gojet_admin_session"+"="+cookie)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if idempotency != "" {
		req.Header.Set("Idempotency-Key", idempotency)
	}
	if correlation != "" {
		req.Header.Set("X-Correlation-ID", correlation)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return HTTPResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return HTTPResult{}, err
	}
	result := HTTPResult{Status: resp.StatusCode, Headers: resp.Header.Clone(), Raw: string(raw), Body: map[string]any{}}
	if len(bytes.TrimSpace(raw)) > 0 && strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(raw, &result.Body)
	}
	for _, setCookie := range resp.Cookies() {
		if setCookie.Name == "gojet_admin_session" {
			result.Cookie = setCookie.Value
		}
	}
	return result, nil
}

func CSRF(result HTTPResult) string {
	value, _ := result.Body["csrf_token"].(string)
	return value
}

func Secret(result HTTPResult) string {
	value, _ := result.Body["secret"].(string)
	return value
}

func NoStoreNoIndex(result HTTPResult) bool {
	cache := strings.ToLower(result.Headers.Get("Cache-Control"))
	robots := strings.ToLower(result.Headers.Get("X-Robots-Tag"))
	return strings.Contains(cache, "no-store") && strings.Contains(robots, "noindex")
}
