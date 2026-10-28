package authn

import "context"

type ctxKey struct{}

// NewContext returns a child context carrying an authenticated identity.
// Middleware writes the identity only after authentication succeeds.
func NewContext(ctx context.Context, v any) context.Context {
	return context.WithValue(ctx, ctxKey{}, v)
}

// FromContext returns the authenticated identity stored by NewContext.
// It returns false when no value is stored or when the stored value is not T.
func FromContext[T any](ctx context.Context) (T, bool) {
	var zero T
	if ctx == nil {
		return zero, false
	}
	v, ok := ctx.Value(ctxKey{}).(T)
	return v, ok
}
