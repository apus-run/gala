package ginbreaker

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// These mirror ginx/middlewares/circuitbreaker's bench: whole Gin requests in
// parallel against one shared circuit, baseline excluded. The numbers are
// directly comparable with that package's, unlike the component-level bench.
func benchParallel(b *testing.B, mw gin.HandlerFunc) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	if mw != nil {
		router.Use(mw)
	}
	router.GET("/", func(c *gin.Context) { c.Status(200) })
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		for pb.Next() {
			router.ServeHTTP(httptest.NewRecorder(), request)
		}
	})
}

func BenchmarkGinbreakerClosed(b *testing.B) {
	benchParallel(b, NewBuilder().SetTimeout(time.Hour).Build())
}

func BenchmarkGinbreakerOpen(b *testing.B) {
	builder := NewBuilder().SetTimeout(time.Hour)
	mw := builder.Build()
	builder.Breaker().ForceOpen()
	benchParallel(b, mw)
}
