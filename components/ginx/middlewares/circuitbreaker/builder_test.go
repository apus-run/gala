package circuitbreaker_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/apus-run/gala/components/ginx/middlewares/circuitbreaker"
	"github.com/gin-gonic/gin"
)

func builderRouter(mw gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(mw)
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })
	return r
}

func TestBuilderChainsSettingsAndExposesBreaker(t *testing.T) {
	clock := newFakeClock()
	b := circuitbreaker.NewBuilder().SetFailureThreshold(1).SetTimeout(time.Hour).SetClock(clock.Now)
	mw := b.Build()
	if b.Breaker() == nil {
		t.Fatal("Breaker is nil after Build")
	}
	r := builderRouter(mw)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)) // fails, trips
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 while open, got %d", w.Code)
	}
	b.Breaker().ForceClose() // the exposed handle controls the circuit
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want the handler's 500 after ForceClose, got %d", w.Code)
	}
	b.Breaker().ForceOpen()
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 after ForceOpen, got %d", w.Code)
	}
}

func TestBuilderDefaultsTripAtDefaultThreshold(t *testing.T) {
	b := circuitbreaker.NewBuilder()
	if b.Breaker() != nil {
		t.Fatal("Breaker is not nil before Build")
	}
	r := builderRouter(b.Build())
	for range 5 { // DefaultConfig.FailureThreshold
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 after default threshold, got %d", w.Code)
	}
}
