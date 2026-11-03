// Package casbin demonstrates authorization after authentication: an
// application-owned Enforcer answers allow/deny for subject, path and method.
// The middleware only reads the identity (here from a header set by your auth
// layer); mount real authentication first, as the package docs require.
// Package casbin 演示认证之后的鉴权：由应用持有的 Enforcer 对主体、路径和
// 方法判定允许或拒绝。中间件只读取身份（此处来自你的认证层设置的请求头）；
// 按包文档的要求，请先挂载真实的认证中间件。
package casbin

import (
	"net/http"

	middleware "github.com/apus-run/gala/components/ginx/middlewares/casbin"
	"github.com/gin-gonic/gin"
)

// allowSet stands in for your casbin enforcer; use authz.Authorizer.Enforce
// in production.
// allowSet 用来代替你的 casbin enforcer；生产环境请使用
// authz.Authorizer.Enforce。
type allowSet struct{ rules map[[3]string]bool }

// Enforce answers the policy decision for the given values | Enforce 对给定值返回策略判定结果.
func (a allowSet) Enforce(vals ...any) (bool, error) {
	key := [3]string{}
	for i := range key {
		s, _ := vals[i].(string)
		key[i] = s
	}
	return a.rules[key], nil
}

// New builds the demo engine with the casbin middleware | New 构建挂了 casbin 中间件的示例引擎.
func New() (*gin.Engine, error) {
	mw, err := middleware.NewBuilder().
		SetEnforcer(allowSet{rules: map[[3]string]bool{
			{"alice", "/orders", http.MethodGet}:  true,
			{"bob", "/orders", http.MethodDelete}: true,
		}}).
		SetLookup(func(c *gin.Context) (string, error) { // from your auth layer | 来自你的认证层
			return c.GetHeader("X-User"), nil
		}).
		Build()
	if err != nil {
		return nil, err
	}
	r := gin.New()
	r.Use(mw)
	r.GET("/orders", func(c *gin.Context) { c.JSON(200, gin.H{"orders": []int{1, 2}}) })
	r.DELETE("/orders", func(c *gin.Context) { c.Status(204) })
	return r, nil
}
