package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	textshares "github.com/Techshrr/GoJet/internal/text"
	"github.com/redis/go-redis/v9"
)

func buildTextHandler(db *sql.DB, redisClient *redis.Client, testAuth bool) (http.Handler, bool, error) {
	if os.Getenv("GOJET_TEXT_ENABLED") != "1" {
		return nil, false, nil
	}
	quota, err := requiredTextUint64Env("GOJET_TEXT_WORKSPACE_QUOTA")
	if err != nil {
		return nil, false, err
	}
	publicAuthSecret := strings.TrimSpace(os.Getenv("GOJET_TEXT_PUBLIC_AUTH_SECRET"))
	if len(publicAuthSecret) < 32 {
		return nil, false, fmt.Errorf("GOJET_TEXT_PUBLIC_AUTH_SECRET must be at least 32 bytes when Text is enabled")
	}
	store := textshares.NewStore(db, quota)
	var api *textshares.API
	if testAuth {
		api, err = textshares.NewAPI(store, true, []byte(publicAuthSecret))
	} else {
		authority, authorityErr := buildTextSessionAuthority(db, redisClient)
		if authorityErr != nil {
			return nil, false, fmt.Errorf("configure Text authentication authority: %w", authorityErr)
		}
		api, err = textshares.NewAPIWithActorResolver(store, authority.resolve, []byte(publicAuthSecret))
	}
	if err != nil {
		return nil, false, fmt.Errorf("configure Text API: %w", err)
	}
	return api.Handler(), true, nil
}

func requiredTextUint64Env(name string) (uint64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	value, err := strconv.ParseUint(raw, 10, 64)
	if raw == "" || err != nil || value == 0 || value > 1000000 {
		return 0, fmt.Errorf("%s must be a positive integer no greater than 1000000", name)
	}
	return value, nil
}
