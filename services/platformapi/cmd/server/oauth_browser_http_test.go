package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func oauthBrowserTestMux(h *authHTTPHandler) http.Handler {
	inner := http.NewServeMux()
	h.registerOAuthBrowserRoutes(inner)
	inner.HandleFunc("GET /api/public/auth/providers", h.handleProviders)
	root := http.NewServeMux()
	mountAuthRoutes(root, inner)
	return root
}

func TestOAuthBrowserRejectsInvalidAuthority(t *testing.T) {
	mux := oauthBrowserTestMux(&authHTTPHandler{})
	for _, path := range []string{
		"/api/public/auth/unknown/start", "/api/public/auth/google/start?intent=bind",
		"/api/public/auth/google/start?intent=unexpected",
		"/api/public/auth/google/callback?state=gos_unissued&code=untrusted",
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 400 || len(w.Result().Cookies()) != 0 || w.Header().Get("Location") != "" {
			t.Fatal("invalid authority reached OAuth service")
		}
	}
	for _, value := range []string{"", "gos_other-browser"} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: oauthBrowserCookie("google"), Value: value})
		if validOAuthBrowserCallback(r, "google", "gos_bound") {
			t.Fatal("browser state mismatch accepted")
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: oauthBrowserCookie("google"), Value: "gos_bound"})
	if !validOAuthBrowserCallback(r, "google", "gos_bound") || validOAuthBrowserCallback(r, "facebook", "gos_bound") {
		t.Fatal("provider-scoped browser state binding failed")
	}
}

func TestOAuthBrowserStartPersistence(t *testing.T) {
	if os.Getenv("GOJET_P20_OAUTH_BROWSER_PROBE") != "1" {
		t.Skip("requires isolated OAuth administrator probe database")
	}
	runtime, err := adminfixture.Open()
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer runtime.Close()
	crypto, err := authn.NewOAuthCrypto("p20-fixture", bytes.Repeat([]byte{0x37}, 32))
	if err != nil {
		t.Fatal("fixture crypto unavailable")
	}
	oauth, err := authn.NewOAuthService(runtime.DB, crypto, 10*time.Minute)
	if err != nil {
		t.Fatal("OAuth service unavailable")
	}
	mux := oauthBrowserTestMux(&authHTTPHandler{oauth: oauth, db: runtime.DB})
	discovery := httptest.NewRecorder()
	mux.ServeHTTP(discovery, httptest.NewRequest(http.MethodGet, "/api/public/auth/providers", nil))
	var inventory struct {
		Providers []publicOAuthProvider `json:"providers"`
	}
	if discovery.Code != 200 || json.Unmarshal(discovery.Body.Bytes(), &inventory) != nil || len(inventory.Providers) != 8 {
		t.Fatal("production discovery must expose eight providers")
	}
	for i, provider := range authn.RuntimeProviders() {
		if inventory.Providers[i].Provider != provider {
			t.Fatal("production inventory order or identity incorrect")
		}
	}
	for _, table := range []string{"oauth_states", "oauth_handoffs", "oauth_identities", "oauth_provider_configs"} {
		var columnType string
		if err := runtime.DB.QueryRowContext(context.Background(), "SELECT COLUMN_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME='provider'", table).Scan(&columnType); err != nil || !strings.Contains(columnType, "'x','linkedin'") {
			t.Fatal("optional-provider durable schema incomplete")
		}
	}
	for _, provider := range []string{"google", "x", "linkedin"} {
		for _, intent := range []string{"login", "register"} {
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "https://site.p20.test/api/public/auth/"+provider+"/start?intent="+intent, nil))
			if w.Code != http.StatusFound {
				t.Fatalf("real public start route returned %d", w.Code)
			}
			location, err := url.Parse(w.Header().Get("Location"))
			if err != nil || location.Host != map[string]string{"google": "accounts.google.com", "x": "x.com", "linkedin": "www.linkedin.com"}[provider] || location.Scheme != "https" {
				t.Fatal("provider redirect absent")
			}
			query := location.Query()
			state := query.Get("state")
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Name != oauthBrowserCookie(provider) || cookies[0].Value != state || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" {
				t.Fatal("browser binding cookie missing or unsafe")
			}
			var id, storedIntent, keyID string
			var encrypted []byte
			hash := authn.HashOpaque(state)
			err = runtime.DB.QueryRowContext(context.Background(), "SELECT id,intent,pkce_key_id,pkce_verifier_ciphertext FROM oauth_states WHERE provider=? AND state_hash=? AND consumed_at IS NULL", provider, hash[:]).Scan(&id, &storedIntent, &keyID, &encrypted)
			if err != nil || storedIntent != intent {
				t.Fatal("start did not persist the requested intent")
			}
			verifier, err := crypto.Decrypt(encrypted, keyID, "oauth_pkce:"+id)
			if err != nil {
				t.Fatal("encrypted PKCE authority invalid")
			}
			digest := sha256.Sum256([]byte(verifier))
			if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(digest[:]) {
				t.Fatal("redirect does not bind persisted PKCE verifier")
			}
			if strings.Contains(w.Header().Get("Location")+w.Body.String(), verifier) || query.Has("client_secret") || w.Body.Len() != 0 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
				t.Fatal("public redirect leaked credentials or was cacheable")
			}
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/public/auth/facebook/start", nil))
	if w.Code < 400 || w.Header().Get("Location") != "" || len(w.Result().Cookies()) != 0 {
		t.Fatal("disabled provider issued a redirect or browser state")
	}
}
