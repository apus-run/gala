package cache

import (
	"context"
	"errors"
	"time"

	"github.com/apus-run/gala/pkg/lang/value"

	"github.com/apus-run/gala/components/cache/internal/errs"
)

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

// Value 代表一个从缓存中读取出来的值
type Value struct {
	value.AnyValue
}

func (v Value) KeyNotFound() bool {
	return errors.Is(v.Error, errs.ErrKeyNotExist)
}
