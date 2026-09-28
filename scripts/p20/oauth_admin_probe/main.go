// OAuth administrator persistence proof. This does not claim external login proof.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/admin"
	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func must(err error) {
	if err != nil {
		// Do not serialize database errors, submitted credentials or HTTP bodies.
		panic("OAuth administrator probe operation failed")
	}
}

func main() {
	ctx := context.Background()
	runtime, err := adminfixture.Open()
	must(err)
	defer runtime.Close()
	must(adminfixture.Reset(ctx, runtime))
	service, err := adminfixture.NewService(runtime, "t025-oauth", 100)
	must(err)
	now := time.Now().UTC().Add(-time.Minute)
	const email = "oauth-admin@p20.test"
	const password = "P20-OAuth-Fixture-Password-2026!"
	_, err = adminfixture.Bootstrap(ctx, service, email, password, []string{admin.PermissionSettingsManage}, now)
	must(err)
	_, login, _, err := adminfixture.LoginAndConfirmMFA(ctx, service, email, password, now.Add(time.Second))
	must(err)
	crypto, err := authn.NewOAuthCrypto("p20-fixture", bytes.Repeat([]byte{0x37}, 32))
	must(err)
	oauth, err := authn.NewOAuthService(runtime.DB, crypto, 10*time.Minute)
	must(err)
	api, err := admin.NewHTTPAPI(service)
	must(err)
	server := httptest.NewServer(api.OAuthGovernanceHandler(oauth))
	defer server.Close()
	checks := map[string]bool{}
	request := func(method, path, origin, token, csrf, key string, body any) adminfixture.HTTPResult {
		result, err := adminfixture.Request(ctx, server, method, path, origin, token, csrf, key, "p20-oauth-probe", body)
		must(err)
		return result
	}
	const listPath = "/api/admin/oauth/providers"
	const path = listPath + "/google"
	list := func() adminfixture.HTTPResult {
		return request("GET", listPath, "", login.Token, "", "", nil)
	}
	initial := list()
	checks["administrator_list"] = initial.Status == 200 && initial.Body["authority"] == "administrator" && adminfixture.NoStoreNoIndex(initial)
	registry, ok := initial.Body["providers"].([]any)
	checks["production_registry_has_eight_providers"] = ok && len(registry) == 8
	var version uint64
	must(runtime.DB.QueryRowContext(ctx, "SELECT version FROM oauth_provider_configs WHERE provider='google'").Scan(&version))
	const secret = "p20-fixture-client-secret-not-a-live-credential"
	body := map[string]any{
		"enabled": true, "client_id": "p20-fixture.apps.googleusercontent.com", "client_secret": secret,
		"authorization_url": "https://accounts.google.com/o/oauth2/v2/auth", "token_url": "https://oauth2.googleapis.com/token",
		"userinfo_url": "https://openidconnect.googleapis.com/v1/userinfo", "redirect_uri": "https://site.p20.test/oauth/google/callback",
		"scopes": []string{"openid", "email", "profile"}, "expected_version": version, "reason": "Verify administrator OAuth persistence",
	}
	patch := func(key string) adminfixture.HTTPResult {
		return request("PATCH", path, adminfixture.AllowedOrigin, login.Token, adminfixture.CSRF(list()), key, body)
	}
	saved := patch("p20-oauth-save")
	checks["save"] = saved.Status == 200 && saved.Body["replayed"] == false
	replay := patch("p20-oauth-save")
	checks["idempotent_replay"] = replay.Status == 200 && replay.Body["replayed"] == true
	body["client_id"] = "changed.apps.googleusercontent.com"
	checks["idempotency_payload_conflict"] = patch("p20-oauth-save").Status == 409
	body["client_id"] = "p20-fixture.apps.googleusercontent.com"
	checks["stale_version_conflict"] = patch("p20-oauth-stale").Status == 409
	var before []byte
	must(runtime.DB.QueryRowContext(ctx, "SELECT client_secret_ciphertext FROM oauth_provider_configs WHERE provider='google'").Scan(&before))
	plain, err := crypto.Decrypt(before, crypto.KeyID(), "oauth_client_secret:google")
	checks["encrypted_secret"] = err == nil && plain == secret && !bytes.Contains(before, []byte(secret))
	body["expected_version"] = version + 1
	body["client_secret"] = ""
	checks["blank_secret_save"] = patch("p20-oauth-retain").Status == 200
	var after []byte
	var finalVersion uint64
	must(runtime.DB.QueryRowContext(ctx, "SELECT client_secret_ciphertext,version FROM oauth_provider_configs WHERE provider='google'").Scan(&after, &finalVersion))
	checks["secret_retained"] = bytes.Equal(before, after) && finalVersion == version+2
	checks["missing_csrf_denied"] = request("PATCH", path, adminfixture.AllowedOrigin, login.Token, "", "p20-oauth-no-csrf", body).Status == 403
	checks["foreign_origin_denied"] = request("PATCH", path, "https://untrusted.test", login.Token, adminfixture.CSRF(list()), "p20-oauth-origin", body).Status == 403
	checks["unauthenticated_denied"] = request("GET", listPath, "", "", "", "", nil).Status == 401
	// Force the audit write to fail after the configuration UPDATE. Both must
	// roll back in the same real database transaction.
	body["expected_version"] = version + 2
	body["client_id"] = "must-not-persist.apps.googleusercontent.com"
	_, err = runtime.DB.ExecContext(ctx, "CREATE TRIGGER p20_oauth_probe_reject_audit BEFORE INSERT ON admin_audit_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture audit failure'")
	must(err)
	failed := patch("p20-oauth-audit-failure")
	_, err = runtime.DB.ExecContext(ctx, "DROP TRIGGER p20_oauth_probe_reject_audit")
	must(err)
	var finalClientID string
	must(runtime.DB.QueryRowContext(ctx, "SELECT client_id,version FROM oauth_provider_configs WHERE provider='google'").Scan(&finalClientID, &finalVersion))
	checks["audit_failure_rolls_back_configuration"] = failed.Status >= 500 && finalVersion == version+2 && finalClientID == "p20-fixture.apps.googleusercontent.com"
	var audits, records int
	must(runtime.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin_audit_events WHERE action='admin.oauth.provider.update'").Scan(&audits))
	must(runtime.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin_idempotency_records WHERE action='admin.oauth.provider.update'").Scan(&records))
	checks["exactly_two_committed_mutations"] = audits == 2 && records == 2
	checks["responses_redacted"] = !strings.Contains(saved.Raw+replay.Raw+list().Raw, secret)
	for _, provider := range []string{"x", "linkedin"} {
		var enabled bool
		var providerVersion uint64
		must(runtime.DB.QueryRowContext(ctx, "SELECT enabled,version FROM oauth_provider_configs WHERE provider=?", provider).Scan(&enabled, &providerVersion))
		checks[provider+"_initially_disabled"] = !enabled && providerVersion == 1
		config := map[string]any{
			"enabled": true, "client_id": "p20-" + provider + "-client", "client_secret": "p20-" + provider + "-secret",
			"redirect_uri": "https://site.p20.test/oauth/" + provider + "/callback", "expected_version": providerVersion,
			"reason": "Verify optional direct-provider administration",
		}
		if provider == "x" {
			config["authorization_url"] = "https://x.com/i/oauth2/authorize"
			config["token_url"] = "https://api.x.com/2/oauth2/token"
			config["userinfo_url"] = "https://api.x.com/2/users/me"
			config["scopes"] = []string{"tweet.read", "users.read"}
		} else {
			config["authorization_url"] = "https://www.linkedin.com/oauth/v2/authorization"
			config["token_url"] = "https://www.linkedin.com/oauth/v2/accessToken"
			config["userinfo_url"] = "https://api.linkedin.com/v2/userinfo"
			config["scopes"] = []string{"openid", "profile", "email"}
		}
		response := request("PATCH", listPath+"/"+provider, adminfixture.AllowedOrigin, login.Token, adminfixture.CSRF(list()), "p20-config-"+provider, config)
		checks[provider+"_audited_save"] = response.Status == 200 && !strings.Contains(response.Raw, "p20-"+provider+"-secret")
	}
	passed := adminfixture.AllTrue(checks)
	must(json.NewEncoder(os.Stdout).Encode(map[string]any{"source_sha": os.Getenv("GITHUB_SHA"), "checks": checks, "passed": passed, "formal": false, "external_provider_exchange_verified": false}))
	if !passed {
		fmt.Fprintln(os.Stderr, "OAuth administrator persistence checks failed; inspect boolean evidence")
		os.Exit(1)
	}
}
