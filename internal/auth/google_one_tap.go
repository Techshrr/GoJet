package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const googleCertificatesURL = "https://www.googleapis.com/oauth2/v1/certs"

// GoogleOneTapVerifier authenticates GIS credentials, not OAuth authorization
// codes. The caller must bind nonce to a browser and consume it atomically before
// issuing a session. Verification alone never creates or links an account.
type GoogleOneTapVerifier struct {
	client *http.Client
	mu sync.Mutex
	keys map[string]*rsa.PublicKey
	expires time.Time
}

func NewGoogleOneTapVerifier() *GoogleOneTapVerifier {
	return &GoogleOneTapVerifier{client: &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (v *GoogleOneTapVerifier) Verify(ctx context.Context, credential, clientID, nonce string, now time.Time) (OAuthProviderClaim, error) {
	denied := OAuthProviderClaim{}
	if v == nil || v.client == nil || clientID == "" || nonce == "" || len(credential) > 16384 {
		return denied, ErrForbidden
	}
	parts := strings.Split(credential, ".")
	if len(parts) != 3 { return denied, ErrForbidden }
	var header struct { Alg string `json:"alg"`; Kid string `json:"kid"`; Crit []string `json:"crit"` }
	if decodeGoogleJWTPart(parts[0], &header) != nil || header.Alg != "RS256" || header.Kid == "" || len(header.Crit) != 0 {
		return denied, ErrForbidden
	}
	var claims struct {
		Issuer string `json:"iss"`
		Audience string `json:"aud"`
		AuthorizedParty string `json:"azp"`
		Subject string `json:"sub"`
		Expires int64 `json:"exp"`
		Issued int64 `json:"iat"`
		NotBefore int64 `json:"nbf"`
		Nonce string `json:"nonce"`
		Email string `json:"email"`
		EmailVerified bool `json:"email_verified"`
		HostedDomain string `json:"hd"`
		Name string `json:"name"`
	}
	if decodeGoogleJWTPart(parts[1], &claims) != nil || claims.Audience != clientID ||
		(claims.AuthorizedParty != "" && claims.AuthorizedParty != clientID) ||
		(claims.Issuer != "accounts.google.com" && claims.Issuer != "https://accounts.google.com") ||
		claims.Expires <= now.Unix() || claims.Issued <= 0 || claims.Issued > now.Add(time.Minute).Unix() ||
		claims.Expires <= claims.Issued || claims.NotBefore > now.Unix() ||
		strings.TrimSpace(claims.Subject) == "" || len(claims.Subject) > 255 || len(claims.Name) > 255 ||
		subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(nonce)) != 1 {
		return denied, ErrForbidden
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil { return denied, ErrForbidden }
	key, err := v.key(ctx, header.Kid, now)
	if err != nil { return denied, ErrForbidden }
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) != nil { return denied, ErrForbidden }
	// Google is not authoritative for an arbitrary external email, even when the
	// account's email_verified flag is true. Keep local verification in that case.
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	verified := claims.EmailVerified && (strings.HasSuffix(email, "@gmail.com") || claims.HostedDomain != "")
	return OAuthProviderClaim{Subject: claims.Subject, Email: email, EmailVerified: verified, DisplayName: claims.Name}, nil
}

func decodeGoogleJWTPart(part string, out any) error {
	raw, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil { return err }
	return json.Unmarshal(raw, out)
}

func (v *GoogleOneTapVerifier) key(ctx context.Context, kid string, now time.Time) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if now.Before(v.expires) {
		if key := v.keys[kid]; key != nil { return key, nil }
		// Unknown attacker-controlled key IDs must not force a network request.
		return nil, ErrForbidden
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleCertificatesURL, nil)
	if err != nil { return nil, ErrForbidden }
	response, err := v.client.Do(req)
	if err != nil { return nil, ErrForbidden }
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK { return nil, ErrForbidden }
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1 << 20) + 1))
	if err != nil || len(raw) > 1 << 20 { return nil, ErrForbidden }
	var certificates map[string]string
	if json.Unmarshal(raw, &certificates) != nil || len(certificates) == 0 || len(certificates) > 16 { return nil, ErrForbidden }
	keys := make(map[string]*rsa.PublicKey, len(certificates))
	ttl := time.Minute
	for _, directive := range strings.Split(response.Header.Get("Cache-Control"), ",") {
		name, value, ok := strings.Cut(strings.TrimSpace(directive), "=")
		if ok && strings.EqualFold(name, "max-age") {
			seconds, parseErr := strconv.ParseInt(strings.Trim(value, "\""), 10, 64)
			if parseErr == nil && seconds >= 0 {
				if seconds > 21600 { seconds = 21600 }
				ttl = time.Duration(seconds) * time.Second
			}
		}
	}
	expires := now.Add(ttl)
	for id, encoded := range certificates {
		block, _ := pem.Decode([]byte(encoded))
		if block == nil || block.Type != "CERTIFICATE" { return nil, ErrForbidden }
		certificate, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr != nil || now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) { return nil, ErrForbidden }
		key, ok := certificate.PublicKey.(*rsa.PublicKey)
		if !ok || key.N.BitLen() < 2048 { return nil, ErrForbidden }
		keys[id] = key
		if certificate.NotAfter.Before(expires) { expires = certificate.NotAfter }
	}
	v.keys, v.expires = keys, expires
	if key := keys[kid]; key != nil { return key, nil }
	return nil, ErrForbidden
}
