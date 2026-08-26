package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

type result struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(2) }

func main() {
	db, client, ctx, cancel, csrfKey := setup()
	defer cancel()
	defer db.Close()
	defer client.Close()
	var mysqlVersion string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&mysqlVersion); err != nil {
		fail(err)
	}
	store := auth.NewStore(db)
	stamp := time.Now().UTC().UnixNano()
	user, err := store.CreateUser(ctx, auth.CreateUserInput{Email: fmt.Sprintf("p15-t010-%d@example.test", stamp), DisplayName: "P15 T010 Initial"})
	if err != nil {
		fail(err)
	}
	sessionSecret, err := store.CreateSession(ctx, user.ID, time.Hour, fmt.Sprintf("p15-t010-%d", stamp))
	if err != nil {
		fail(err)
	}
	replay, err := auth.NewRedisDigestReplayStore(client, fmt.Sprintf("auth:csrf:t010:%d", stamp), 10*time.Minute)
	if err != nil {
		fail(err)
	}
	csrf, err := auth.NewCSRFManager(csrfKey, 5*time.Minute, replay)
	if err != nil {
		fail(err)
	}
	origins, err := auth.NewOriginPolicy("https://app.gojet.test", "https://admin.gojet.test")
	if err != nil {
		fail(err)
	}
	now := time.Now().UTC()

	missingErr := attempt(ctx, db, user.ID, sessionSecret.Session, origins, csrf, now, "", "P15 T010 Missing")
	disallowedErr := attempt(ctx, db, user.ID, sessionSecret.Session, origins, csrf, now, "https://evil.example", "P15 T010 Evil")
	malformedErr := attempt(ctx, db, user.ID, sessionSecret.Session, origins, csrf, now, "https://app.gojet.test/path?next=evil", "P15 T010 Malformed")
	allowedErr := attempt(ctx, db, user.ID, sessionSecret.Session, origins, csrf, now, "https://app.gojet.test", "P15 T010 Allowed")
	adminAllowedErr := attempt(ctx, db, user.ID, sessionSecret.Session, origins, csrf, now, "HTTPS://ADMIN.GOJET.TEST", "P15 T010 Admin Allowed")

	var displayName string
	if err := db.QueryRowContext(ctx, `SELECT display_name FROM auth_users WHERE id=?`, user.ID).Scan(&displayName); err != nil {
		fail(err)
	}
	checks := map[string]bool{
		"missing_origin_fails_closed":                 errors.Is(missingErr, auth.ErrForbidden),
		"cross_site_origin_fails_closed":              errors.Is(disallowedErr, auth.ErrForbidden),
		"malformed_origin_fails_closed":               errors.Is(malformedErr, auth.ErrForbidden),
		"allowed_same_site_origin_succeeds":           allowedErr == nil,
		"allowed_origin_normalization_is_case_stable": adminAllowedErr == nil,
		"rejected_origins_do_not_mutate_state":        displayName == "P15 T010 Admin Allowed",
	}
	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := result{Case: "P15-T010", Status: status, MySQLVersion: mysqlVersion, RecordCounts: map[string]int{"authorized_origin_mutations": count(ctx, db, `SELECT COUNT(*) FROM auth_users WHERE id=? AND display_name='P15 T010 Admin Allowed'`, user.ID)}, Checks: checks}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}

func attempt(ctx context.Context, db *sql.DB, userID string, session auth.Session, origins *auth.OriginPolicy, csrf *auth.CSRFManager, now time.Time, origin, displayName string) error {
	token, err := csrf.Issue(session.ID, now)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPatch, "https://app.gojet.test/api/me/profile", nil)
	if err != nil {
		return err
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	request.Header.Set(auth.CSRFHeaderName, token)
	if err := auth.AuthorizeUnsafeRequest(ctx, request, session, origins, csrf, now); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE auth_users SET display_name=?,updated_at=? WHERE id=?`, displayName, time.Now().UTC().Truncate(time.Microsecond), userID)
	return err
}

func setup() (*sql.DB, *redis.Client, context.Context, context.CancelFunc, []byte) {
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	csrfHex := strings.TrimSpace(os.Getenv("GOJET_AUTH_CSRF_KEY_HEX"))
	if dsn == "" || redisAddr == "" || csrfHex == "" {
		fail(fmt.Errorf("P15-T010 integration configuration missing"))
	}
	csrfKey, err := hex.DecodeString(csrfHex)
	if err != nil || len(csrfKey) != 32 {
		fail(fmt.Errorf("GOJET_AUTH_CSRF_KEY_HEX must decode to 32 bytes"))
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fail(err)
	}
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	if err := db.PingContext(ctx); err != nil {
		cancel()
		fail(err)
	}
	if err := client.Ping(ctx).Err(); err != nil {
		cancel()
		fail(err)
	}
	return db, client, ctx, cancel, csrfKey
}

func count(ctx context.Context, db *sql.DB, query string, args ...any) int {
	var n int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		fail(err)
	}
	return n
}
