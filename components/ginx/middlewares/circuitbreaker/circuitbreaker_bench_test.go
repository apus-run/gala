// Copyright (c) 2021 Fiber. Adapted from gofiber/contrib/v3/circuitbreaker.
// Licensed under the MIT License; see LICENSE.
package circuitbreaker_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apus-run/gala/components/ginx/middlewares/circuitbreaker"
	"github.com/gin-gonic/gin"
)

// These measure whole Gin requests in parallel against one shared circuit.
// Compare closed/open with the baseline to assess middleware overhead.
func benchParallel(b *testing.B, cb *circuitbreaker.CircuitBreaker) {
	router := gin.New()
	if cb != nil {
		router.Use(cb.Build())
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
func BenchmarkCircuitBreakerBaseline(b *testing.B) { benchParallel(b, nil) }
func BenchmarkCircuitBreakerClosed(b *testing.B) {
	benchParallel(b, circuitbreaker.New(circuitbreaker.Config{}))
}
func BenchmarkCircuitBreakerOpen(b *testing.B) {
	cb := circuitbreaker.New(circuitbreaker.Config{})
	cb.ForceOpen()
	benchParallel(b, cb)
}
