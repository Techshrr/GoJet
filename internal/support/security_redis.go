package support

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

type SubmissionSurface string

const (
	SubmissionPublicContact SubmissionSurface = "public-contact"
	SubmissionTicketCreate  SubmissionSurface = "ticket-create"
	SubmissionTicketReply   SubmissionSurface = "ticket-reply"
)

const supportSubmissionRateScript = `
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return count
`

type RedisSubmissionGuard struct {
	client    *redis.Client
	limit     int64
	window    time.Duration
	replayTTL time.Duration
}

func NewRedisSubmissionGuard(client *redis.Client, limit int64, window, replayTTL time.Duration) (*RedisSubmissionGuard, error) {
	if client == nil || limit <= 0 || window <= 0 || replayTTL <= 0 {
		return nil, ErrInvalidInput
	}
	return &RedisSubmissionGuard{client: client, limit: limit, window: window, replayTTL: replayTTL}, nil
}

// ClaimDigest implements TurnstileReplayStore. Only the SHA-256 token digest
// reaches Redis; the raw Turnstile token is neither part of a key nor a value.
func (g *RedisSubmissionGuard) ClaimDigest(ctx context.Context, digest [32]byte) (bool, error) {
	if g == nil || g.client == nil || digest == ([32]byte{}) {
		return false, ErrInvalidInput
	}
	key := "support:turnstile:replay:" + hex.EncodeToString(digest[:])
	claimed, err := g.client.SetNX(ctx, key, "1", g.replayTTL).Result()
	if err != nil {
		return false, err
	}
	return claimed, nil
}

// AllowSubmission atomically applies the P14 server-side request budget. Redis
// errors are returned with allow=false so callers cannot accidentally fail open.
func (g *RedisSubmissionGuard) AllowSubmission(ctx context.Context, surface SubmissionSurface, remoteAddr string) (bool, error) {
	if g == nil || g.client == nil || !validSubmissionSurface(surface) {
		return false, ErrInvalidInput
	}
	key := submissionRateKey(surface, remoteAddr)
	count, err := g.client.Eval(ctx, supportSubmissionRateScript, []string{key}, g.window.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	return count <= g.limit, nil
}

func validSubmissionSurface(surface SubmissionSurface) bool {
	switch surface {
	case SubmissionPublicContact, SubmissionTicketCreate, SubmissionTicketReply:
		return true
	default:
		return false
	}
}

func submissionRateKey(surface SubmissionSurface, remoteAddr string) string {
	return fmt.Sprintf("support:rate:%s:%s", surface, submissionRateIdentity(remoteAddr))
}

func submissionRateIdentity(remoteAddr string) string {
	value := strings.TrimSpace(remoteAddr)
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	if value == "" {
		value = "unknown"
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}
