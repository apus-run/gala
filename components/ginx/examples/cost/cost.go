// Package cost demonstrates per-request latency logging around the rest of
// the chain; mount it after the access log so both see the same request.
package cost

import (
	middleware "github.com/apus-run/gala/components/ginx/middlewares/cost"
	"github.com/gin-gonic/gin"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(middleware.NewBuilder().Build())
	r.GET("/", func(c *gin.Context) { c.String(200, "ok") })
	return r
}
