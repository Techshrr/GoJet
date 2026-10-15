package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Included in the existing production-adapter CI test selection.
func TestHTTPProviderAdapterGoogleOneTap(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil { t.Fatal(err) }
	now := time.Unix(1790000000, 0)
	verifier := NewGoogleOneTapVerifier()
	verifier.keys = map[string]*rsa.PublicKey{"google-test-key": &key.PublicKey}
	verifier.expires = now.Add(time.Hour)
	verifier.client.Transport = oauthRoundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("untrusted token must not trigger a key refresh while cache is valid")
		return nil, nil
	})
	encode := func(value any) string {
		raw, marshalErr := json.Marshal(value)
		if marshalErr != nil { t.Fatal(marshalErr) }
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	token := func(claims map[string]any, alg, kid string) string {
		unsigned := encode(map[string]any{"alg": alg, "kid": kid}) + "." + encode(claims)
		digest := sha256.Sum256([]byte(unsigned))
		signature, signErr := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if signErr != nil { t.Fatal(signErr) }
		return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	}
	claims := func() map[string]any {
		return map[string]any{"iss": "https://accounts.google.com", "aud": "our-client", "sub": "stable-subject",
			"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": "browser-bound-nonce",
			"email": "owner@gmail.com", "email_verified": true, "name": "Owner"}
	}
	valid := token(claims(), "RS256", "google-test-key")
	claim, err := verifier.Verify(context.Background(), valid, "our-client", "browser-bound-nonce", now)
	if err != nil || claim.Subject != "stable-subject" || !claim.EmailVerified { t.Fatalf("valid signed token rejected: %v", err) }
	for _, test := range []struct { name, field string; value any }{
		{"wrong issuer", "iss", "https://attacker.example"},
		{"wrong audience", "aud", "another-client"},
		{"wrong authorized party", "azp", "another-client"},
		{"wrong nonce", "nonce", "another-browser"},
		{"missing nonce", "nonce", ""},
		{"expired", "exp", now.Unix()},
		{"future issued", "iat", now.Add(2*time.Minute).Unix()},
		{"future not-before", "nbf", now.Add(time.Second).Unix()},
		{"empty subject", "sub", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := claims(); c[test.field] = test.value
			if _, err := verifier.Verify(context.Background(), token(c, "RS256", "google-test-key"), "our-client", "browser-bound-nonce", now); err == nil { t.Fatal("invalid claim accepted") }
		})
	}
	for _, raw := range []string{
		token(claims(), "none", "google-test-key"), token(claims(), "HS256", "google-test-key"),
		token(claims(), "RS256", "unknown-key"), valid + "corrupt", "malformed",
	} {
		if _, err := verifier.Verify(context.Background(), raw, "our-client", "browser-bound-nonce", now); err == nil { t.Fatal("invalid signature/header accepted") }
	}
	c := claims(); c["email"] = "owner@external.example"
	claim, err = verifier.Verify(context.Background(), token(c, "RS256", "google-test-key"), "our-client", "browser-bound-nonce", now)
	if err != nil || claim.EmailVerified { t.Fatal("external email must retain local verification") }
	c["hd"] = "external.example"
	claim, err = verifier.Verify(context.Background(), token(c, "RS256", "google-test-key"), "our-client", "browser-bound-nonce", now)
	if err != nil || !claim.EmailVerified { t.Fatal("verified hosted-domain claim rejected") }
	if _, err := verifier.Verify(context.Background(), valid, "our-client", "", now); err == nil { t.Fatal("missing expected nonce accepted") }
}
