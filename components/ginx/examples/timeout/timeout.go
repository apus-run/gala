// Package timeout demonstrates bounding handler time: the request context
// gets a deadline and handlers must respect it. shortcut: the middleware
// panics on deadline, so an outer recovery answers 500, not 504; upgrade the
// middleware when the status distinction matters.
package timeout

import (
	"time"

	mwrecovery "github.com/apus-run/gala/components/ginx/middlewares/recovery"
	middleware "github.com/apus-run/gala/components/ginx/middlewares/timeout"
	"github.com/gin-gonic/gin"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(mwrecovery.NewBuilder().Build()) // catches the deadline panic below
	r.Use(middleware.NewBuilder().SetTimeout(100 * time.Millisecond).Build())
	r.GET("/report", func(c *gin.Context) {
		select { // a real handler polls the request context while working
		case <-time.After(time.Second):
			c.String(200, "slow report done")
		case <-c.Request.Context().Done():
			return
		}
	})
	return r
}
