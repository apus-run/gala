package redis

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/cache"
	"github.com/apus-run/gala/components/cache/internal/errs"
)

var (
	_ cache.Cache = (*Cache)(nil)
)

type Cache struct {
	client redis.Cmdable
}

func New(client redis.Cmdable) *Cache {
	return &Cache{
		client: client,
	}
}

func (s *Cache) Set(ctx context.Context, key string, val any, exp time.Duration) error {
	return s.client.Set(ctx, key, val, exp).Err()
}

func (s *Cache) Get(ctx context.Context, key string) (any, error) {
	val, err := s.client.Get(ctx, key).Result()
	if err != nil && errors.Is(err, redis.Nil) {
		return nil, errs.ErrKeyNotExist
	}
	return val, err
}

func (s *Cache) GetAny(ctx context.Context, key string) (val cache.Value) {
	val.Value, val.Error = s.client.Get(ctx, key).Result()
	if val.Error != nil && errors.Is(val.Error, redis.Nil) {
		val.Error = errs.ErrKeyNotExist
	}
	return
}

func (s *Cache) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, key).Err()
}

func (s *Cache) Deletes(ctx context.Context, keys ...string) (int64, error) {
	return s.client.Del(ctx, keys...).Result()
}

func (s *Cache) Flush(ctx context.Context) error {
	return s.client.FlushDBAsync(ctx).Err()
}
func (s *Cache) Keys(ctx context.Context) []string {
	return s.client.Keys(ctx, "*").Val()
}

func (s *Cache) Contains(ctx context.Context, key string) bool {
	return s.client.Exists(ctx, key).Val() > 0
}

func (s *Cache) String() string {
	return "redis"
}
