// Package redis provides a redis-backed implementation of [cache.Cache] |
// redis 提供基于 redis 的 [cache.Cache] 实现.
//
// # Notes | 说明
//
// Values reach go-redis unserialized: strings and numbers round-trip, while
// structs, maps and slices must be serialized by the caller first. Flush
// runs FLUSHDB ASYNC and also drops keys not written through this Cache;
// Keys walks the keyspace with SCAN and inherits its semantics.
// 值不经序列化直接交给 go-redis：字符串与数字可原样往返，结构体、map、切片
// 需调用方自行序列化。Flush 执行 FLUSHDB ASYNC，会连带清掉不是经本 Cache 写入
// 的 key；Keys 用 SCAN 遍历，遵循 SCAN 语义。
//
// # Example | 示例
//
//	c := redis.New(rdb)
//	_ = c.Set(ctx, "k", "v", time.Minute)
//	v, err := c.Get(ctx, "k")
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

// Cache is a cache.Cache implementation backed by redis | Cache 是基于 redis 的缓存实现。
type Cache struct {
	client redis.Cmdable
}

// New creates a redis cache on top of the given client | New 基于给定客户端创建 redis 缓存。
func New(client redis.Cmdable) *Cache {
	return &Cache{
		client: client,
	}
}

// Set stores a value with an expiry | Set 写入缓存（带过期时间）。
func (s *Cache) Set(ctx context.Context, key string, val any, exp time.Duration) error {
	return s.client.Set(ctx, key, val, exp).Err()
}

// Get returns the value for key; a miss maps redis.Nil to ErrKeyNotExist |
// Get 读取缓存，未命中把 redis.Nil 转成 ErrKeyNotExist。
func (s *Cache) Get(ctx context.Context, key string) (any, error) {
	val, err := s.client.Get(ctx, key).Result()
	if err != nil && errors.Is(err, redis.Nil) {
		return nil, errs.ErrKeyNotExist
	}
	return val, err
}

// GetAny returns the wrapped value, with redis.Nil mapped like Get |
// GetAny 获取封装后的值，未命中映射与 Get 一致。
func (s *Cache) GetAny(ctx context.Context, key string) (val cache.Value) {
	val.Value, val.Error = s.client.Get(ctx, key).Result()
	if val.Error != nil && errors.Is(val.Error, redis.Nil) {
		val.Error = errs.ErrKeyNotExist
	}
	return
}

// Delete removes the given key | Delete 删除指定键。
func (s *Cache) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, key).Err()
}

// Deletes removes keys in batch, returning how many were removed | Deletes 批量删除，
// 返回实际删除个数。
func (s *Cache) Deletes(ctx context.Context, keys ...string) (int64, error) {
	// DEL without keys is rejected by redis; return 0 to match memory and lru
	// DEL 不带 key 会被 redis 拒绝；与 memory、lru 一致，返回 0
	if len(keys) == 0 {
		return 0, nil
	}
	return s.client.Del(ctx, keys...).Result()
}

// Flush clears the current redis database (FLUSHDB ASYNC), including keys not
// written through this Cache. Do not call it when the database is shared with
// other services.
// Flush 清空当前 redis 库（FLUSHDB ASYNC），包括不是经由本 Cache 写入的 key。
// 与其他业务共用一个库时不要调用。
func (s *Cache) Flush(ctx context.Context) error {
	return s.client.FlushDBAsync(ctx).Err()
}

// Keys walks the keyspace with SCAN: KEYS * is a single O(N) command that
// blocks the whole redis while it runs. The result may include keys added
// during the walk and miss keys deleted during it (SCAN semantics).
// Keys 用 SCAN 遍历：KEYS * 是 O(N) 的单条命令，执行期间阻塞整个 redis。
// 结果可能包含遍历期间新增的 key，也可能漏掉被删除的 key（SCAN 的语义）。
func (s *Cache) Keys(ctx context.Context) []string {
	var keys []string
	iter := s.client.Scan(ctx, 0, "*", 1000).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if iter.Err() != nil {
		return nil
	}
	return keys
}

// Contains reports whether the key exists | Contains 检查键是否存在。
func (s *Cache) Contains(ctx context.Context, key string) bool {
	return s.client.Exists(ctx, key).Val() > 0
}

// String describes the cache | String 缓存状态描述。
func (s *Cache) String() string {
	return "redis"
}
