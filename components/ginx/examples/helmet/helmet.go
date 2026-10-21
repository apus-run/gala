// Package helmet demonstrates security response headers with upstream Helmet
// semantics: XSS protection, content-type sniffing protection and friends on
// every response, mounted once engine-wide.
package helmet

import (
	"github.com/apus-run/gala/components/ginx/middlewares/helmet"
	"github.com/gin-gonic/gin"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(helmet.NewBuilder().Build())
	r.GET("/", func(c *gin.Context) { c.String(200, "ok") })
	return r
}
