// Package locallimit demonstrates the in-process active-request cap: at most
// N handlers run concurrently per process; the excess gets 429 before any
// downstream work starts. For a cluster-wide cap see redislimit.
package locallimit

import (
	"time"

	"github.com/apus-run/gala/components/ginx/middlewares/activelimit/locallimit"
	"github.com/gin-gonic/gin"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(locallimit.NewLocalActiveLimit(2).Build())
	r.POST("/import", func(c *gin.Context) { // heavy, bounded work
		time.Sleep(50 * time.Millisecond)
		c.String(200, "imported")
	})
	return r
}
