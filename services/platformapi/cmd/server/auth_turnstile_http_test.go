package main

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

type challengeHTTP func(*http.Request) (*http.Response, error)

func (f challengeHTTP) Do(r *http.Request) (*http.Response, error) { return f(r) }

type challengeReplay struct {
	seen map[[32]byte]bool
	fail bool
}

func (s *challengeReplay) ClaimDigest(_ context.Context, digest [32]byte) (bool, error) {
	if s.fail {
		return false, errors.New("fixture replay unavailable")
	}
	if s.seen[digest] {
		return false, nil
	}
	s.seen[digest] = true
	return true, nil
}

func TestAuthChallengeMutationOrdering(t *testing.T) {
	paths := []string{"/api/auth/login", "/api/auth/register", "/api/auth/forgotpassword", "/api/auth/resetpassword", "/api/public/login-email-code", "/api/public/email-code", "/api/public/register-email-code", "/api/auth/verifyemail", "/api/mail/verification", "/api/public/auth/social-registration/complete"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			mutations, upstream := 0, 0
			unavailable := false
			replay := &challengeReplay{seen: map[[32]byte]bool{}}
			gate := &authChallengeGate{load: func(context.Context) (authChallengePolicy, error) {
				return authChallengePolicy{Enabled: true, SiteKey: "fixture-site", Secret: "fixture-secret"}, nil
			}, replay: replay,
				client: challengeHTTP(func(r *http.Request) (*http.Response, error) {
					upstream++
					if unavailable {
						return nil, errors.New("fixture upstream outage")
					}
					if r.URL.String() != "https://challenges.cloudflare.com/turnstile/v0/siteverify" || r.Method != "POST" {
						t.Fatal("wrong verification authority")
					}
					if err := r.ParseForm(); err != nil || r.Form.Get("secret") != "fixture-secret" {
						t.Fatal("secret not sent to verifier")
					}
					body := `{"success":false}`
					if strings.HasPrefix(r.Form.Get("response"), "valid-") {
						body = `{"success":true}`
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
				})}
			handler := gate.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { mutations++; w.WriteHeader(204) }))
			call := func(token string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", path, strings.NewReader(`{"email":"fixture@example.test"}`))
				r.Header.Set("X-Turnstile-Token", token)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				if strings.Contains(w.Body.String(), token) && token != "" {
					t.Fatal("verification token reflected")
				}
				if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
					t.Fatal("auth protection response cacheable")
				}
				return w
			}
			for _, token := range []string{"", "invalid", "expired", strings.Repeat("a", 2049)} {
				if call(token).Code != 400 {
					t.Fatal("invalid challenge accepted")
				}
			}
			if mutations != 0 || upstream != 2 {
				t.Fatal("invalid token reached mutation or missing/oversized token reached upstream")
			}
			if call("valid-once").Code != 204 || mutations != 1 {
				t.Fatal("valid challenge did not reach mutation")
			}
			if call("valid-once").Code != 400 || mutations != 1 {
				t.Fatal("replay reached mutation")
			}
			replay.fail = true
			if call("valid-redis-outage").Code != 400 || mutations != 1 {
				t.Fatal("Redis failure opened mutation")
			}
			replay.fail = false
			unavailable = true
			if call("valid-upstream-outage").Code != 400 || mutations != 1 {
				t.Fatal("provider failure opened mutation")
			}
			gate.load = func(context.Context) (authChallengePolicy, error) {
				return authChallengePolicy{}, errors.New("configuration unavailable")
			}
			if call("valid-config-outage").Code != 400 || mutations != 1 {
				t.Fatal("configuration failure opened mutation")
			}
			gate.load = func(context.Context) (authChallengePolicy, error) { return authChallengePolicy{}, nil }
			if call("").Code != 204 || mutations != 2 {
				t.Fatal("explicit disabled policy not honored")
			}
		})
	}
}

