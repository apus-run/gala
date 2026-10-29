package timeout

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestBuildTimeoutReturns504(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(NewBuilder().SetTimeout(10 * time.Millisecond).Build())
	r.GET("/slow", func(c *gin.Context) {
		<-c.Request.Context().Done()
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/slow", nil))

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", w.Code)
	}
}

// 响应已经写完后才超时：不能再追加 504 JSON，也不能 panic。
func TestBuildTimeoutAfterResponseWritten(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(NewBuilder().SetTimeout(10 * time.Millisecond).Build())
	r.GET("/late", func(c *gin.Context) {
		c.String(http.StatusOK, "done")
		<-c.Request.Context().Done()
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/late", nil))

	if w.Code != http.StatusOK || w.Body.String() != "done" {
		t.Fatalf("got %d %q, want 200 \"done\"", w.Code, w.Body.String())
	}
}
