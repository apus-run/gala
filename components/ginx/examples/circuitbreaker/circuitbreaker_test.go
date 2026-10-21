package circuitbreaker_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	example "github.com/apus-run/gala/components/ginx/examples/circuitbreaker"
	"github.com/gin-gonic/gin"
)

func TestFailuresOpenCircuitGroupOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := example.New()
	get := func(path string) int {
		w := httptest.NewRecorder()
		s.Engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w.Code
	}
	for range 2 { // two upstream failures trip the threshold
		get("/api/orders/0")
	}
	if code := get("/api/orders/42"); code != http.StatusServiceUnavailable {
		t.Fatalf("group should fail fast while open, got %d", code)
	}
	if code := get("/healthz"); code != http.StatusOK {
		t.Fatalf("liveness outside the group must stay reachable, got %d", code)
	}
	if s.Breaker.Breaker().IsOpen() != true {
		t.Fatal("breaker handle should report open")
	}
}
