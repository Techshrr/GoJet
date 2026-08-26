package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/scripts/p15/runnerutil"
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
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	db, mysqlVersion, err := runnerutil.OpenMySQL(ctx)
	if err != nil {
		fail(err)
	}
	defer db.Close()
	redisClient, err := runnerutil.OpenRedis(ctx)
	if err != nil {
		fail(err)
	}
	defer redisClient.Close()
	crypto, err := runnerutil.OAuthCrypto()
	if err != nil {
		fail(err)
	}
	oauth, err := auth.NewOAuthService(db, crypto, 10*time.Minute)
	if err != nil {
		fail(err)
	}

	stamp := time.Now().UTC().UnixNano()
	now := time.Now().UTC()
	user, err := runnerutil.ActivateUser(ctx, db, fmt.Sprintf("p15-t016-%d@example.test", stamp), "P15 T016", now)
	if err != nil {
		fail(err)
	}
	current, err := runnerutil.CreateSession(ctx, db, user.ID, fmt.Sprintf("p15-t016-session-%d", stamp), time.Hour)
	if err != nil {
		fail(err)
	}
	initial, initialErr := oauth.ListProviderConfigs(ctx)

	deniedPermission := &runnerutil.PermissionRecorder{Allowed: false}
	deniedAuthority, err := runnerutil.MutationAuthority(ctx, redisClient, current.Session, http.MethodPatch, runnerutil.AllowedOrigin, now)
	if err != nil {
		fail(err)
	}
	_, deniedErr := oauth.UpdateProviderConfig(ctx, current.Session, deniedAuthority, deniedPermission, user.ID, fmt.Sprintf("p15-t016-denied-%d", stamp), providerUpdate(auth.ProviderGoogle, "p15-t016-denied-secret"), now)

	permission := &runnerutil.PermissionRecorder{Allowed: true}
	validAuthority, err := runnerutil.MutationAuthority(ctx, redisClient, current.Session, http.MethodPatch, runnerutil.AllowedOrigin, now.Add(time.Millisecond))
	if err != nil {
		fail(err)
	}
	secret := "p15-t016-client-secret"
	updated, updateErr := oauth.UpdateProviderConfig(ctx, current.Session, validAuthority, permission, user.ID, fmt.Sprintf("p15-t016-update-%d", stamp), providerUpdate(auth.ProviderGoogle, secret), now.Add(time.Millisecond))
	configs, listErr := oauth.ListProviderConfigs(ctx)

	var ciphertext []byte
	var keyID sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT client_secret_ciphertext,secret_key_id FROM oauth_provider_configs WHERE provider='google'`).Scan(&ciphertext, &keyID); err != nil {
		fail(err)
	}
	projection, err := json.Marshal(updated)
	if err != nil {
		fail(err)
	}

	expectedProviders := []string{auth.ProviderGoogle, auth.ProviderFacebook, auth.ProviderGitHub, auth.ProviderQQ, auth.ProviderWeChat, auth.ProviderRainbow}
	actualProviders := make([]string, 0, len(configs))
	for _, cfg := range configs {
		actualProviders = append(actualProviders, cfg.Provider)
	}
	configuredCount := 0
	googleConfiguredCount := 0
	for _, cfg := range configs {
		if cfg.Configured {
			configuredCount++
		}
		if cfg.Provider == auth.ProviderGoogle && cfg.Configured {
			googleConfiguredCount++
		}
	}

	checks := map[string]bool{
		"registry_is_exactly_six_frozen_providers":                  initialErr == nil && len(initial) == 6 && listErr == nil && reflect.DeepEqual(actualProviders, expectedProviders),
		"target_provider_initially_server_derived_unconfigured":     providerUnconfigured(initial, auth.ProviderGoogle),
		"settings_manage_denial_fails_closed":                       errors.Is(deniedErr, auth.ErrForbidden) && deniedPermission.Seen == auth.SettingsManage,
		"authorized_provider_update_is_server_derived":              updateErr == nil && updated.Provider == auth.ProviderGoogle && updated.Enabled && updated.Configured && updated.SecretConfigured && googleConfiguredCount == 1,
		"client_secret_is_encrypted_and_masked":                     keyID.Valid && len(ciphertext) > 32 && !bytes.Equal(ciphertext, []byte(secret)) && !bytes.Contains(projection, []byte(secret)),
		"p15_consumes_settings_manage_without_permission_lifecycle": permission.Seen == auth.SettingsManage,
	}
	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := result{Case: "P15-T016", Status: status, MySQLVersion: mysqlVersion, RecordCounts: map[string]int{"provider_count": len(configs), "configured_provider_count": configuredCount, "google_configured_count": googleConfiguredCount}, Checks: checks}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}

func providerUnconfigured(configs []auth.OAuthProviderConfig, provider string) bool {
	for _, cfg := range configs {
		if cfg.Provider == provider {
			return !cfg.Enabled && !cfg.Configured && !cfg.SecretConfigured
		}
	}
	return false
}

func providerUpdate(provider, secret string) auth.OAuthProviderUpdate {
	return auth.OAuthProviderUpdate{Provider: provider, Enabled: true, ClientID: "p15-t016-client-id", ClientSecret: secret, AuthorizationURL: "https://provider.example/authorize", TokenURL: "https://provider.example/token", UserInfoURL: "https://provider.example/userinfo", RedirectURI: "https://gojet.example/oauth/" + provider + "/callback", Scopes: []string{"openid", "email"}}
}
