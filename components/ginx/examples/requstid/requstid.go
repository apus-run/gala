// Package requstid demonstrates mounting the request-ID middleware first in
// the chain: every downstream middleware and the client see the same ID, and
// an incoming X-Request-ID is honored instead of regenerated.
package requstid

import (
	"github.com/apus-run/gala/components/ginx/middlewares/requstid"
	"github.com/gin-gonic/gin"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(requstid.RequestID())
	r.GET("/whoami", func(c *gin.Context) {
		c.JSON(200, gin.H{"request_id": requstid.GetCtxRequestID(c)})
	})
	return r
}
