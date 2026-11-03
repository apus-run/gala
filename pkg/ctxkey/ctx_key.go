// Package ctxkey defines collision-free, typed keys for context values
// | ctxkey 定义无冲突、带类型的 context 值键.
//
// # Notes | 说明
//
// Each call to [NewContextKey] returns a distinct key, identified by its own
// pointer, even for the same value type T. Create a key once (for example in
// a package-level variable) and share it; two independently created keys
// never see each other's values. [FromContext] reports whether the key is
// present.
//
// 每次 [NewContextKey] 都返回一个独立键，以各自指针地址区分唯一性，
// 即使值类型 T 相同也是如此。应创建一次（如包级变量）后共享使用；
// 独立创建的两个键互相不可见。[FromContext] 返回键是否存在。
package ctxkey

import (
	"context"
)

// A ContextKey is a unique, typed key for a value stored in a context
// | ContextKey 是 context 中存储值的唯一、带类型键.
//
// Two keys are distinct even when created for the same type T.
// 即使为同一类型 T 创建，两个键也互不相同。
type ContextKey[T any] struct {
	// key is the pointer that gives this key its uniqueness | 以指针地址保证键的唯一性.
	key *int
}

// NewContextKey returns a context key for a value of type T | NewContextKey 返回
// 类型 T 的值的 context 键.
//
// Each call returns a distinct key; create once and share.
// 每次调用返回一个新键；应创建一次后共享使用。
func NewContextKey[T any]() *ContextKey[T] {
	return &ContextKey[T]{key: new(int)}
}

// NewContext returns ctx augmented with this key and the given value
// | NewContext 返回携带此键与给定值的 ctx.
func (k *ContextKey[T]) NewContext(ctx context.Context, v T) context.Context {
	return context.WithValue(ctx, k.key, v)
}

// FromContext returns the value associated with this key in the context and
// reports whether it was present | FromContext 返回此键在 context 中关联的值，
// 并报告该键是否存在.
//
// The value is the zero value for T when the key is absent.
// 键不存在时返回 T 的零值。
func (k *ContextKey[T]) FromContext(ctx context.Context) (v T, ok bool) {
	v, ok = ctx.Value(k.key).(T)
	return
}
