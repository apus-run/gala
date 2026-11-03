// Package timeout gives each gin request a context deadline |
// timeout 为每个 gin 请求设置 context 截止时间.
//
// # Notes | 说明
//
// The deadline reaches only handlers that honor their context — the
// middleware cannot cancel a running handler. When the deadline expires and
// nothing has been written yet it answers 504; an already written response
// is left untouched. SetTimeout must be called, otherwise the deadline is
// zero (no timeout).
// 截止时间只对尊重 context 的 handler 生效——中间件无法取消正在执行的
// handler。到期且尚未写出响应时返回 504；已写出的响应保持原样。必须调用
// SetTimeout，否则截止时间为零（即无超时）。
//
// # Example | 示例
//
//	r.Use(timeout.NewBuilder().SetTimeout(3 * time.Second).Build())
package timeout

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Builder assembles the timeout middleware | Builder 组装超时中间件.
type Builder struct {
	timeout time.Duration
}

// NewBuilder creates a Builder | NewBuilder 创建 Builder。
func NewBuilder() *Builder {
	return &Builder{}
}

// SetTimeout sets the per-request deadline | SetTimeout 设置单请求超时。
func (b *Builder) SetTimeout(timeout time.Duration) *Builder {
	b.timeout = timeout
	return b
}

// Build returns the middleware returning 504 once the deadline expires
// | Build 返回超时后返回 504 的中间件。
func (b *Builder) Build() gin.HandlerFunc {
	return func(c *gin.Context) {
		// wrap the request context with a timeout
		// 用带超时的 context 包装请求 context
		ctx, cancel := context.WithTimeout(c.Request.Context(), b.timeout)

		defer func() {
			// cancel to clear resources after finished
			// 结束后 cancel 释放资源
			cancel()

			// The status code cannot change once headers are written, or a
			// JSON blob would be appended after the already-sent body.
			// 已经写出响应头时不能再改状态码，否则会在已发出的响应体后追加一段 JSON
			if ctx.Err() == context.DeadlineExceeded && !c.Writer.Written() {
				log.Println("server timeout:", c.Request.Method, c.Request.URL.Path)

				// write response and abort the request
				// 写响应并中止请求
				c.AbortWithStatusJSON(http.StatusGatewayTimeout, gin.H{
					"code":    504,
					"message": http.StatusText(http.StatusGatewayTimeout),
				})
			}
		}()

		// replace request with context wrapped request
		// 用包装后的 context 替换请求
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// cost.NewBuilder().Build() | 用法示例
