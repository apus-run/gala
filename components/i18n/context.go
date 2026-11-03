package i18n

import (
	"context"
)

type translator struct{}

// NewContext stores the I18n translator into ctx | NewContext 把 I18n 翻译器存入 ctx。
func NewContext(ctx context.Context, i *I18n) context.Context {
	return context.WithValue(ctx, translator{}, i)
}

// FromContext returns the I18n stored in ctx, or a default instance when absent
// | FromContext 取出 ctx 中的 I18n，缺失时返回默认实例。
func FromContext(ctx context.Context) *I18n {
	if i, ok := ctx.Value(translator{}).(*I18n); ok {
		return i
	}

	return New()
}
