package ginx

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// WrapContext returns a context wrapped by this package
// WrapContext 返回被本包包装后的 Context。
func WrapContext(c *gin.Context) *Context {
	return &Context{
		Context: c,
	}
}

// Handle convert HandlerFunc to gin.HandlerFunc
// Handle 把 HandlerFunc 转换为 gin.HandlerFunc。
func Handle(h HandlerFunc) gin.HandlerFunc {
	return func(ginCtx *gin.Context) {
		c := WrapContext(ginCtx)
		h(c)
	}
}

// ProxyHandle turns a ProxyHandlerFunc into a gin.HandlerFunc: on error or a
// nil proxy it aborts with 502, otherwise it forwards via the reverse proxy
// | ProxyHandle 把 ProxyHandlerFunc 转成 gin.HandlerFunc：出错或代理为空时
// 以 502 中止，否则交给反向代理转发。
func ProxyHandle(fn ProxyHandlerFunc) gin.HandlerFunc {
	return func(ginCtx *gin.Context) {
		c := WrapContext(ginCtx)
		proxy, err := fn(c)

		if err != nil || proxy == nil {
			// Internal errors are not echoed to the client, and the status
			// code must not be 200, otherwise monitoring cannot see failures.
			// 内部错误不回显给客户端，状态码也不能是 200，否则监控看不到失败。
			c.Data(http.StatusBadGateway, "text/plain; charset=utf-8", []byte(http.StatusText(http.StatusBadGateway)))
			c.Abort()
		} else {
			proxy.ServeHTTP(c.Writer, c.Request)
		}
	}
}

type ginKey struct{}

// NewGinContext returns a new Context that carries gin.Context value.
// NewGinContext 返回携带 gin.Context 值的新 context。
func NewGinContext(ctx context.Context, c *gin.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ginKey{}, c)
}

// FromGinContext returns the gin.Context value stored in ctx, if any.
// FromGinContext 取出 ctx 中保存的 gin.Context（如有）。
func FromGinContext(ctx context.Context) (c *gin.Context, ok bool) {
	if ctx == nil {
		return nil, false
	}
	c, ok = ctx.Value(ginKey{}).(*gin.Context)
	return
}
