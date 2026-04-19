package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

func ConnectRedis(ctx context.Context, addr string) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis: ping: %w", err)
	}
	return rdb, nil
}

const blockedKeyPrefix = "jwt:blocked:"

func IsBlocked(ctx context.Context, rdb *redis.Client, jti string) (bool, error) {
	n, err := rdb.Exists(ctx, blockedKeyPrefix+jti).Result()
	return n > 0, err
}

func Block(ctx context.Context, rdb *redis.Client, jti string, ttl time.Duration) error {
	return rdb.Set(ctx, blockedKeyPrefix+jti, 1, ttl).Err()
}
