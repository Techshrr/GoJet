package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	SessionCookieName = "__Host-gojet_session"
	CSRFHeaderName    = "X-CSRF-Token"
	csrfPrefix        = "gcf_"
)

type DigestReplayStore interface {
	ClaimDigest(context.Context, [32]byte) (bool, error)
}

func ApplyPrivateAuthHeaders(header http.Header) {
	header.Set("Cache-Control", "no-store, private")
	header.Set("Pragma", "no-cache")
	header.Set("X-Robots-Tag", "noindex, nofollow")
}

func NewSessionCookie(rawToken string, expiresAt time.Time) (*http.Cookie, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" || !expiresAt.After(time.Now().UTC().Add(-time.Minute)) {
		return nil, ErrInvalid
	}
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    rawToken,
		Path:     "/",
		Expires:  expiresAt.UTC(),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}, nil
}

func AuthenticateRequest(ctx context.Context, store *Store, request *http.Request, now time.Time) (Session, error) {
	if store == nil || request == nil {
		return Session{}, ErrUnauthorized
	}
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return Session{}, ErrUnauthorized
	}
	return store.GetSessionByToken(ctx, cookie.Value, now)
}

type OriginPolicy struct {
	allowed map[string]struct{}
}

func NewOriginPolicy(origins ...string) (*OriginPolicy, error) {
	if len(origins) == 0 {
		return nil, ErrInvalid
	}
	allowed := make(map[string]struct{}, len(origins))
	for _, raw := range origins {
		normalized, err := normalizeOrigin(raw)
		if err != nil {
			return nil, ErrInvalid
		}
		allowed[normalized] = struct{}{}
	}
	return &OriginPolicy{allowed: allowed}, nil
}

func (p *OriginPolicy) ValidateUnsafe(request *http.Request) error {
	if request == nil || p == nil || len(p.allowed) == 0 {
		return ErrForbidden
	}
	switch request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	}
	normalized, err := normalizeOrigin(request.Header.Get("Origin"))
	if err != nil {
		return ErrForbidden
	}
	if _, ok := p.allowed[normalized]; !ok {
		return ErrForbidden
	}
	return nil
}

func normalizeOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", ErrInvalid
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && scheme != "http" {
		return "", ErrInvalid
	}
	return scheme + "://" + strings.ToLower(parsed.Host), nil
}

type CSRFManager struct {
	secret [32]byte
	ttl    time.Duration
	replay DigestReplayStore
}

func NewCSRFManager(secret []byte, ttl time.Duration, replay DigestReplayStore) (*CSRFManager, error) {
	if len(secret) != 32 || ttl < time.Minute || ttl > time.Hour || replay == nil {
		return nil, ErrInvalid
	}
	var fixed [32]byte
	copy(fixed[:], secret)
	return &CSRFManager{secret: fixed, ttl: ttl, replay: replay}, nil
}

func (m *CSRFManager) Issue(sessionID string, now time.Time) (string, error) {
	if m == nil || strings.TrimSpace(sessionID) == "" {
		return "", ErrInvalid
	}
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	expires := now.UTC().Add(m.ttl).Unix()
	payload := strings.TrimSpace(sessionID) + "|" + strconv.FormatInt(expires, 10) + "|" + base64.RawURLEncoding.EncodeToString(nonce)
	mac := hmac.New(sha256.New, m.secret[:])
	_, _ = mac.Write([]byte(payload))
	signature := mac.Sum(nil)
	return csrfPrefix + base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (m *CSRFManager) ValidateAndClaim(ctx context.Context, sessionID, raw string, now time.Time) error {
	if m == nil || m.replay == nil || strings.TrimSpace(sessionID) == "" {
		return ErrForbidden
	}
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, csrfPrefix) {
		return ErrForbidden
	}
	parts := strings.Split(strings.TrimPrefix(raw, csrfPrefix), ".")
	if len(parts) != 2 {
		return ErrForbidden
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ErrForbidden
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(signature) != sha256.Size {
		return ErrForbidden
	}
	mac := hmac.New(sha256.New, m.secret[:])
	_, _ = mac.Write(payloadBytes)
	expected := mac.Sum(nil)
	if subtle.ConstantTimeCompare(signature, expected) != 1 {
		return ErrForbidden
	}
	payload := strings.Split(string(payloadBytes), "|")
	if len(payload) != 3 || payload[0] != strings.TrimSpace(sessionID) {
		return ErrForbidden
	}
	expires, err := strconv.ParseInt(payload[1], 10, 64)
	if err != nil || !time.Unix(expires, 0).After(now.UTC()) {
		return ErrExpired
	}
	if _, err := base64.RawURLEncoding.DecodeString(payload[2]); err != nil {
		return ErrForbidden
	}
	digest := sha256.Sum256([]byte(raw))
	claimed, err := m.replay.ClaimDigest(ctx, digest)
	if err != nil {
		return err
	}
	if !claimed {
		return ErrReplay
	}
	return nil
}

func AuthorizeUnsafeRequest(ctx context.Context, request *http.Request, session Session, origins *OriginPolicy, csrf *CSRFManager, now time.Time) error {
	if request == nil || session.ID == "" || session.Status != SessionStatusActive || !session.ExpiresAt.After(now.UTC()) {
		return ErrUnauthorized
	}
	if err := origins.ValidateUnsafe(request); err != nil {
		return err
	}
	if err := csrf.ValidateAndClaim(ctx, session.ID, request.Header.Get(CSRFHeaderName), now); err != nil {
		if errors.Is(err, ErrExpired) || errors.Is(err, ErrReplay) {
			return err
		}
		return ErrForbidden
	}
	return nil
}
