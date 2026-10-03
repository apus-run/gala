package casbin_test

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	casbinmw "github.com/apus-run/gala/components/ginx/middlewares/casbin"
	"github.com/gin-gonic/gin"
)

type enforcerFunc func(...any) (bool, error)

func (f enforcerFunc) Enforce(values ...any) (bool, error) {
	return f(values...)
}

func TestExternalOptionFuncComposition(t *testing.T) {
	calls := 0
	composed := casbinmw.OptionFunc(func(o *casbinmw.Options) {
		o.Apply(
			casbinmw.WithValidationRule(casbinmw.AtLeastOneRule),
			casbinmw.WithPermissionParser(casbinmw.PermissionParserWithSeparator("/")),
		)
	})
	handler, err := casbinmw.NewBuilder().
		SetLookup(func(*gin.Context) (string, error) { return "alice", nil }).
		SetEnforcer(enforcerFunc(func(values ...any) (bool, error) {
			calls++
			return reflect.DeepEqual(values, []any{"alice", "blog", "create"}), nil
		})).
		RequiresPermissions([]string{"blog/delete", "blog/create"}, composed)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.GET("/", handler, func(c *gin.Context) { c.Status(http.StatusNoContent) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusNoContent || calls != 2 {
		t.Fatalf("status=%d calls=%d", response.Code, calls)
	}
}
