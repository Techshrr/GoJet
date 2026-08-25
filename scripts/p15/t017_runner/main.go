package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
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
	admin, err := runnerutil.ActivateUser(ctx, db, fmt.Sprintf("p15-t017-admin-%d@example.test", stamp), "P15 T017 Admin", now)
	if err != nil {
		fail(err)
	}
	session, err := runnerutil.CreateSession(ctx, db, admin.ID, fmt.Sprintf("p15-t017-admin-session-%d", stamp), time.Hour)
	if err != nil {
		fail(err)
	}
	permission := &runnerutil.PermissionRecorder{Allowed: true}
	authority, err := runnerutil.MutationAuthority(ctx, redisClient, session.Session, http.MethodPatch, runnerutil.AllowedOrigin, now)
	if err != nil {
		fail(err)
	}
	secret := "p15-t017-client-secret"
	cfg, err := oauth.UpdateProviderConfig(ctx, session.Session, authority, permission, admin.ID, fmt.Sprintf("p15-t017-config-%d", stamp), providerUpdate(auth.ProviderGoogle, secret), now)
	if err != nil {
		fail(err)
	}

	first, firstErr := oauth.Start(ctx, auth.OAuthStartInput{Provider: auth.ProviderGoogle, Intent: auth.OAuthIntentLogin, CorrelationID: fmt.Sprintf("p15-t017-start-a-%d", stamp)}, now.Add(time.Millisecond))
	second, secondErr := oauth.Start(ctx, auth.OAuthStartInput{Provider: auth.ProviderGoogle, Intent: auth.OAuthIntentLogin, CorrelationID: fmt.Sprintf("p15-t017-start-b-%d", stamp)}, now.Add(2*time.Millisecond))
	parsed, parseErr := url.Parse(first.AuthorizationURL)
	query := url.Values{}
	if parseErr == nil {
		query = parsed.Query()
	}

	var stateHash, pkceCiphertext []byte
	var expiresAt time.Time
	if err := db.QueryRowContext(ctx, `SELECT state_hash,pkce_verifier_ciphertext,expires_at FROM oauth_states WHERE id=?`, first.StateID).Scan(&stateHash, &pkceCiphertext, &expiresAt); err != nil {
		fail(err)
	}

	badAuthority, err := runnerutil.MutationAuthority(ctx, redisClient, session.Session, http.MethodPatch, runnerutil.AllowedOrigin, now.Add(3*time.Millisecond))
	if err != nil {
		fail(err)
	}
	bad := providerUpdate(auth.ProviderGoogle, "p15-t017-bad-secret")
	bad.RedirectURI = "https://attacker.example/callback"
	_, badRedirectErr := oauth.UpdateProviderConfig(ctx, session.Session, badAuthority, permission, admin.ID, fmt.Sprintf("p15-t017-bad-redirect-%d", stamp), bad, now.Add(3*time.Millisecond))

	states, err := runnerutil.Count(ctx, db, `SELECT COUNT(*) FROM oauth_states WHERE provider='google' AND intent='login'`)
	if err != nil {
		fail(err)
	}
	checks := map[string]bool{
		"oauth_state_is_unpredictable_and_unique":         firstErr == nil && secondErr == nil && first.State != second.State && first.StateID != second.StateID && len(first.State) > 40,
		"state_is_hash_only_in_durable_storage":           len(stateHash) == 32 && !bytes.Equal(stateHash, []byte(first.State)),
		"pkce_verifier_is_encrypted_at_rest":              len(pkceCiphertext) > 32 && !bytes.Contains(pkceCiphertext, []byte(first.PKCEVerifier)),
		"authorization_uses_reviewed_server_redirect":     parseErr == nil && parsed.Scheme == "https" && query.Get("redirect_uri") == cfg.RedirectURI && query.Get("state") == first.State && query.Get("code_challenge_method") == "S256" && query.Get("code_challenge") != "",
		"state_is_expiry_bound":                           expiresAt.After(now) && !expiresAt.After(now.Add(11*time.Minute)) && first.ExpiresAt.Equal(expiresAt),
		"arbitrary_redirect_configuration_fails_closed":   errors.Is(badRedirectErr, auth.ErrInvalid),
		"multiple_start_records_are_server_authoritative": states == 2,
	}
	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := result{Case: "P15-T017", Status: status, MySQLVersion: mysqlVersion, RecordCounts: map[string]int{"oauth_state_records": states}, Checks: checks}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}

func providerUpdate(provider, secret string) auth.OAuthProviderUpdate {
	return auth.OAuthProviderUpdate{Provider: provider, Enabled: true, ClientID: "p15-t017-client-id", ClientSecret: secret, AuthorizationURL: "https://provider.example/authorize", TokenURL: "https://provider.example/token", UserInfoURL: "https://provider.example/userinfo", RedirectURI: "https://gojet.example/oauth/" + provider + "/callback", Scopes: []string{"openid", "email"}}
}
