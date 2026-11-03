// Package ctxcache carries a request-scoped cache inside a context.Context
// | ctxcache 在 context.Context 中携带请求级缓存.
//
// # Notes | 说明
//
// [Init] attaches a fresh *sync.Map to the context and returns the context
// unchanged if one is already attached. [Store], [Get] and [HasKey] on a
// context that was never initialized silently drop the write or report a
// miss instead of returning an error. The cache is safe for concurrent use
// but lives only as long as the context; it is not an external cache.
//
// [Init] 向 context 挂载一个新的 *sync.Map，已存在时原样返回。
// 在未经 [Init] 的 context 上调用 [Store]、[Get]、[HasKey] 会静默丢写或未命中，
// 不返回错误。缓存并发安全，但生命周期与 context 相同，不是外部缓存。
package ctxcache

import (
	"context"
	"sync"
)

type ctxCacheKey struct{}

// Init attaches a fresh request-scoped cache to ctx and returns the resulting
// context | Init 向 ctx 挂载一个新的请求级缓存并返回新 context.
//
// If a cache is already attached, ctx is returned unchanged.
// 若已挂载缓存，则原样返回 ctx。
func Init(ctx context.Context) context.Context {
	if val, exist := ctx.Value(ctxCacheKey{}).(*sync.Map); exist && val != nil {
		return ctx
	}
	return context.WithValue(ctx, ctxCacheKey{}, new(sync.Map))
}

// Get returns the value stored under key in the request-scoped cache and
// reports whether a value of type T was found.
// Get 返回请求级缓存中 key 存储的值，并报告是否找到类型为 T 的值。
//
// It returns the zero value and false when the context has no cache, the key
// is missing, or the stored value is not a T.
// context 未挂载缓存、key 不存在或存储值不是 T 时，返回零值和 false。
func Get[T any](ctx context.Context, key any) (value T, ok bool) {
	var zero T

	cacheMap, valid := ctx.Value(ctxCacheKey{}).(*sync.Map)
	if !valid {
		return zero, false
	}

	loadedValue, exists := cacheMap.Load(key)
	if !exists {
		return zero, false
	}

	if v, match := loadedValue.(T); match {
		return v, true
	}

	return zero, false
}

// Store saves obj under key in the request-scoped cache, silently doing
// nothing when the context has no cache | Store 将 obj 存入请求级缓存的
// key 下，context 未挂载缓存时静默跳过.
func Store(ctx context.Context, key any, obj any) {
	if cacheMap, ok := ctx.Value(ctxCacheKey{}).(*sync.Map); ok {
		cacheMap.Store(key, obj)
	}
}

// HasKey reports whether key exists in the request-scoped cache, returning
// false when the context has no cache | HasKey 报告请求级缓存中是否存在 key，
// context 未挂载缓存时返回 false.
func HasKey(ctx context.Context, key any) bool {
	if cacheMap, ok := ctx.Value(ctxCacheKey{}).(*sync.Map); ok {
		_, ok := cacheMap.Load(key)
		return ok
	}

	return false
}
