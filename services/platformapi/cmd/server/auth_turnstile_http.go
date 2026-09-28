package main

import (
 "context"
 "database/sql"
 "encoding/hex"
 "errors"
 "net/http"
 "os"
 "strings"
 "time"

 adminaccess "github.com/Techshrr/GoJet/internal/admin"
 authn "github.com/Techshrr/GoJet/internal/auth"
 "github.com/Techshrr/GoJet/internal/support"
 "github.com/redis/go-redis/v9"
)

type authChallengePolicy struct {
 Enabled bool
 SiteKey string
 Secret string
}

type authChallengeGate struct {
 load func(context.Context) (authChallengePolicy, error)
 replay support.TurnstileReplayStore
 client support.HTTPDoer
}

// Read the existing P17-owned switch on each request: configuration changes and
// provider failure must take effect without restarting the authentication API.
func loadAuthChallengePolicy(ctx context.Context, db *sql.DB) (authChallengePolicy, error) {
 var p authChallengePolicy
 if db == nil { return p, authn.ErrForbidden }
 var state string
 var ciphertext []byte
 var keyID sql.NullString
 err := db.QueryRowContext(ctx, "SELECT enabled,site_key,provider_state,secret_ciphertext,secret_key_id FROM admin_turnstile_config WHERE id=1").Scan(&p.Enabled, &p.SiteKey, &state, &ciphertext, &keyID)
 if errors.Is(err, sql.ErrNoRows) { return p, nil } // Same unconfigured default as P17.
 if err != nil { return p, authn.ErrForbidden }
 if !p.Enabled { return p, nil }
 if state != "healthy" || strings.TrimSpace(p.SiteKey) == "" || !keyID.Valid || len(ciphertext) == 0 { return p, authn.ErrForbidden }
 key, err := hex.DecodeString(os.Getenv("GOJET_ADMIN_TOTP_KEY_HEX"))
 if err != nil { return p, authn.ErrForbidden }
 cipher, err := adminaccess.NewSecretCipher(strings.TrimSpace(os.Getenv("GOJET_ADMIN_TOTP_KEY_ID")), key)
 if err != nil { return p, authn.ErrForbidden }
 p.Secret, err = cipher.Decrypt(ciphertext, keyID.String, "admin-turnstile:singleton")
 if err != nil || strings.TrimSpace(p.Secret) == "" { return p, authn.ErrForbidden }
 return p, nil
}

func buildAuthChallengeGate(db *sql.DB, client *redis.Client) (*authChallengeGate, error) {
 replay, err := support.NewRedisSubmissionGuard(client, 1, time.Minute, 10*time.Minute)
 if err != nil { return nil, err }
 return &authChallengeGate{load: func(ctx context.Context) (authChallengePolicy, error) { return loadAuthChallengePolicy(ctx, db) }, replay: replay}, nil
}

func authChallengeProtected(r *http.Request) bool {
 if r.Method != http.MethodPost { return false }
 switch r.URL.Path {
 case "/api/auth/login", "/api/auth/register", "/api/auth/forgotpassword", "/api/auth/resetpassword",
 "/api/public/login-email-code", "/api/public/email-code", "/api/public/register-email-code",
 "/api/auth/verifyemail", "/api/mail/verification", "/api/public/auth/social-registration/complete":
  return true
 }
 return false
}

func (g *authChallengeGate) wrap(next http.Handler) http.Handler {
 return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  if !authChallengeProtected(r) { next.ServeHTTP(w, r); return }
  authn.ApplyPrivateAuthHeaders(w.Header())
  deny := func() { writeAuthProblem(w, http.StatusBadRequest, "turnstile_rejected", "Verification could not be completed. Please try again.") }
  if g == nil || g.load == nil { deny(); return }
  policy, err := g.load(r.Context())
  if err != nil { deny(); return }
  if !policy.Enabled { next.ServeHTTP(w, r); return }
  token := strings.TrimSpace(r.Header.Get("X-Turnstile-Token"))
  if token == "" || len(token) > 2048 { deny(); return }
  verifier, err := support.NewTurnstileHTTPVerifier(policy.Secret, g.client)
  if err != nil { deny(); return }
  guard, err := authn.NewAuthTurnstileGuard(verifier, g.replay)
  if err != nil || guard.Verify(r.Context(), token) != nil { deny(); return }
  next.ServeHTTP(w, r)
 })
}
