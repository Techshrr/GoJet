package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
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
	oauth, err := auth.NewOAuthService(db, crypto, 5*time.Minute)
	if err != nil {
		fail(err)
	}

	stamp := time.Now().UTC().UnixNano()
	now := time.Now().UTC()
	admin, err := runnerutil.ActivateUser(ctx, db, fmt.Sprintf("p15-t018-admin-%d@example.test", stamp), "P15 T018 Admin", now)
	if err != nil {
		fail(err)
	}
	session, err := runnerutil.CreateSession(ctx, db, admin.ID, fmt.Sprintf("p15-t018-session-%d", stamp), time.Hour)
	if err != nil {
		fail(err)
	}
	permission := &runnerutil.PermissionRecorder{Allowed: true}
	authority, err := runnerutil.MutationAuthority(ctx, redisClient, session.Session, http.MethodPatch, runnerutil.AllowedOrigin, now)
	if err != nil {
		fail(err)
	}
	secret := "p15-t018-client-secret"
	cfg, err := oauth.UpdateProviderConfig(ctx, session.Session, authority, permission, admin.ID, fmt.Sprintf("p15-t018-config-%d", stamp), providerUpdate(auth.ProviderGitHub, secret), now)
	if err != nil {
		fail(err)
	}

	start, err := oauth.Start(ctx, auth.OAuthStartInput{Provider: auth.ProviderGitHub, Intent: auth.OAuthIntentLogin, CorrelationID: fmt.Sprintf("p15-t018-start-%d", stamp)}, now.Add(time.Millisecond))
	if err != nil {
		fail(err)
	}
	adapter := &runnerutil.DeterministicOAuthAdapter{ExpectedProvider: auth.ProviderGitHub, ExpectedCode: "p15-t018-good-code", ExpectedClientID: cfg.ClientID, ExpectedClientSecret: secret, ExpectedRedirectURI: cfg.RedirectURI, ExpectedPKCEVerifier: start.PKCEVerifier, Claim: auth.OAuthProviderClaim{Subject: "p15-t018-subject", Email: "p15-t018@example.test", EmailVerified: true, DisplayName: "P15 T018"}}

	badState := start.State[:len(start.State)-1] + alternateLast(start.State[len(start.State)-1:])
	_, mismatchErr := oauth.Callback(ctx, adapter, auth.OAuthCallbackInput{Provider: auth.ProviderGitHub, State: badState, Code: "p15-t018-good-code", CorrelationID: fmt.Sprintf("p15-t018-mismatch-%d", stamp)}, now.Add(2*time.Millisecond))
	consumedAfterMismatch := consumed(ctx, db, start.StateID)
	identitiesAfterMismatch, _ := runnerutil.Count(ctx, db, `SELECT COUNT(*) FROM oauth_identities`)

	_, providerErr := oauth.Callback(ctx, adapter, auth.OAuthCallbackInput{Provider: auth.ProviderGitHub, State: start.State, Code: "p15-t018-wrong-code", CorrelationID: fmt.Sprintf("p15-t018-provider-fail-%d", stamp)}, now.Add(3*time.Millisecond))
	consumedAfterProviderFailure := consumed(ctx, db, start.StateID)
	callback, successErr := oauth.Callback(ctx, adapter, auth.OAuthCallbackInput{Provider: auth.ProviderGitHub, State: start.State, Code: "p15-t018-good-code", CorrelationID: fmt.Sprintf("p15-t018-success-%d", stamp)}, now.Add(4*time.Millisecond))
	consumedAfterSuccess := consumed(ctx, db, start.StateID)
	callsAfterSuccess := adapter.Calls
	_, replayErr := oauth.Callback(ctx, adapter, auth.OAuthCallbackInput{Provider: auth.ProviderGitHub, State: start.State, Code: "p15-t018-good-code", CorrelationID: fmt.Sprintf("p15-t018-replay-%d", stamp)}, now.Add(5*time.Millisecond))

	expiredStart, err := oauth.Start(ctx, auth.OAuthStartInput{Provider: auth.ProviderGitHub, Intent: auth.OAuthIntentLogin, CorrelationID: fmt.Sprintf("p15-t018-expired-start-%d", stamp)}, now.Add(6*time.Millisecond))
	if err != nil {
		fail(err)
	}
	past := now.Add(-time.Minute).Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `UPDATE oauth_states SET expires_at=? WHERE id=?`, past, expiredStart.StateID); err != nil {
		fail(err)
	}
	expiredAdapter := &runnerutil.DeterministicOAuthAdapter{ExpectedProvider: auth.ProviderGitHub, ExpectedCode: "unused", ExpectedClientID: cfg.ClientID, ExpectedClientSecret: secret, ExpectedRedirectURI: cfg.RedirectURI, ExpectedPKCEVerifier: expiredStart.PKCEVerifier, Claim: auth.OAuthProviderClaim{Subject: "unused"}}
	_, expiredErr := oauth.Callback(ctx, expiredAdapter, auth.OAuthCallbackInput{Provider: auth.ProviderGitHub, State: expiredStart.State, Code: "unused", CorrelationID: fmt.Sprintf("p15-t018-expired-%d", stamp)}, now.Add(7*time.Millisecond))

	var metadata string
	if err := db.QueryRowContext(ctx, `SELECT CAST(metadata_json AS CHAR) FROM auth_audit_events WHERE action='auth.oauth.callback' AND resource_id=? ORDER BY id DESC LIMIT 1`, start.StateID).Scan(&metadata); err != nil {
		fail(err)
	}
	leakFree := true
	for _, fragment := range []string{start.State, start.PKCEVerifier, "p15-t018-good-code", secret, callback.ProviderSubject} {
		if fragment != "" && strings.Contains(metadata, fragment) {
			leakFree = false
		}
	}
	audits, err := runnerutil.Count(ctx, db, `SELECT COUNT(*) FROM auth_audit_events WHERE action='auth.oauth.callback' AND resource_id=? AND result='success'`, start.StateID)
	if err != nil {
		fail(err)
	}

	checks := map[string]bool{
		"state_mismatch_fails_before_provider_or_identity_mutation": errors.Is(mismatchErr, auth.ErrForbidden) && adapter.Calls == callsAfterSuccess && !consumedAfterMismatch && identitiesAfterMismatch == 0,
		"provider_or_pkce_failure_does_not_consume_state":           errors.Is(providerErr, auth.ErrForbidden) && !consumedAfterProviderFailure,
		"valid_callback_consumes_state_once":                        successErr == nil && callback.StateID == start.StateID && consumedAfterSuccess && callsAfterSuccess == 2,
		"callback_replay_fails_before_provider_exchange":            errors.Is(replayErr, auth.ErrReplay) && adapter.Calls == callsAfterSuccess,
		"expired_state_fails_before_provider_exchange":              errors.Is(expiredErr, auth.ErrExpired) && expiredAdapter.Calls == 0,
		"callback_audit_redacts_state_code_pkce_secret_subject":     leakFree && audits == 1,
	}
	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := result{Case: "P15-T018", Status: status, MySQLVersion: mysqlVersion, RecordCounts: map[string]int{"provider_exchange_calls": adapter.Calls, "callback_success_audits": audits}, Checks: checks}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}

func consumed(ctx context.Context, db *sql.DB, stateID string) bool {
	var value sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT consumed_at FROM oauth_states WHERE id=?`, stateID).Scan(&value); err != nil {
		fail(err)
	}
	return value.Valid
}

func alternateLast(last string) string {
	if last == "A" {
		return "B"
	}
	return "A"
}

func providerUpdate(provider, secret string) auth.OAuthProviderUpdate {
	return auth.OAuthProviderUpdate{Provider: provider, Enabled: true, ClientID: "p15-t018-client-id", ClientSecret: secret, AuthorizationURL: "https://provider.example/authorize", TokenURL: "https://provider.example/token", UserInfoURL: "https://provider.example/userinfo", RedirectURI: "https://gojet.example/oauth/" + provider + "/callback", Scopes: []string{"openid", "email"}}
}
