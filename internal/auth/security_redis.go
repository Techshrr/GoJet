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
	AuthRateRegister  AuthRateSurface = "register"
	AuthRateLogin     AuthRateSurface = "login"
	AuthRateEmailCode AuthRateSurface = "login-email-code"
	AuthRateRecovery  AuthRateSurface = "recovery"
)

const authRateScript = `
local identity_count = redis.call('INCR', KEYS[1])
if identity_count == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
local identity_ttl = redis.call('PTTL', KEYS[1])
local ip_count = redis.call('INCR', KEYS[2])
if ip_count == 1 then redis.call('PEXPIRE', KEYS[2], ARGV[1]) end
local ip_ttl = redis.call('PTTL', KEYS[2])
return {identity_count, identity_ttl, ip_count, ip_ttl}
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
	Allowed       bool
	RetryAfter    time.Duration
	Count         int64
	IdentityCount int64
	IPCount       int64
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
	identityKey, ipKey := authRateKeys(surface, identity, remoteAddr)
	values, err := l.client.Eval(ctx, authRateScript, []string{identityKey, ipKey}, l.window.Milliseconds()).Slice()
	if err != nil || len(values) != 4 {
		return RateDecision{Allowed: false}, err
	}
	identityCount, ok1 := redisInt64(values[0])
	identityTTLMS, ok2 := redisInt64(values[1])
	ipCount, ok3 := redisInt64(values[2])
	ipTTLMS, ok4 := redisInt64(values[3])
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return RateDecision{Allowed: false}, ErrInvalid
	}
	if identityTTLMS < 0 {
		identityTTLMS = l.window.Milliseconds()
	}
	if ipTTLMS < 0 {
		ipTTLMS = l.window.Milliseconds()
	}
	retryMS := identityTTLMS
	if ipTTLMS > retryMS {
		retryMS = ipTTLMS
	}
	retry := time.Duration(retryMS) * time.Millisecond
	if retry > l.window {
		retry = l.window
	}
	count := identityCount
	if ipCount > count {
		count = ipCount
	}
	return RateDecision{
		Allowed:       identityCount <= l.limit && ipCount <= l.limit,
		RetryAfter:    retry,
		Count:         count,
		IdentityCount: identityCount,
		IPCount:       ipCount,
	}, nil
}

func validAuthRateSurface(surface AuthRateSurface) bool {
	switch surface {
	case AuthRateRegister, AuthRateLogin, AuthRateEmailCode, AuthRateRecovery:
		return true
	default:
		return false
	}
}

func authRateKeys(surface AuthRateSurface, identity, remoteAddr string) (string, string) {
	identity = strings.ToLower(strings.TrimSpace(identity))
	if identity == "" {
		identity = "anonymous"
	}
	host := strings.TrimSpace(remoteAddr)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	if host == "" {
		host = "unknown"
	}
	identityHash := sha256.Sum256([]byte(identity))
	ipHash := sha256.Sum256([]byte(host))
	return fmt.Sprintf("auth:rate:%s:identity:%s", surface, hex.EncodeToString(identityHash[:16])),
		fmt.Sprintf("auth:rate:%s:ip:%s", surface, hex.EncodeToString(ipHash[:16]))
}

func redisInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	default:
		return 0, false
	}
}
