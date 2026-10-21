package casbin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	example "github.com/apus-run/gala/components/ginx/examples/casbin"
	"github.com/gin-gonic/gin"
)

func do(method, path, user string) int {
	gin.SetMode(gin.TestMode)
	r, err := example.New()
	if err != nil {
		panic(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	if user != "" {
		req.Header.Set("X-User", user)
	}
	r.ServeHTTP(w, req)
	return w.Code
}

func TestAuthorizationMatrix(t *testing.T) {
	if code := do(http.MethodGet, "/orders", "alice"); code != http.StatusOK {
		t.Fatalf("alice should list orders, got %d", code)
	}
	if code := do(http.MethodDelete, "/orders", "alice"); code != http.StatusForbidden {
		t.Fatalf("alice must not delete, got %d", code)
	}
	if code := do(http.MethodDelete, "/orders", "bob"); code != http.StatusNoContent {
		t.Fatalf("bob should delete, got %d", code)
	}
	if code := do(http.MethodGet, "/orders", ""); code != http.StatusUnauthorized {
		t.Fatalf("anonymous should be unauthorized, got %d", code)
	}
}
