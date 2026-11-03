// Package secure wraps github.com/unrolled/secure as a gin middleware |
// secure 把 github.com/unrolled/secure 包装成 gin 中间件.
//
// # Notes | 说明
//
// Defaults enable SSL redirect to SSLHost 127.0.0.1:443; the rest of the
// behavior (HSTS, frame options, content security policy ...) comes from
// secure.Options via WithSecureOptions. Any redirect or rejection aborts
// the chain.
// 默认开启 SSL 重定向到 SSLHost 127.0.0.1:443；其余行为（HSTS、frame options、
// content security policy 等）经 WithSecureOptions 传入 secure.Options 配置。
// 重定向或拒绝都会中止链。
//
// # Example | 示例
//
//	r.Use(secure.NewBuilder().
//		WithSecureOptions(&secure.Options{SSLRedirect: true, SSLHost: "example.com"}).
//		Build())
package secure

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/unrolled/secure"
)

// Builder assembles the secure middleware | Builder 组装安全中间件.
type Builder struct {
	*secure.Options
}

// WithSecureOptions sets the underlying secure options | WithSecureOptions 设置底层 secure 选项。
func (b *Builder) WithSecureOptions(opts *secure.Options) *Builder {
	b.Options = opts
	return b
}

// NewBuilder creates a Builder with SSL redirect defaults | NewBuilder 按默认 SSL 重定向创建 Builder。
func NewBuilder() *Builder {
	return &Builder{
		Options: &secure.Options{
			SSLRedirect: true,
			SSLHost:     "127.0.0.1:443",
		},
	}
}

// Build returns the secure middleware | Build 返回安全中间件。
func (b *Builder) Build() gin.HandlerFunc {
	return func(c *gin.Context) {
		middleware := secure.New(*b.Options)
		err := middleware.Process(c.Writer, c.Request)
		if err != nil {
			// Process already wrote the redirect or error response; the chain
			// must be aborted or the business handler would still run.
			// Process 已经写出了重定向或错误响应，必须中止链，否则后面的业务 handler 仍会执行
			log.Println(err)
			c.Abort()
			return
		}
		// Redirect-style responses carry no error but still need the abort.
		// 重定向类响应不带 error，也要中止
		if c.Writer.Written() {
			c.Abort()
			return
		}
		c.Next()
	}
}
