package captcha

import (
	"context"
	"strings"
	"time"

	"github.com/mojocn/base64Captcha"

	"github.com/apus-run/gala/components/cache"
)

// keyPrefix 把验证码限定在自己的命名空间。否则 Verify(id, ...) 的 id 由调用方传入，
// 在共享的 redis 里可以指向任意 key：clear=true 时会把它删掉。
const keyPrefix = "captcha:"

type cacheStore struct {
	cache      cache.Cache
	expiration time.Duration
}

// NewCacheStore 把 gala 的 cache.Cache 桥接为 base64Captcha.Store。
// 默认 DefaultMemStore 是进程内存，多实例部署/重启即失效；桥到统一的
// cache 后端（memory 或 redis）后验证码答案可跨实例共享。
//
// expiration<=0 时使用 base64Captcha.Expiration（10 分钟）：0 在 memory 与 redis 上
// 都表示永不过期，没人校验的验证码会无限堆积。
func NewCacheStore(c cache.Cache, expiration time.Duration) base64Captcha.Store {
	if expiration <= 0 {
		expiration = base64Captcha.Expiration
	}
	return &cacheStore{cache: c, expiration: expiration}
}

// Set sets the digits for the captcha id.
func (s *cacheStore) Set(id string, value string) error {
	return s.cache.Set(context.Background(), keyPrefix+id, value, s.expiration)
}

// Get returns stored digits for the captcha id. Clear indicates
// whether the captcha must be deleted from the store.
//
// clear=true 时只有真正删掉这条记录的调用才拿到值：读和删是两步，多实例共用 redis 时
// 并发校验会让同一个验证码通过多次。Deletes 在 memory 下持锁、在 redis 下是单条 DEL，
// 返回的删除数是原子的，同一个 id 只会有一个调用拿到 1。
func (s *cacheStore) Get(id string, clear bool) string {
	ctx := context.Background()
	key := keyPrefix + id
	v, err := s.cache.Get(ctx, key)
	if err != nil {
		return ""
	}
	if clear {
		if n, err := s.cache.Deletes(ctx, key); err != nil || n != 1 {
			return ""
		}
	}
	sv, _ := v.(string)
	return sv
}

// Verify captcha's answer directly. 与 base64Captcha 自带的 store 一致：
// 忽略首尾空白、不区分大小写；空 id 或空答案一律失败。
func (s *cacheStore) Verify(id, answer string, clear bool) bool {
	answer = strings.TrimSpace(answer)
	if id == "" || answer == "" {
		return false
	}
	return strings.EqualFold(s.Get(id, clear), answer)
}
