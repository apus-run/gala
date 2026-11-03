// Package recovery converts gin handler panics into a 500 JSON response |
// recovery 把 gin handler 的 panic 转为 500 JSON 响应.
//
// # Notes | 说明
//
// Panics are logged with the full stack; broken-pipe style client errors
// (reset by peer, unexpected EOF, i/o timeout ...) get a bare 500 without a
// body. The success-path response is fixed JSON — there are no
// customization hooks.
// panic 会连同完整堆栈写入日志；断管类客户端错误（reset by peer、
// unexpected EOF、i/o timeout 等）只回裸 500、不写响应体。响应体是固定 JSON，
// 没有自定义入口。
//
// # Example | 示例
//
//	r.Use(recovery.NewBuilder().Build())
package recovery

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"github.com/gin-gonic/gin"
)

// Builder assembles the panic-recovery middleware | Builder 组装 panic 恢复中间件.
type Builder struct{}

// NewBuilder creates a Builder | NewBuilder 创建 Builder。
func NewBuilder() *Builder {
	return &Builder{}
}

// Build returns the middleware that recovers panics into a 500 JSON response
// | Build 返回把 panic 恢复为 500 JSON 响应的中间件。
func (b *Builder) Build() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// log.Println("error: ", err)
				// log.Println("exec panic error", safemap[string]any{
				// 	"trace_error": string(debug.Stack()),
				// })

				ctx := c.Request.Context()
				log.Println("exec panic error", map[string]any{
					"module":      "web",
					"trace_error": string(debug.Stack()),
				})

				// broker pipe | 断开的管道
				if isBrokenPipe(ctx, err) {
					c.AbortWithStatus(http.StatusInternalServerError)
					return
				}

				// services error | 服务内部错误
				c.AbortWithStatusJSON(http.StatusInternalServerError, map[string]any{
					"code":    http.StatusInternalServerError,
					"message": "server inner error",
				})
				return
			}
		}()

		c.Next()
	}
}

// isBrokenPipe reports whether the panic came from a broken client connection
// | isBrokenPipe 判断 panic 是否源于客户端连接断开。
func isBrokenPipe(ctx context.Context, err any) bool {
	// Check for a broken connection, as it is not really a
	// condition that warrants a panic stack trace.
	// 检查断开的连接：这种情况并不真正需要 panic 堆栈。
	var brokenPipe bool
	if ne, ok := err.(*net.OpError); ok {
		if se, exist := ne.Err.(*os.SyscallError); exist {
			errMsg := strings.ToLower(se.Error())
			// logger error | 记录系统调用错误日志
			log.Println(ctx, "os syscall error", map[string]any{
				"trace_error": errMsg,
			})

			if strings.Contains(errMsg, "broken pipe") ||
				strings.Contains(errMsg, "reset by peer") ||
				strings.Contains(errMsg, "request headers: small read buffer") ||
				strings.Contains(errMsg, "unexpected EOF") ||
				strings.Contains(errMsg, "i/o timeout") {
				brokenPipe = true
			}
		}
	}

	return brokenPipe
}
