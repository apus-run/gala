package secure

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/unrolled/secure"
)

type Builder struct {
	*secure.Options
}

func (b *Builder) WithSecureOptions(opts *secure.Options) *Builder {
	b.Options = opts
	return b
}

func NewBuilder() *Builder {
	return &Builder{
		Options: &secure.Options{
			SSLRedirect: true,
			SSLHost:     "127.0.0.1:443",
		},
	}
}

func (b *Builder) Build() gin.HandlerFunc {
	return func(c *gin.Context) {
		middleware := secure.New(*b.Options)
		err := middleware.Process(c.Writer, c.Request)
		if err != nil {
			// Process 已经写出了重定向或错误响应，必须中止链，否则后面的业务 handler 仍会执行
			log.Println(err)
			c.Abort()
			return
		}
		// 重定向类响应不带 error，也要中止
		if c.Writer.Written() {
			c.Abort()
			return
		}
		c.Next()
	}
}
