// Package helmet demonstrates security response headers with upstream Helmet
// semantics: XSS protection, content-type sniffing protection and friends on
// every response, mounted once engine-wide.
// Package helmet 演示沿用上游 Helmet 语义的安全响应头：在每个响应上附加
// XSS 防护、content-type 嗅探防护等，引擎级挂载一次即可。
package helmet

import (
	"github.com/apus-run/gala/components/ginx/middlewares/helmet"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine with the helmet middleware | New 构建挂了安全响应头中间件的示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.Use(helmet.NewBuilder().Build())
	r.GET("/", func(c *gin.Context) { c.String(200, "ok") })
	return r
}
