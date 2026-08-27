package admin

import (
	"context"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLoginLimiter is the production fail-closed administrator login rate authority.
// The caller supplies a bounded namespace so tests and deployments cannot collide.
type RedisLoginLimiter struct {
	client *redis.Client
	prefix string
	limit  int64
	window time.Duration
}

func NewRedisLoginLimiter(client *redis.Client, prefix string, limit int64, window time.Duration) (*RedisLoginLimiter, error) {
	prefix = strings.TrimSpace(prefix)
	if client == nil || prefix == "" || limit < 1 || limit > 1000 || window < time.Minute || window > time.Hour {
		return nil, ErrInvalid
	}
	return &RedisLoginLimiter{client: client, prefix: prefix, limit: limit, window: window}, nil
}

func (l *RedisLoginLimiter) Allow(ctx context.Context, key string) (bool, error) {
	if l == nil || l.client == nil || strings.TrimSpace(key) == "" {
		return false, ErrInvalid
	}
	redisKey := l.prefix + key
	count, err := l.client.Incr(ctx, redisKey).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		if err := l.client.Expire(ctx, redisKey, l.window).Err(); err != nil {
			_ = l.client.Del(ctx, redisKey).Err()
			return false, err
		}
	}
	return count <= l.limit, nil
}

func (l *RedisLoginLimiter) Reset(ctx context.Context, key string) error {
	if l == nil || l.client == nil || strings.TrimSpace(key) == "" {
		return ErrInvalid
	}
	return l.client.Del(ctx, l.prefix+key).Err()
}
