// Package secure demonstrates the unrolled/secure middleware: by default it
// redirects plain HTTP to the configured SSL host and sets transport-security
// headers. Tune the embedded secure.Options for your deployment.
package secure

import (
	"net/http"

	middleware "github.com/apus-run/gala/components/ginx/middlewares/secure"
	"github.com/gin-gonic/gin"
)

func New() *gin.Engine {
	r := gin.New()
	r.Use(middleware.NewBuilder().Build()) // defaults: SSLRedirect to 127.0.0.1:443
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}
