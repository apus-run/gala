// Package recovery demonstrates the panic safety net: a panicking handler
// becomes a 500 JSON response instead of a dropped connection. Mount it early
// so access logs still record the request.
package recovery

import (
	"github.com/apus-run/gala/components/ginx/middlewares/recovery"
	"github.com/gin-gonic/gin"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(recovery.NewBuilder().Build())
	r.GET("/orders/:id", func(c *gin.Context) {
		if c.Param("id") == "0" { // a nil-map write or similar programming error
			panic("order index not initialized")
		}
		c.JSON(200, gin.H{"id": c.Param("id")})
	})
	return r
}
