package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/redis/go-redis/v9"
)

const authRateUnavailableCode = "auth_rate_unavailable"

type authRateRequestBody struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func buildAuthRateMiddleware(client *redis.Client) (func(http.Handler) http.Handler, error) {
	limit, err := parseAuthRateLimit(os.Getenv("GOJET_AUTH_RATE_LIMIT"))
	if err != nil {
		return nil, err
	}
	window, err := parseAuthRateWindow(os.Getenv("GOJET_AUTH_RATE_WINDOW_SECONDS"))
	if err != nil {
		return nil, err
	}
	limiter, err := authn.NewRedisAuthRateLimiter(client, limit, window)
	if err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		if next == nil {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				authn.ApplyPrivateAuthHeaders(w.Header())
				writeAuthProblem(w, http.StatusServiceUnavailable, authRateUnavailableCode, "The authentication service could not complete the request.")
			})
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			surface, identity, protected := authRateRequest(r)
			if !protected {
				next.ServeHTTP(w, r)
				return
			}
			decision, err := limiter.Allow(r.Context(), surface, identity, r.RemoteAddr)
			if err != nil {
				authn.ApplyPrivateAuthHeaders(w.Header())
				writeAuthProblem(w, http.StatusServiceUnavailable, authRateUnavailableCode, "The authentication service could not complete the request.")
				return
			}
			if !decision.Allowed {
				authn.ApplyPrivateAuthHeaders(w.Header())
				w.Header().Set("Retry-After", strconv.FormatInt(retryAfterSeconds(decision.RetryAfter), 10))
				writeAuthProblem(w, http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again later.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

func parseAuthRateLimit(raw string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, authn.ErrInvalid
	}
	return value, nil
}

func parseAuthRateWindow(raw string) (time.Duration, error) {
	seconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || seconds <= 0 || seconds > int64((24*time.Hour)/time.Second) {
		return 0, authn.ErrInvalid
	}
	return time.Duration(seconds) * time.Second, nil
}

func authRateRequest(r *http.Request) (authn.AuthRateSurface, string, bool) {
	if r == nil || r.Method != http.MethodPost {
		return "", "", false
	}
	var surface authn.AuthRateSurface
	switch r.URL.Path {
	case "/api/auth/register":
		surface = authn.AuthRateRegister
	case "/api/auth/login":
		surface = authn.AuthRateLogin
	case "/api/public/login-email-code":
		surface = authn.AuthRateEmailCode
	case "/api/auth/forgotpassword":
		surface = authn.AuthRateRecovery
	default:
		return "", "", false
	}

	if r.Body == nil {
		return surface, "anonymous", true
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, authHTTPBodyLimit+1))
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil || len(raw) > authHTTPBodyLimit {
		return surface, "anonymous", true
	}
	var body authRateRequestBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return surface, "anonymous", true
	}
	identity := strings.TrimSpace(body.Email)
	if identity == "" && surface == authn.AuthRateEmailCode {
		identity = strings.TrimSpace(body.Code)
	}
	if identity == "" {
		identity = "anonymous"
	}
	return surface, identity, true
}

func retryAfterSeconds(value time.Duration) int64 {
	if value <= 0 {
		return 1
	}
	seconds := int64((value + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}
