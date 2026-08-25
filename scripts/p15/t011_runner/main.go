package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/support"
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

type providerFailureVerifier struct{}

func (providerFailureVerifier) Verify(context.Context, string) (support.TurnstileVerification, error) {
	return support.TurnstileVerification{}, errors.New("provider unavailable")
}

type rejectingVerifier struct{}

func (rejectingVerifier) Verify(context.Context, string) (support.TurnstileVerification, error) {
	return support.TurnstileVerification{Success: false}, nil
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
	stamp := time.Now().UTC().UnixNano()
	store := auth.NewStore(db)
	user, err := store.CreateUser(ctx, auth.CreateUserInput{Email: fmt.Sprintf("p15-t011-%d@example.test", stamp), DisplayName: "P15 T011 Initial"})
	if err != nil {
		fail(err)
	}
	prefix := fmt.Sprintf("auth:turnstile:t011:%d", stamp)
	replay, err := auth.NewRedisDigestReplayStore(client, prefix, 10*time.Minute)
	if err != nil {
		fail(err)
	}
	validToken := fmt.Sprintf("p15-t011-valid-%d", stamp)
	verifier, err := support.NewDeterministicTurnstileVerifier(validToken)
	if err != nil {
		fail(err)
	}
	guard, err := auth.NewAuthTurnstileGuard(verifier, replay)
	if err != nil {
		fail(err)
	}
	validErr := guardThenMutate(ctx, db, user.ID, guard, validToken, "P15 T011 Allowed")
	replayErr := guardThenMutate(ctx, db, user.ID, guard, validToken, "P15 T011 Replay")
	invalidErr := guardThenMutate(ctx, db, user.ID, guard, "p15-t011-invalid", "P15 T011 Invalid")
	missingErr := guardThenMutate(ctx, db, user.ID, guard, "", "P15 T011 Missing")
	expiredGuard, err := auth.NewAuthTurnstileGuard(rejectingVerifier{}, replay)
	if err != nil {
		fail(err)
	}
	expiredErr := guardThenMutate(ctx, db, user.ID, expiredGuard, "p15-t011-expired", "P15 T011 Expired")
	failureGuard, err := auth.NewAuthTurnstileGuard(providerFailureVerifier{}, replay)
	if err != nil {
		fail(err)
	}
	providerErr := guardThenMutate(ctx, db, user.ID, failureGuard, "p15-t011-provider-failure", "P15 T011 Provider Failure")

	var displayName string
	if err := db.QueryRowContext(ctx, `SELECT display_name FROM auth_users WHERE id=?`, user.ID).Scan(&displayName); err != nil {
		fail(err)
	}
	keys, err := client.Keys(ctx, prefix+":*").Result()
	if err != nil {
		fail(err)
	}
	joinedKeys := strings.Join(keys, "\n")
	checks := map[string]bool{
		"valid_turnstile_allows_one_protected_mutation": validErr == nil && displayName == "P15 T011 Allowed",
		"replayed_turnstile_fails_closed":               errors.Is(replayErr, support.ErrTurnstileReplay),
		"invalid_turnstile_fails_closed":                errors.Is(invalidErr, support.ErrTurnstileRejected),
		"missing_turnstile_fails_closed":                missingErr != nil,
		"expired_turnstile_fails_closed":                errors.Is(expiredErr, support.ErrTurnstileRejected),
		"provider_failure_fails_closed":                 errors.Is(providerErr, support.ErrTurnstileRejected),
		"rejected_turnstile_never_mutates_state":        displayName == "P15 T011 Allowed",
		"redis_replay_authority_uses_digest_keys_only":  len(keys) == 1 && !strings.Contains(joinedKeys, validToken),
	}
	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := result{Case: "P15-T011", Status: status, MySQLVersion: mysqlVersion, RecordCounts: map[string]int{"turnstile_replay_keys": len(keys), "authorized_mutations": boolCount(displayName == "P15 T011 Allowed")}, Checks: checks}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}

func guardThenMutate(ctx context.Context, db *sql.DB, userID string, guard *auth.AuthTurnstileGuard, token, displayName string) error {
	if err := guard.Verify(ctx, token); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `UPDATE auth_users SET display_name=?,updated_at=? WHERE id=?`, displayName, time.Now().UTC().Truncate(time.Microsecond), userID)
	return err
}

func setup() (*sql.DB, *redis.Client, context.Context, context.CancelFunc) {
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	if dsn == "" || redisAddr == "" {
		fail(fmt.Errorf("P15-T011 integration configuration missing"))
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

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
