package bodylimit

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newEngine(max int64, readBody *error) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(NewBuilder().SetMaxBytes(max).Build())
	r.POST("/x", func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		if readBody != nil {
			*readBody = err
		}
		if err != nil {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.Status(http.StatusOK)
	})
	return r
}

func TestWithinLimitPasses(t *testing.T) {
	w := httptest.NewRecorder()
	newEngine(10, nil).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("0123456789")))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// 声明了过大的 Content-Length：不读 body，直接 413。
func TestDeclaredLengthOverLimit(t *testing.T) {
	called := false
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(NewBuilder().SetMaxBytes(10).Build())
	r.POST("/x", func(c *gin.Context) { called = true })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(strings.Repeat("a", 11))))
	if w.Code != http.StatusRequestEntityTooLarge || called {
		t.Fatalf("status = %d, handler called = %v; want 413 without running the handler", w.Code, called)
	}
}

// 分块传输没有 Content-Length：读取超限时必须失败，不能把整个 body 读进内存。
func TestChunkedBodyOverLimitFailsToRead(t *testing.T) {
	var readErr error
	req := httptest.NewRequest(http.MethodPost, "/x", io.NopCloser(strings.NewReader(strings.Repeat("a", 1000))))
	req.ContentLength = -1
	w := httptest.NewRecorder()
	newEngine(10, &readErr).ServeHTTP(w, req)

	var tooLarge *http.MaxBytesError
	if !errors.As(readErr, &tooLarge) {
		t.Fatalf("read error = %v, want *http.MaxBytesError", readErr)
	}
}
