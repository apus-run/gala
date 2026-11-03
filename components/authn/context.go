package authn

import "context"

type ctxKey struct{}

// NewContext returns a child context carrying an authenticated identity.
// Middleware writes the identity only after authentication succeeds.
// NewContext 返回携带已认证身份的子 context。
// 中间件只在认证成功后才写入身份。
func NewContext(ctx context.Context, v any) context.Context {
	return context.WithValue(ctx, ctxKey{}, v)
}

// FromContext returns the authenticated identity stored by NewContext.
// It returns false when no value is stored or when the stored value is not T.
// FromContext 取回 NewContext 存入的已认证身份。
// 没有存值或存值不是 T 时返回 false。
func FromContext[T any](ctx context.Context) (T, bool) {
	var zero T
	if ctx == nil {
		return zero, false
	}
	v, ok := ctx.Value(ctxKey{}).(T)
	return v, ok
}
