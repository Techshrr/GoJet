package links

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	linkPasswordAttemptLimit  = 10
	linkPasswordAttemptWindow = 5 * time.Minute
)

const passwordAttemptScript = `
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return count
`

func passwordAttemptIdentity(remoteAddr string) string {
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

func passwordAttemptKey(linkID uint64, remoteAddr string) string {
	return fmt.Sprintf("access:password:%d:%s", linkID, passwordAttemptIdentity(remoteAddr))
}

// AllowPasswordAttempt atomically counts public password guesses without
// persisting raw client addresses in Redis keys. Errors must fail closed.
func (s *RedisRiskStore) AllowPasswordAttempt(ctx context.Context, linkID uint64, remoteAddr string) (bool, error) {
	if linkID == 0 {
		return false, ErrInvalidInput
	}
	value, err := s.client.Eval(ctx, passwordAttemptScript, []string{passwordAttemptKey(linkID, remoteAddr)}, linkPasswordAttemptWindow.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	return value <= linkPasswordAttemptLimit, nil
}

func (s *RedisRiskStore) ClearPasswordAttempts(ctx context.Context, linkID uint64, remoteAddr string) error {
	if linkID == 0 {
		return ErrInvalidInput
	}
	return s.client.Del(ctx, passwordAttemptKey(linkID, remoteAddr)).Err()
}
