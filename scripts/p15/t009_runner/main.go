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
	user, err := store.CreateUser(ctx, auth.CreateUserInput{Email: fmt.Sprintf("p15-t009-%d@example.test", stamp), DisplayName: "P15 T009 Initial"})
	if err != nil {
		fail(err)
	}
	sessionSecret, err := store.CreateSession(ctx, user.ID, time.Hour, fmt.Sprintf("p15-t009-%d", stamp))
	if err != nil {
		fail(err)
	}
	replay, err := auth.NewRedisDigestReplayStore(client, fmt.Sprintf("auth:csrf:t009:%d", stamp), 10*time.Minute)
	if err != nil {
		fail(err)
	}
	csrf, err := auth.NewCSRFManager(csrfKey, time.Minute, replay)
	if err != nil {
		fail(err)
	}
	origins, err := auth.NewOriginPolicy("https://app.gojet.test")
	if err != nil {
		fail(err)
	}
	now := time.Now().UTC()

	missingErr := authorizeThenMutate(ctx, db, user.ID, sessionSecret.Session, origins, csrf, now, "", "P15 T009 Missing")
	invalidErr := authorizeThenMutate(ctx, db, user.ID, sessionSecret.Session, origins, csrf, now, "gcf_invalid", "P15 T009 Invalid")
	expiredToken, err := csrf.Issue(sessionSecret.Session.ID, now.Add(-2*time.Minute))
	if err != nil {
		fail(err)
	}
	expiredErr := authorizeThenMutate(ctx, db, user.ID, sessionSecret.Session, origins, csrf, now, expiredToken, "P15 T009 Expired")
	validToken, err := csrf.Issue(sessionSecret.Session.ID, now)
	if err != nil {
		fail(err)
	}
	validErr := authorizeThenMutate(ctx, db, user.ID, sessionSecret.Session, origins, csrf, now, validToken, "P15 T009 Allowed")
	replayErr := authorizeThenMutate(ctx, db, user.ID, sessionSecret.Session, origins, csrf, now, validToken, "P15 T009 Replay")

	var displayName string
	if err := db.QueryRowContext(ctx, `SELECT display_name FROM auth_users WHERE id=?`, user.ID).Scan(&displayName); err != nil {
		fail(err)
	}
	mutations := count(ctx, db, `SELECT COUNT(*) FROM auth_users WHERE id=? AND display_name='P15 T009 Allowed'`, user.ID)
	checks := map[string]bool{
		"missing_csrf_fails_closed":             errors.Is(missingErr, auth.ErrForbidden),
		"invalid_csrf_fails_closed":             errors.Is(invalidErr, auth.ErrForbidden),
		"expired_csrf_fails_closed":             errors.Is(expiredErr, auth.ErrExpired),
		"valid_csrf_allows_authorized_mutation": validErr == nil && mutations == 1,
		"replayed_csrf_fails_closed":            errors.Is(replayErr, auth.ErrReplay),
		"rejected_requests_do_not_mutate_state": displayName == "P15 T009 Allowed",
	}
	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := result{Case: "P15-T009", Status: status, MySQLVersion: mysqlVersion, RecordCounts: map[string]int{"authorized_mutations": mutations}, Checks: checks}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}

func authorizeThenMutate(ctx context.Context, db *sql.DB, userID string, session auth.Session, origins *auth.OriginPolicy, csrf *auth.CSRFManager, now time.Time, token, displayName string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPatch, "https://app.gojet.test/api/me/profile", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Origin", "https://app.gojet.test")
	if token != "" {
		request.Header.Set(auth.CSRFHeaderName, token)
	}
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
		fail(fmt.Errorf("P15-T009 integration configuration missing"))
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
