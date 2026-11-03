// Package cache defines the [Cache] interface that the memory, lru and redis
// subpackages implement | 定义 [Cache] 接口，由 memory、lru、redis 子包实现。
//
// # Notes | 说明
//
// The [Cache] comment is the contract: exp<=0 never expires, Get reports
// misses and expiry as errors, and Deletes is atomic per key — one-time
// checks such as captcha rely on that. In-memory TTLs are second-granular;
// the redis implementation stores strings and numbers only, without
// serialization. Generic v2 variants of memory and lru live alongside.
// [Cache] 的注释即契约：exp<=0 永不过期，Get 以错误区分未命中与过期，
// Deletes 对单个 key 原子——一次性校验（如 captcha）依赖这一点。内存实现的
// TTL 粒度是秒；redis 实现只存字符串和数字、不做序列化。memory 与 lru 的
// 泛型 v2 版本并存于各自子目录。
//
// # Example | 示例
//
//	c := memory.New()
//	_ = c.Set(ctx, "k", "v", time.Minute)
//	v, err := c.Get(ctx, "k")
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/apus-run/gala/pkg/lang/value"

	"github.com/apus-run/gala/components/cache/internal/errs"
)

// Cache is the interface that the memory, lru and redis implementations all
// follow. Contract:
//
//   - exp<=0 means never expires. In-memory implementations keep second-granular
//     expiry timestamps: exp is rounded up to whole seconds, so the real lifetime
//     floats inside (⌈exp⌉-1s, ⌈exp⌉]; sub-second-to-few-seconds precise TTLs
//     cannot be relied on — use redis when a precise short TTL is needed.
//   - Get returns ErrKeyNotExist on a miss and ErrItemExpired on an expired item.
//   - Delete and Deletes do not error on missing keys; Deletes returns how many
//     keys were actually deleted and is atomic per key: on concurrent Deletes of
//     the same key only one call gets 1 (captcha's one-time check relies on this).
//   - Keys returns nil when there is no valid key.
//   - Flush clears the whole cache; the redis implementation flushes the current
//     redis database, including keys not written through this Cache.
//   - The redis implementation suits strings and numbers only, with no
//     serialization: numbers and booleans come back as strings, and writing a
//     struct, map or slice errors (serialize to string or []byte yourself);
//     memory and lru return the original stored value.
//   - memory and lru Set returns ErrSetKeyFailed for an empty key or nil value.
//
// Cache 是 memory、lru、redis 三个实现共同遵守的接口。语义约定：
//
//   - exp<=0 表示永不过期。内存实现的过期时间戳是秒级的，exp 先向上取整到秒，
//     实际寿命在 (⌈exp⌉-1s, ⌈exp⌉] 之间浮动，不能依赖亚秒到几秒的精确 TTL；
//     需要精确短 TTL 时用 redis。
//   - Get 未命中返回 ErrKeyNotExist，已过期返回 ErrItemExpired。
//   - Delete 与 Deletes 对不存在的 key 不报错，Deletes 返回实际删除的个数，
//     且是原子的：同一个 key 并发删除只有一个调用得到 1（captcha 的一次性校验依赖这一点）。
//   - Keys 没有有效 key 时返回 nil。
//   - Flush 清空整个缓存；redis 实现清空的是当前 redis 库，包括不是经由本 Cache 写入的 key。
//   - redis 实现只适合字符串和数字，不做序列化：数字和布尔取回来是字符串，
//     struct、map、slice 写入时报错（需要自己序列化成 string 或 []byte）；
//     memory、lru 取回的是存入的原值。
//   - memory 与 lru 的 Set 对空 key 或 nil 值返回 ErrSetKeyFailed。
type Cache interface {
	Set(ctx context.Context, key string, val any, exp time.Duration) error
	Get(ctx context.Context, key string) (any, error)
	GetAny(ctx context.Context, key string) Value
	Delete(ctx context.Context, key string) error
	Deletes(ctx context.Context, keys ...string) (int64, error)
	Flush(ctx context.Context) error
	Keys(ctx context.Context) []string
	Contains(ctx context.Context, key string) bool
	String() string
}

// Value wraps one value read out of the cache | 代表一个从缓存中读取出来的值。
type Value struct {
	value.AnyValue
}

// KeyNotFound reports whether the read failed because the key was missing |
// KeyNotFound 表示读取是否因 key 不存在而失败。
func (v Value) KeyNotFound() bool {
	return errors.Is(v.Error, errs.ErrKeyNotExist)
}
