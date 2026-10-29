package secure

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// SSL 重定向已经写出响应，业务 handler 不能再执行。
func TestBuildAbortsAfterRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	called := false
	r := gin.New()
	r.Use(NewBuilder().Build())
	r.POST("/orders", func(c *gin.Context) { called = true })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "http://example.com/orders", nil))

	if called {
		t.Fatal("handler ran after secure middleware redirected the request")
	}
	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", w.Code)
	}
}