func TestAuthChallengeAdministratorConfigAndRedis(t *testing.T) {
	if os.Getenv("GOJET_P20_OAUTH_BROWSER_PROBE") != "1" {
		t.Skip("requires isolated migrated administrator database and Redis")
	}
	runtime, err := adminfixture.Open()
	if err != nil {
		t.Fatal("fixture unavailable")
	}
	defer runtime.Close()
	ctx := context.Background()
	p, err := loadAuthChallengePolicy(ctx, runtime.DB)
	if err != nil || p.Enabled {
		t.Fatal("unconfigured administrator default changed")
	}
	key, err := hex.DecodeString(os.Getenv("GOJET_ADMIN_TOTP_KEY_HEX"))
	if err != nil {
		t.Fatal("fixture key invalid")
	}
	cipher, err := adminaccess.NewSecretCipher(os.Getenv("GOJET_ADMIN_TOTP_KEY_ID"), key)
	if err != nil {
		t.Fatal("fixture cipher invalid")
	}
	encrypted, err := cipher.Encrypt("fixture-turnstile-secret", "admin-turnstile:singleton")
	if err != nil {
		t.Fatal("fixture encryption failed")
	}
	_, err = runtime.DB.ExecContext(ctx, `INSERT INTO admin_turnstile_config(id,site_key,secret_ciphertext,secret_key_id,enabled,provider_state,version,updated_by) SELECT 1,'fixture-site',?,?,1,'healthy',1,id FROM admin_administrators LIMIT 1`, encrypted, os.Getenv("GOJET_ADMIN_TOTP_KEY_ID"))
	if err != nil {
		t.Fatal("fixture configuration failed")
	}
	defer runtime.DB.ExecContext(ctx, "DELETE FROM admin_turnstile_config WHERE id=1")
	gate, err := buildAuthChallengeGate(runtime.DB, runtime.Redis)
	if err != nil {
		t.Fatal("real gate unavailable")
	}
	gate.client = challengeHTTP(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil || r.Form.Get("secret") != "fixture-turnstile-secret" {
			t.Fatal("administrator secret not used")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"success":true}`))}, nil
	})
	mutations := 0
	h := gate.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { mutations++; w.WriteHeader(204) }))
	call := func(token string) int {
		r := httptest.NewRequest("POST", "/api/auth/register", nil)
		r.Header.Set("X-Turnstile-Token", token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if call("") != 400 || mutations != 0 {
		t.Fatal("enabled persisted policy bypassed")
	}
	if call("p20-auth-challenge-fixture-once") != 204 || mutations != 1 {
		t.Fatal("valid persisted policy failed")
	}
	if call("p20-auth-challenge-fixture-once") != 400 || mutations != 1 {
		t.Fatal("real Redis did not reject replay")
	}
	if _, err = runtime.DB.ExecContext(ctx, "UPDATE admin_turnstile_config SET provider_state='provider_error' WHERE id=1"); err != nil {
		t.Fatal("fixture update failed")
	}
	if call("p20-auth-fresh-fixture") != 400 || mutations != 1 {
		t.Fatal("provider state update did not fail closed immediately")
	}

	t.Run("OfficialCloudflareRegistration", func(t *testing.T) {
		if os.Getenv("GOJET_P20_AUTH_TURNSTILE_EXTERNAL_PROBE") != "1" {
			t.Skip("explicit isolated external test authority required")
		}
		const officialTestSecret = "1x0000000000000000000000000000000AA"
		if os.Getenv("GOJET_TURNSTILE_SECRET") != officialTestSecret {
			t.Fatal("only official isolated test credential is allowed")
		}
		ciphertext, err := cipher.Encrypt(officialTestSecret, "admin-turnstile:singleton")
		if err != nil {
			t.Fatal("test credential encryption failed")
		}
		_, err = runtime.DB.ExecContext(ctx, "UPDATE admin_turnstile_config SET site_key='1x00000000000000000000AA',secret_ciphertext=?,provider_state='healthy' WHERE id=1", ciphertext)
		if err != nil {
			t.Fatal("test authority setup failed")
		}
		productionGate, err := buildAuthChallengeGate(runtime.DB, runtime.Redis)
		if err != nil {
			t.Fatal("production challenge gate unavailable")
		}
		// No injected HTTP client: inherited verifier calls the real fixed Cloudflare
		// siteverify endpoint. This uses an official test key, not a human challenge.
		handler, enabled, err := buildAuthHandler(runtime.DB, false)
		if err != nil || !enabled {
			t.Fatal("production authentication builder unavailable")
		}
		root := http.NewServeMux()
		mountAuthRoutes(root, productionGate.wrap(handler))
		const address = "t025-external-registration@example.test"
		counts := func() (int, int, int) {
			t.Helper()
			var users, grants, audits int
			if runtime.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_users WHERE email_normalized=?", address).Scan(&users) != nil {
				t.Fatal("user state unavailable")
			}
			if runtime.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_one_time_grants WHERE email_normalized=?", address).Scan(&grants) != nil {
				t.Fatal("grant state unavailable")
			}
			if runtime.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_audit_events a JOIN auth_users u ON u.id=a.user_id WHERE u.email_normalized=? AND a.action='auth.registration.created'", address).Scan(&audits) != nil {
				t.Fatal("audit state unavailable")
			}
			return users, grants, audits
		}
		request := func(token string) int {
			t.Helper()
			req := httptest.NewRequest("POST", "/api/auth/register", strings.NewReader(`{"email":"t025-external-registration@example.test","display_name":"External verification fixture","password":"Fixture-Registration-2026!","correlation_id":"p20-t025-external-registration"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Turnstile-Token", token)
			response := httptest.NewRecorder()
			root.ServeHTTP(response, req)
			if strings.Contains(response.Body.String(), address) || strings.Contains(response.Body.String(), officialTestSecret) || (token != "" && strings.Contains(response.Body.String(), token)) {
				t.Fatal("sensitive input reflected")
			}
			return response.Code
		}
		if request("") != 400 {
			t.Fatal("missing challenge reached registration")
		}
		if u, g, a := counts(); u != 0 || g != 0 || a != 0 {
			t.Fatal("denied request wrote durable registration state")
		}
		if request("XXXX.DUMMY.TOKEN.XXXX") != 202 {
			t.Fatal("official siteverify did not authorize production registration")
		}
		if u, g, a := counts(); u != 1 || g != 1 || a != 1 {
			t.Fatal("verified registration did not commit exactly once")
		}
		if request("XXXX.DUMMY.TOKEN.XXXX") != 400 {
			t.Fatal("verified token replay reached registration")
		}
		if u, g, a := counts(); u != 1 || g != 1 || a != 1 {
			t.Fatal("replay changed durable registration state")
		}
	})
}
