package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type AuthRateSurface string

const (
	AuthRateRegister AuthRateSurface = "register"
	AuthRateLogin    AuthRateSurface = "login"
	AuthRateEmailCode AuthRateSurface = "login-email-code"
	AuthRateRecovery  AuthRateSurface = "recovery"
)

const authRateScript = `
local count = redis.call('INCR', KEYS[1])
if count == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
local ttl = redis.call('PTTL', KEYS[1])
return {count, ttl}
`

type RedisDigestReplayStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

func NewRedisDigestReplayStore(client *redis.Client, prefix string, ttl time.Duration) (*RedisDigestReplayStore, error) {
	prefix = strings.TrimSpace(prefix)
	if client == nil || prefix == "" || ttl <= 0 || strings.ContainsAny(prefix, " \t\r\n") {
		return nil, ErrInvalid
	}
	return &RedisDigestReplayStore{client: client, prefix: prefix, ttl: ttl}, nil
}

func (s *RedisDigestReplayStore) ClaimDigest(ctx context.Context, digest [32]byte) (bool, error) {
	if s == nil || s.client == nil || digest == ([32]byte{}) {
		return false, ErrInvalid
	}
	key := s.prefix + ":" + hex.EncodeToString(digest[:])
	return s.client.SetNX(ctx, key, "1", s.ttl).Result()
}

type RateDecision struct {
	Allowed    bool
	RetryAfter time.Duration
	Count      int64
}

type RedisAuthRateLimiter struct {
	client *redis.Client
	limit  int64
	window time.Duration
}

func NewRedisAuthRateLimiter(client *redis.Client, limit int64, window time.Duration) (*RedisAuthRateLimiter, error) {
	if client == nil || limit <= 0 || window < time.Second || window > 24*time.Hour {
		return nil, ErrInvalid
	}
	return &RedisAuthRateLimiter{client: client, limit: limit, window: window}, nil
}

func (l *RedisAuthRateLimiter) Allow(ctx context.Context, surface AuthRateSurface, identity, remoteAddr string) (RateDecision, error) {
	if l == nil || l.client == nil || !validAuthRateSurface(surface) {
		return RateDecision{Allowed: false}, ErrInvalid
	}
	key := authRateKey(surface, identity, remoteAddr)
	values, err := l.client.Eval(ctx, authRateScript, []string{key}, l.window.Milliseconds()).Slice()
	if err != nil || len(values) != 2 {
		return RateDecision{Allowed: false}, err
	}
	count, ok1 := values[0].(int64)
	ttlMS, ok2 := values[1].(int64)
	if !ok1 || !ok2 {
		return RateDecision{Allowed: false}, ErrInvalid
	}
	if ttlMS < 0 {
		ttlMS = l.window.Milliseconds()
	}
	retry := time.Duration(ttlMS) * time.Millisecond
	if retry > l.window {
		retry = l.window
	}
	decision := RateDecision{Allowed: count <= l.limit, RetryAfter: retry, Count: count}
	return decision, nil
}

func validAuthRateSurface(surface AuthRateSurface) bool {
	switch surface {
	case AuthRateRegister, AuthRateLogin, AuthRateEmailCode, AuthRateRecovery:
		return true
	default:
		return false
	}
}

func authRateKey(surface AuthRateSurface, identity, remoteAddr string) string {
	identity = strings.ToLower(strings.TrimSpace(identity))
	host := strings.TrimSpace(remoteAddr)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	if host == "" {
		host = "unknown"
	}
	sum := sha256.Sum256([]byte(identity + "\x00" + host))
	return fmt.Sprintf("auth:rate:%s:%s", surface, hex.EncodeToString(sum[:16]))
}
