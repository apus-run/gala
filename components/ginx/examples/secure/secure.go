// Package secure demonstrates the unrolled/secure middleware: by default it
// redirects plain HTTP to the configured SSL host and sets transport-security
// headers. Tune the embedded secure.Options for your deployment.
// Package secure 演示 unrolled/secure 中间件：默认将纯 HTTP 请求重定向到
// 配置的 SSL 主机，并设置传输安全响应头。请按部署情况调整内嵌的
// secure.Options。
package secure

import (
	"net/http"

	middleware "github.com/apus-run/gala/components/ginx/middlewares/secure"
	"github.com/gin-gonic/gin"
)

// New builds the demo engine with the secure middleware | New 构建挂了安全传输中间件的示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.Use(middleware.NewBuilder().Build()) // defaults: SSLRedirect to 127.0.0.1:443 | 默认值：SSLRedirect 到 127.0.0.1:443
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}
