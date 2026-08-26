package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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

type surfaceEvidence struct {
	firstAllowed  bool
	secondAllowed bool
	thirdDenied   bool
	ipSprayDenied bool
	retryBounded  bool
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(2) }

func main() {
	db, client, ctx, cancel := setup()
	defer cancel()
	defer db.Close()
	defer client.Close()
	var mysqlVersion string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&mysqlVersion); err != nil {
		fail(err)
	}
	window := 30 * time.Second
	limiter, err := auth.NewRedisAuthRateLimiter(client, 2, window)
	if err != nil {
		fail(err)
	}
	stamp := time.Now().UTC().UnixNano()
	surfaces := []auth.AuthRateSurface{auth.AuthRateRegister, auth.AuthRateLogin, auth.AuthRateEmailCode, auth.AuthRateRecovery}
	evidence := map[auth.AuthRateSurface]surfaceEvidence{}
	for i, surface := range surfaces {
		identity := fmt.Sprintf("p15-t012-%s-%d@example.test", surface, stamp)
		remote := fmt.Sprintf("203.0.113.%d:443", 30+i)
		first, err := limiter.Allow(ctx, surface, identity, remote)
		if err != nil {
			fail(err)
		}
		second, err := limiter.Allow(ctx, surface, identity, remote)
		if err != nil {
			fail(err)
		}
		third, err := limiter.Allow(ctx, surface, identity, remote)
		if err != nil {
			fail(err)
		}
		spray, err := limiter.Allow(ctx, surface, fmt.Sprintf("different-%d@example.test", i), remote)
		if err != nil {
			fail(err)
		}
		evidence[surface] = surfaceEvidence{
			firstAllowed:  first.Allowed,
			secondAllowed: second.Allowed,
			thirdDenied:   !third.Allowed && third.IdentityCount == 3 && third.IPCount == 3,
			ipSprayDenied: !spray.Allowed && spray.IdentityCount == 1 && spray.IPCount >= 4,
			retryBounded:  third.RetryAfter > 0 && third.RetryAfter <= window && spray.RetryAfter > 0 && spray.RetryAfter <= window,
		}
	}

	store := auth.NewStore(db)
	existingEmail := fmt.Sprintf("p15-t012-existing-%d@example.test", stamp)
	if _, err := store.CreateUser(ctx, auth.CreateUserInput{Email: existingEmail, DisplayName: "P15 T012"}); err != nil {
		fail(err)
	}
	neutralLimiter, err := auth.NewRedisAuthRateLimiter(client, 5, window)
	if err != nil {
		fail(err)
	}
	existingDecision, err := neutralLimiter.Allow(ctx, auth.AuthRateRecovery, existingEmail, "198.51.100.41:443")
	if err != nil {
		fail(err)
	}
	nonexistentEmail := fmt.Sprintf("p15-t012-missing-%d@example.test", stamp)
	missingDecision, err := neutralLimiter.Allow(ctx, auth.AuthRateRecovery, nonexistentEmail, "198.51.100.42:443")
	if err != nil {
		fail(err)
	}

	keys, err := client.Keys(ctx, "auth:rate:*").Result()
	if err != nil {
		fail(err)
	}
	joined := strings.ToLower(strings.Join(keys, "\n"))
	surfaceOK := true
	for _, item := range evidence {
		if !(item.firstAllowed && item.secondAllowed && item.thirdDenied && item.ipSprayDenied && item.retryBounded) {
			surfaceOK = false
			break
		}
	}
	checks := map[string]bool{
		"all_auth_abuse_surfaces_are_server_limited":    surfaceOK,
		"identity_bucket_blocks_repeated_abuse":         allThirdDenied(evidence),
		"ip_bucket_blocks_account_spray":                allSprayDenied(evidence),
		"retry_after_is_positive_and_bounded":           allRetryBounded(evidence),
		"redis_keys_exclude_raw_identity_and_ip":        !strings.Contains(joined, "example.test") && !strings.Contains(joined, "203.0.113") && !strings.Contains(joined, "198.51.100"),
		"recovery_limit_is_account_enumeration_neutral": existingDecision.Allowed == missingDecision.Allowed && existingDecision.Count == missingDecision.Count,
	}
	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := result{Case: "P15-T012", Status: status, MySQLVersion: mysqlVersion, RecordCounts: map[string]int{"rate_limit_keys": len(keys), "surfaces_exercised": len(surfaces)}, Checks: checks}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}

func allThirdDenied(evidence map[auth.AuthRateSurface]surfaceEvidence) bool {
	for _, item := range evidence {
		if !item.thirdDenied {
			return false
		}
	}
	return true
}

func allSprayDenied(evidence map[auth.AuthRateSurface]surfaceEvidence) bool {
	for _, item := range evidence {
		if !item.ipSprayDenied {
			return false
		}
	}
	return true
}

func allRetryBounded(evidence map[auth.AuthRateSurface]surfaceEvidence) bool {
	for _, item := range evidence {
		if !item.retryBounded {
			return false
		}
	}
	return true
}

func setup() (*sql.DB, *redis.Client, context.Context, context.CancelFunc) {
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	if dsn == "" || redisAddr == "" {
		fail(fmt.Errorf("P15-T012 integration configuration missing"))
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
	return db, client, ctx, cancel
}
