package main

import (
	"database/sql"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/support"
	"github.com/Techshrr/GoJet/internal/trust"
	"github.com/redis/go-redis/v9"
)

func buildTrustHandler(db *sql.DB, redisClient *redis.Client, testAuth bool) (http.Handler, bool, error) {
	if os.Getenv("GOJET_TRUST_ENABLED") != "1" {
		return nil, false, nil
	}
	limit := int64(10)
	if raw := strings.TrimSpace(os.Getenv("GOJET_TRUST_ABUSE_RATE_LIMIT")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 || parsed > 1000 {
			return nil, false, trust.ErrInvalid
		}
		limit = parsed
	}
	window := 5 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("GOJET_TRUST_ABUSE_RATE_WINDOW")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < time.Second || parsed > time.Hour {
			return nil, false, trust.ErrInvalid
		}
		window = parsed
	}
	replayTTL := 10 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("GOJET_TRUST_TURNSTILE_REPLAY_TTL")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < time.Minute || parsed > 24*time.Hour {
			return nil, false, trust.ErrInvalid
		}
		replayTTL = parsed
	}
	guard, err := trust.NewAbuseSubmissionGuard(redisClient, limit, window, replayTTL)
	if err != nil {
		return nil, false, err
	}

	var verifier support.TurnstileVerifier
	if testAuth && os.Getenv("GOJET_TEST_TRUST_TURNSTILE_ENABLED") == "1" {
		verifier, err = support.NewDeterministicTurnstileVerifier(os.Getenv("GOJET_TEST_TRUST_TURNSTILE_TOKEN"))
	} else {
		secret := strings.TrimSpace(os.Getenv("GOJET_TURNSTILE_SECRET"))
		if secret == "" {
			return nil, false, trust.ErrVerification
		}
		verifier, err = support.NewTurnstileHTTPVerifier(secret, nil)
	}
	if err != nil {
		return nil, false, err
	}
	service, err := trust.NewAbuseService(trust.NewStore(db), verifier, guard)
	if err != nil {
		return nil, false, err
	}
	api, err := trust.NewPublicAbuseAPI(service)
	if err != nil {
		return nil, false, err
	}
	return api.Handler(), true, nil
}

func mountTrustRoutes(root *http.ServeMux, handler http.Handler) {
	root.Handle("POST /api/public/abuse-reports", handler)
}
