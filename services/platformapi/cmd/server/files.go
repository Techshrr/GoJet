package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/Techshrr/GoJet/internal/files"
)

func buildFilesHandler(db *sql.DB, testAuth bool) (http.Handler, bool, error) {
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
	policyRaw := strings.TrimSpace(os.Getenv("GOJET_FILE_TYPE_ALLOWLIST"))
	if policyRaw == "" {
		return nil, false, fmt.Errorf("GOJET_FILE_TYPE_ALLOWLIST is required when files are enabled")
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
	api, err := files.NewAPI(store, storage, policy, testAuth, maxUpload)
	if err != nil {
		return nil, false, fmt.Errorf("configure file API: %w", err)
	}
	return api.Handler(), true, nil
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
