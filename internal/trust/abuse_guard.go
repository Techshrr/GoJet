package trust

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const abuseRateScript = `
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return count
`

type AbuseSubmissionGuard struct {
	client    *redis.Client
	limit     int64
	window    time.Duration
	replayTTL time.Duration
}

func NewAbuseSubmissionGuard(client *redis.Client, limit int64, window, replayTTL time.Duration) (*AbuseSubmissionGuard, error) {
	if client == nil || limit <= 0 || window <= 0 || replayTTL <= 0 {
		return nil, ErrInvalid
	}
	return &AbuseSubmissionGuard{client: client, limit: limit, window: window, replayTTL: replayTTL}, nil
}

// ClaimDigest satisfies support.TurnstileReplayStore without importing the
// support package. Only the caller-computed SHA-256 token digest reaches Redis.
func (g *AbuseSubmissionGuard) ClaimDigest(ctx context.Context, digest [32]byte) (bool, error) {
	if g == nil || g.client == nil || digest == ([32]byte{}) {
		return false, ErrInvalid
	}
	key := "p16:abuse:turnstile:" + hex.EncodeToString(digest[:])
	claimed, err := g.client.SetNX(ctx, key, "1", g.replayTTL).Result()
	if err != nil {
		return false, err
	}
	return claimed, nil
}

func (g *AbuseSubmissionGuard) AllowSubmission(ctx context.Context, remoteAddr string) (bool, error) {
	if g == nil || g.client == nil {
		return false, ErrInvalid
	}
	identity := strings.TrimSpace(remoteAddr)
	if host, _, err := net.SplitHostPort(identity); err == nil {
		identity = host
	}
	if identity == "" {
		identity = "unknown"
	}
	sum := sha256.Sum256([]byte(identity))
	key := "p16:abuse:rate:" + hex.EncodeToString(sum[:16])
	count, err := g.client.Eval(ctx, abuseRateScript, []string{key}, g.window.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	return count <= g.limit, nil
}
