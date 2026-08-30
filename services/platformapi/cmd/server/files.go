package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/files"
	"github.com/Techshrr/GoJet/internal/links"
	"github.com/redis/go-redis/v9"
)

func buildFilesHandler(db *sql.DB, testAuth bool) (http.Handler, bool, error) {
	var redisClient *redis.Client
	if !testAuth && os.Getenv("GOJET_FILES_ENABLED") == "1" {
		redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
		if redisAddr == "" {
			return nil, false, fmt.Errorf("GOJET_REDIS_ADDR is required when production Files are enabled")
		}
		redisDB := 0
		if raw := strings.TrimSpace(os.Getenv("GOJET_REDIS_DB")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 {
				return nil, false, fmt.Errorf("GOJET_REDIS_DB must be a non-negative integer")
			}
			redisDB = parsed
		}
		redisClient = links.NewRedisClient(redisAddr, os.Getenv("GOJET_REDIS_PASSWORD"), redisDB)
	}
	return buildFilesHandlerWithAuthority(db, redisClient, testAuth)
}

func buildFilesHandlerWithAuthority(db *sql.DB, redisClient *redis.Client, testAuth bool) (http.Handler, bool, error) {
	if os.Getenv("GOJET_FILES_ENABLED") != "1" {
		return nil, false, nil
	}
	maxFiles, err := requiredUint64Env("GOJET_FILE_WORKSPACE_MAX_FILES")
	if err != nil {
		return nil, false, err
	}
	maxBytes, err := requiredUint64Env("GOJET_FILE_WORKSPACE_MAX_BYTES")
	if err != nil {
		return nil, false, err
	}
	maxUpload, err := requiredInt64Env("GOJET_FILE_MAX_UPLOAD_BYTES")
	if err != nil {
		return nil, false, err
	}
	if uint64(maxUpload) > maxBytes {
		return nil, false, fmt.Errorf("GOJET_FILE_MAX_UPLOAD_BYTES exceeds GOJET_FILE_WORKSPACE_MAX_BYTES")
	}
	root := strings.TrimSpace(os.Getenv("GOJET_FILE_STORAGE_ROOT"))
	if root == "" {
		return nil, false, fmt.Errorf("GOJET_FILE_STORAGE_ROOT is required when files are enabled")
	}
	publicAuthSecret := strings.TrimSpace(os.Getenv("GOJET_FILE_PUBLIC_AUTH_SECRET"))
	if len(publicAuthSecret) < 32 {
		return nil, false, fmt.Errorf("GOJET_FILE_PUBLIC_AUTH_SECRET must be at least 32 bytes when files are enabled")
	}
	policyRaw := strings.TrimSpace(os.Getenv("GOJET_FILE_TYPE_ALLOWLIST"))
	if policyRaw == "" {
		return nil, false, fmt.Errorf("GOJET_FILE_TYPE_ALLOWLIST is required when files are enabled")
	}
	healthAuthority, err := buildFilesHealthAuthority(root)
	if err != nil {
		return nil, false, err
	}
	storage, err := files.NewNativeStorage(root)
	if err != nil {
		return nil, false, fmt.Errorf("configure file storage: %w", err)
	}
	policy, err := files.ParseTypePolicy(policyRaw)
	if err != nil {
		return nil, false, fmt.Errorf("configure file type policy: %w", err)
	}
	store, err := files.NewResourceStoreWithQuota(db, maxFiles, maxBytes)
	if err != nil {
		return nil, false, fmt.Errorf("configure file store: %w", err)
	}
	var api *files.API
	if testAuth {
		api, err = files.NewAPI(store, storage, policy, true, maxUpload, []byte(publicAuthSecret))
	} else {
		authority, authorityErr := buildFilesSessionAuthority(db, redisClient)
		if authorityErr != nil {
			return nil, false, fmt.Errorf("configure Files authentication authority: %w", authorityErr)
		}
		api, err = files.NewAPIWithActorResolver(store, storage, policy, authority.resolve, maxUpload, []byte(publicAuthSecret))
	}
	if err != nil {
		return nil, false, fmt.Errorf("configure file API: %w", err)
	}
	healthAPI, err := files.NewHealthAPI(healthAuthority, testAuth)
	if err != nil {
		return nil, false, fmt.Errorf("configure file health API: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/admin/platform/storage", healthAPI.Handler())
	mux.Handle("/", api.Handler())
	return mux, true, nil
}

func buildFilesHealthAuthority(root string) (*files.HealthAuthority, error) {
	clamNetwork := strings.TrimSpace(os.Getenv("GOJET_CLAMAV_NETWORK"))
	clamAddress := strings.TrimSpace(os.Getenv("GOJET_CLAMAV_ADDRESS"))
	if clamNetwork == "" || clamAddress == "" {
		authority, err := files.NewUnavailableHealthAuthority(root)
		if err != nil {
			return nil, fmt.Errorf("configure unavailable file health authority: %w", err)
		}
		return authority, nil
	}
	dialTimeout, err := optionalDurationEnv("GOJET_CLAMAV_DIAL_TIMEOUT", 2*time.Second)
	if err != nil {
		return nil, err
	}
	scanTimeout, err := optionalDurationEnv("GOJET_CLAMAV_SCAN_TIMEOUT", 30*time.Second)
	if err != nil {
		return nil, err
	}
	maxSignatureAge, err := optionalDurationEnv("GOJET_CLAMAV_MAX_SIGNATURE_AGE", 48*time.Hour)
	if err != nil {
		return nil, err
	}
	clamav, err := files.NewClamAVClient(clamNetwork, clamAddress, dialTimeout, scanTimeout, maxSignatureAge)
	if err != nil {
		return nil, fmt.Errorf("configure ClamAV health client: %w", err)
	}
	authority, err := files.NewHealthAuthority(root, clamav)
	if err != nil {
		return nil, fmt.Errorf("configure file health authority: %w", err)
	}
	return authority, nil
}

func requiredUint64Env(name string) (uint64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	value, err := strconv.ParseUint(raw, 10, 64)
	if raw == "" || err != nil || value == 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func requiredInt64Env(name string) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	value, err := strconv.ParseInt(raw, 10, 64)
	if raw == "" || err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func optionalDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return value, nil
}
