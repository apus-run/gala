// Package error maps the last errcode error attached to the gin context into a
// problem+json response | error 把 gin context 上最后一条 errcode 错误映射为
// problem+json 响应.
//
// # Notes | 说明
//
// It runs after c.Next and stays silent when the response is already written.
// The last c.Errors entry goes through errcode.Normalize, so plain errors
// become 500 InternalError and expired or canceled contexts map to 504 and 499.
// The response uses errcode.Respond's Body and Content-Type; 5xx failures are
// logged at Error level, everything else at Warn.
// 它在 c.Next 之后运行：响应已写出时不做任何事。最后一条 c.Errors 交给
// errcode.Normalize 分类——普通错误变成 500 InternalError，超时与取消分别映射为
// 504 与 499。响应使用 errcode.Respond 的 Body 与 Content-Type；5xx 记 Error
// 日志，其余记 Warn。
//
// # Example | 示例
//
//	r.Use(error.NewBuilder().Build())
package error

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/apus-run/gala/pkg/errcode"
)

// Builder assembles the error-mapping middleware | Builder 组装错误映射中间件.
type Builder struct{}

// NewBuilder creates a Builder | NewBuilder 创建 Builder。
func NewBuilder() *Builder {
	return &Builder{}
}

// Build returns the middleware that writes the last error as problem+json
// | Build 返回把最后一条错误写成 problem+json 的中间件。
func (b *Builder) Build() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// Skip when downstream already wrote the response.
		// 下游已经写回响应时，避免重复写入。
		if c.Writer.Written() || len(c.Errors) == 0 {
			return
		}

		appErr := errcode.Normalize(c.Errors.Last().Err)
		if appErr == nil {
			return
		}
		status, body := errcode.Respond(appErr)

		level := slog.LevelWarn
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		slog.Log(c.Request.Context(), level, "request failed",
			"method", c.Request.Method, "path", c.Request.URL.Path, "err", appErr)

		payload, err := json.Marshal(body)
		if err != nil {
			// Body only holds strings and ints, so this cannot happen in practice.
			// Body 只有字符串与整数，实际上不会失败。
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Abort()
		c.Data(status, errcode.ContentType, payload)
	}
}
