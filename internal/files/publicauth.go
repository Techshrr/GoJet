package files

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const (
	publicAuthCookieName = "gojet_file_auth"
	publicAuthTTL        = 15 * time.Minute
)

type publicAuthPayload struct {
	Slug        string `json:"s"`
	HashBinding string `json:"h"`
	Expires     int64  `json:"e"`
}

func passwordHashBinding(passwordHash string) string {
	sum := sha256.Sum256([]byte(passwordHash))
	return hex.EncodeToString(sum[:])
}

func (a *API) signPublicAuth(slug, passwordHash string, now time.Time) (string, time.Time, error) {
	if a == nil || len(a.publicAuthSecret) < 32 || strings.TrimSpace(slug) == "" || passwordHash == "" || now.IsZero() {
		return "", time.Time{}, ErrInvalidInput
	}
	expires := now.UTC().Add(publicAuthTTL)
	payload := publicAuthPayload{Slug: slug, HashBinding: passwordHashBinding(passwordHash), Expires: expires.Unix()}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", time.Time{}, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, a.publicAuthSecret)
	_, _ = mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + signature, expires, nil
}

func (a *API) verifyPublicAuthToken(token, slug, passwordHash string, now time.Time) bool {
	if a == nil || len(a.publicAuthSecret) < 32 || token == "" || slug == "" || passwordHash == "" || now.IsZero() {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(provided) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, a.publicAuthSecret)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	var payload publicAuthPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	if payload.Slug != slug || payload.HashBinding != passwordHashBinding(passwordHash) {
		return false
	}
	now = now.UTC()
	if payload.Expires < now.Unix() || payload.Expires > now.Add(publicAuthTTL+time.Minute).Unix() {
		return false
	}
	return true
}

func (a *API) requestHasPublicAuth(r *http.Request, slug, passwordHash string, now time.Time) bool {
	if passwordHash == "" {
		return true
	}
	cookie, err := r.Cookie(publicAuthCookieName)
	if err != nil {
		return false
	}
	return a.verifyPublicAuthToken(cookie.Value, slug, passwordHash, now)
}

func (a *API) setPublicAuthCookie(w http.ResponseWriter, r *http.Request, slug, passwordHash string, now time.Time) error {
	token, expires, err := a.signPublicAuth(slug, passwordHash, now)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: publicAuthCookieName, Value: token, Path: "/", Expires: expires,
		MaxAge: int(publicAuthTTL.Seconds()), HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: !a.testAuthEnabled || requestIsSecure(r),
	})
	return nil
}

func requestIsSecure(r *http.Request) bool {
	if r == nil {
		return false
	}
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func clearPublicAuthCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: publicAuthCookieName, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0),
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secure,
	})
}
