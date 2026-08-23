package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	bio "github.com/Techshrr/GoJet/internal/bio"
	"github.com/redis/go-redis/v9"
)

func buildBioHandler(db *sql.DB, redisClient *redis.Client, testAuth bool) (http.Handler, bool, error) {
	if os.Getenv("GOJET_BIO_ENABLED") != "1" {
		return nil, false, nil
	}
	quota, err := requiredBioUint64Env("GOJET_BIO_WORKSPACE_QUOTA")
	if err != nil {
		return nil, false, err
	}
	if redisClient == nil {
		return nil, false, fmt.Errorf("Redis client is required when Bio is enabled")
	}
	store := bio.NewStore(db, quota)
	risk := bio.NewRedisRiskAuthority(redisClient)
	api, err := bio.NewAPI(store, risk, testAuth)
	if err != nil {
		return nil, false, fmt.Errorf("configure Bio API: %w", err)
	}
	return api.Handler(), true, nil
}

func requiredBioUint64Env(name string) (uint64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	value, err := strconv.ParseUint(raw, 10, 64)
	if raw == "" || err != nil || value == 0 || value > 1000000 {
		return 0, fmt.Errorf("%s must be a positive integer no greater than 1000000", name)
	}
	return value, nil
}
