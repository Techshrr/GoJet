package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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
	user, err := runnerutil.ActivateUser(ctx, db, fmt.Sprintf("p15-t015-%d@example.test", stamp), "P15 T015", now)
	if err != nil {
		fail(err)
	}
	current, err := runnerutil.CreateSession(ctx, db, user.ID, fmt.Sprintf("p15-t015-session-%d", stamp), time.Hour)
	if err != nil {
		fail(err)
	}
	permission := &runnerutil.PermissionRecorder{Allowed: true}
	configAuthority, err := runnerutil.MutationAuthority(ctx, redisClient, current.Session, http.MethodPatch, runnerutil.AllowedOrigin, now)
	if err != nil {
		fail(err)
	}
	clientSecret := "p15-t015-client-secret"
	cfg, err := oauth.UpdateProviderConfig(ctx, current.Session, configAuthority, permission, user.ID, fmt.Sprintf("p15-t015-config-%d", stamp), providerUpdate(auth.ProviderGitHub, clientSecret), now)
	if err != nil {
		fail(err)
	}

	startAuthority, err := runnerutil.MutationAuthority(ctx, redisClient, current.Session, http.MethodPost, runnerutil.AllowedOrigin, now.Add(time.Millisecond))
	if err != nil {
		fail(err)
	}
	start, err := oauth.Start(ctx, auth.OAuthStartInput{Provider: auth.ProviderGitHub, Intent: auth.OAuthIntentBind, InitiatingUserID: user.ID, InitiatingSessionID: current.Session.ID, CorrelationID: fmt.Sprintf("p15-t015-start-%d", stamp), MutationAuthority: startAuthority}, now.Add(time.Millisecond))
	if err != nil {
		fail(err)
	}
	adapter := &runnerutil.DeterministicOAuthAdapter{ExpectedProvider: auth.ProviderGitHub, ExpectedCode: "p15-t015-code", ExpectedClientID: cfg.ClientID, ExpectedClientSecret: clientSecret, ExpectedRedirectURI: cfg.RedirectURI, ExpectedPKCEVerifier: start.PKCEVerifier, Claim: auth.OAuthProviderClaim{Subject: "p15-t015-provider-subject", Email: user.Email, EmailVerified: true, DisplayName: "P15 T015 GitHub"}}
	callback, err := oauth.Callback(ctx, adapter, auth.OAuthCallbackInput{Provider: auth.ProviderGitHub, State: start.State, Code: "p15-t015-code", CorrelationID: fmt.Sprintf("p15-t015-callback-%d", stamp)}, now.Add(2*time.Millisecond))
	if err != nil {
		fail(err)
	}

	bindAuthority, err := runnerutil.MutationAuthority(ctx, redisClient, current.Session, http.MethodPost, runnerutil.AllowedOrigin, now.Add(3*time.Millisecond))
	if err != nil {
		fail(err)
	}
	identity, bindErr := oauth.BindConnectedAccount(ctx, current.Session, bindAuthority, callback, fmt.Sprintf("p15-t015-bind-%d", stamp), now.Add(3*time.Millisecond))
	listed, listErr := oauth.ListConnectedAccounts(ctx, current.Session, now.Add(4*time.Millisecond))

	forged := callback
	forged.StateID = "ost_forged_state"
	forgedAuthority, err := runnerutil.MutationAuthority(ctx, redisClient, current.Session, http.MethodPost, runnerutil.AllowedOrigin, now.Add(5*time.Millisecond))
	if err != nil {
		fail(err)
	}
	_, forgedErr := oauth.BindConnectedAccount(ctx, current.Session, forgedAuthority, forged, fmt.Sprintf("p15-t015-forged-%d", stamp), now.Add(5*time.Millisecond))

	unbindAuthority, err := runnerutil.MutationAuthority(ctx, redisClient, current.Session, http.MethodDelete, runnerutil.AllowedOrigin, now.Add(6*time.Millisecond))
	if err != nil {
		fail(err)
	}
	unbindErr := oauth.UnbindConnectedAccount(ctx, current.Session, unbindAuthority, auth.ProviderGitHub, fmt.Sprintf("p15-t015-unbind-%d", stamp), now.Add(6*time.Millisecond))
	after, afterErr := oauth.ListConnectedAccounts(ctx, current.Session, now.Add(7*time.Millisecond))
	audits, err := runnerutil.Count(ctx, db, `SELECT COUNT(*) FROM auth_audit_events WHERE user_id=? AND action IN ('auth.oauth.connected','auth.oauth.disconnected') AND result='success'`, user.ID)
	if err != nil {
		fail(err)
	}
	remaining, err := runnerutil.Count(ctx, db, `SELECT COUNT(*) FROM oauth_identities WHERE user_id=?`, user.ID)
	if err != nil {
		fail(err)
	}

	checks := map[string]bool{
		"bind_requires_current_account_and_consumed_state":  bindErr == nil && identity.UserID == user.ID && identity.Provider == auth.ProviderGitHub,
		"connected_account_projection_is_safe":              listErr == nil && len(listed) == 1 && listed[0].ID == identity.ID && listed[0].Provider == auth.ProviderGitHub,
		"forged_state_cannot_bind_identity":                 errors.Is(forgedErr, auth.ErrForbidden),
		"unbind_is_owned_and_audited":                       unbindErr == nil && afterErr == nil && len(after) == 0 && remaining == 0 && audits == 2,
		"settings_permission_is_consumed_not_owned":         permission.Seen == auth.SettingsManage,
		"provider_callback_adapter_used_server_credentials": adapter.Calls == 1,
	}
	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := result{Case: "P15-T015", Status: status, MySQLVersion: mysqlVersion, RecordCounts: map[string]int{"connected_accounts_before_unbind": len(listed), "connected_accounts_after_unbind": remaining, "connection_audits": audits}, Checks: checks}
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
	return auth.OAuthProviderUpdate{Provider: provider, Enabled: true, ClientID: "p15-client-id", ClientSecret: secret, AuthorizationURL: "https://provider.example/authorize", TokenURL: "https://provider.example/token", UserInfoURL: "https://provider.example/userinfo", RedirectURI: "https://gojet.example/oauth/" + provider + "/callback", Scopes: []string{"openid", "email"}}
}
