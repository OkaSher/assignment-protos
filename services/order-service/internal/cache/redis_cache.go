package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisCache struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewRedisCache(rdb *redis.Client, ttl time.Duration) *RedisCache {
	return &RedisCache{rdb: rdb, ttl: ttl}
}

func (c *RedisCache) Get(ctx context.Context, key string) (string, bool, error) {
	v, err := c.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (c *RedisCache) Set(ctx context.Context, key, val string) error {
	return c.rdb.Set(ctx, key, val, c.ttl).Err()
}

func (c *RedisCache) Delete(ctx context.Context, key string) error {
	return c.rdb.Del(ctx, key).Err()
}
