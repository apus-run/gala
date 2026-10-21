// Package casbin demonstrates authorization after authentication: an
// application-owned Enforcer answers allow/deny for subject, path and method.
// The middleware only reads the identity (here from a header set by your auth
// layer); mount real authentication first, as the package docs require.
package casbin

import (
	"net/http"

	middleware "github.com/apus-run/gala/components/ginx/middlewares/casbin"
	"github.com/gin-gonic/gin"
)

// allowSet stands in for your casbin enforcer; use authz.Authorizer.Enforce
// in production.
type allowSet struct{ rules map[[3]string]bool }

func (a allowSet) Enforce(vals ...any) (bool, error) {
	key := [3]string{}
	for i := range key {
		s, _ := vals[i].(string)
		key[i] = s
	}
	return a.rules[key], nil
}

func New() (*gin.Engine, error) {
	mw, err := middleware.NewBuilder().
		SetEnforcer(allowSet{rules: map[[3]string]bool{
			{"alice", "/orders", http.MethodGet}:  true,
			{"bob", "/orders", http.MethodDelete}: true,
		}}).
		SetLookup(func(c *gin.Context) (string, error) { // from your auth layer
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
