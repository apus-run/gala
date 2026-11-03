// Package jsoncache stores Go values in Redis as JSON | 以 JSON 形式把 Go 值存入 Redis.
//
// # Notes | 说明
//
// A minimal generic wrapper around go-redis: Save always writes without
// expiration (there is no TTL option), Get returns the zero value of T on a
// cache miss, so a stored zero value is indistinguishable from a miss, and
// every key is prefixed at construction. It takes a single *redis.Client.
// go-redis 的极简泛型包装：Save 永不过期（没有 TTL 选项）；Get 未命中时
// 返回 T 的零值，因此无法与存入的零值区分；所有 key 在构造时统一加前缀。
// 只接受单个 *redis.Client。
//
// # Features | 功能
//
// - Generic JsonCache[T] for any JSON-serializable type | 任意可 JSON 序列化类型的泛型缓存
// - Save/Get/Delete under a shared key prefix | 统一 key 前缀下的存取删
//
// # Example | 示例
//
//	c := jsoncache.New[User]("user:", rdb)
//	_ = c.Save(ctx, "42", &User{ID: 42})
//	u, err := c.Get(ctx, "42")
package jsoncache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// JsonCache stores values of T in Redis under a shared key prefix
// | JsonCache 在统一 key 前缀下把 T 的值存入 Redis.
type JsonCache[T any] struct {
	cache  *redis.Client
	prefix string
}

// New creates a JsonCache[T] with the given key prefix | New 以给定 key 前缀创建 JsonCache[T]。
func New[T any](prefix string, cache *redis.Client) *JsonCache[T] {
	return &JsonCache[T]{
		prefix: prefix,
		cache:  cache,
	}
}

// Save marshals v to JSON and stores it without expiration
// | Save 把 v 序列化为 JSON 并写入 Redis，永不过期。
func (g *JsonCache[T]) Save(ctx context.Context, k string, v *T) error {
	if v == nil {
		return fmt.Errorf("cannot save nil value for key: %s", k)
	}

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal failed for type %T: %w", *v, err)
	}

	key := g.prefix + k
	if err := g.cache.Set(ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("redis set failed for key %s: %w", k, err)
	}
	return nil
}

// Get returns default T if key not found
// Get 未命中 key 时返回 T 的零值。
func (g *JsonCache[T]) Get(ctx context.Context, k string) (*T, error) {
	key := g.prefix + k
	var obj T

	data, err := g.cache.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return &obj, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", k, err)
	}

	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal json for key %s: %w", k, err)
	}
	return &obj, nil
}

// Delete removes the key | Delete 删除 key。
func (g *JsonCache[T]) Delete(ctx context.Context, k string) error {
	// Save/Get both carry the prefix, so Delete must too: without it another
	// key is deleted and the original can never be removed.
	// Save/Get 都带 prefix，Delete 也必须带：不带的话删的是别的 key，原 key 永远删不掉
	if err := g.cache.Del(ctx, g.prefix+k).Err(); err != nil {
		return fmt.Errorf("failed to delete key %s: %w", k, err)
	}
	return nil
}
