package ginbreaker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	gobreaker "github.com/sony/gobreaker/v2"
)

func router(t *testing.T, b *Builder, handler gin.HandlerFunc) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(b.Build())
	r.GET("/", handler)
	return r
}

func TestMiddlewareTripsAndRefuses(t *testing.T) {
	r := router(t, NewBuilder().SetFailureThreshold(2).SetTimeout(20*time.Millisecond),
		func(c *gin.Context) { c.Status(http.StatusInternalServerError) })
	for range 2 {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 while open, got %d", w.Code)
	}
}

func TestMiddlewareRecoversAfterProbe(t *testing.T) {
	closed := false
	failing := true
	r := router(t, NewBuilder().
		SetFailureThreshold(1).
		SetTimeout(20*time.Millisecond).
		SetOnClose(func() { closed = true }),
		func(c *gin.Context) {
			if failing {
				c.Status(http.StatusInternalServerError)
				return
			}
			c.Status(http.StatusOK)
		})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	failing = false
	time.Sleep(30 * time.Millisecond) // no injectable clock upstream
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("successful probe should close the circuit, got %d", w.Code)
	}
	if !closed {
		t.Fatal("OnClose did not fire")
	}
}

func TestMiddlewareHalfOpenSaturation(t *testing.T) {
	var block atomic.Bool
	release := make(chan struct{})
	r := router(t, NewBuilder().SetFailureThreshold(1).SetTimeout(20*time.Millisecond),
		func(c *gin.Context) {
			if block.Load() { // the probe: admitted and held in flight
				<-release
				c.Status(http.StatusOK)
				return
			}
			c.Status(http.StatusInternalServerError)
		})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)) // trips
	block.Store(true)
	time.Sleep(30 * time.Millisecond)
	go r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	time.Sleep(10 * time.Millisecond) // let the probe reach the handler
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	close(release)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429 while half-open saturated, got %d", w.Code)
	}
}

func TestMiddlewarePanicsCountAsFailures(t *testing.T) {
	r := router(t, NewBuilder().SetFailureThreshold(1).SetTimeout(time.Hour),
		func(c *gin.Context) { panic("boom") })
	func() {
		defer func() { _ = recover() }() // no Recovery mounted: the panic must escape to the app
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	// Same run recorded the panic as a failure, so the next call is refused.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 after panic tripped the circuit, got %d", w.Code)
	}
}

func TestHealthHandlerReportsOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	builder := NewBuilder().SetFailureThreshold(1).SetTimeout(time.Hour)
	builder.Build()
	builder.Breaker().ForceOpen()
	r := gin.New()
	r.GET("/health", HealthHandler(builder.Breaker()))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 while forced open, got %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["state"] != gobreaker.StateOpen.String() {
		t.Fatalf("want state open, got %v", body["state"])
	}
}

func TestOnCloseCanInspectBreakerAndKeepsBuiltCallback(t *testing.T) {
	b := NewBuilder().SetFailureThreshold(1).SetTimeout(time.Millisecond)
	observed := make(chan gobreaker.State, 1)
	b.SetOnClose(func() { observed <- b.Breaker().Stats().State })
	failing := true
	r := router(t, b, func(c *gin.Context) {
		if failing {
			c.Status(500)
		} else {
			c.Status(200)
		}
	})
	b.SetOnClose(func() { t.Error("built middleware used replacement callback") })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	failing = false
	time.Sleep(5 * time.Millisecond)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("OnClose deadlocked while reading Stats")
	}
	select {
	case state := <-observed:
		if state != gobreaker.StateClosed {
			t.Fatalf("want closed, got %v", state)
		}
	default:
		t.Fatal("built callback was not invoked")
	}
}

func TestInternalServiceBuilderTripsAfterSixConsecutiveFailures(t *testing.T) {
	r := router(t, NewInternalServiceBuilder().SetTimeout(time.Hour),
		func(c *gin.Context) { c.Status(http.StatusInternalServerError) })
	hit := func() int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		return w.Code
	}
	for i := 1; i <= 6; i++ { // the profile tolerates five failures
		if code := hit(); code != http.StatusInternalServerError {
			t.Fatalf("request %d should reach the handler (500), got %d", i, code)
		}
	}
	if code := hit(); code != http.StatusServiceUnavailable {
		t.Fatalf("sixth failure should have tripped, got %d", code)
	}
}

func TestExternalAPIBuilderTripsOnFailureRate(t *testing.T) {
	var hits atomic.Int32
	r := router(t, NewExternalAPIBuilder().SetTimeout(time.Hour),
		func(c *gin.Context) {
			n := hits.Add(1)
			if n <= 3 || n == 6 { // 3 early failures, 2 successes, the 6th fails
				c.Status(http.StatusInternalServerError)
				return
			}
			c.Status(http.StatusOK)
		})
	hit := func() int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		return w.Code
	}
	for range 6 { // 4/6 ≈ 67% >= 50% with >= 5 requests: trips on the 6th
		hit()
	}
	if code := hit(); code != http.StatusServiceUnavailable {
		t.Fatalf("circuit should be open after crossing the failure rate, got %d", code)
	}
}

func TestStateChangeHookObservesTransition(t *testing.T) {
	var mu sync.Mutex
	var seen [][2]gobreaker.State
	r := router(t, NewBuilder().SetFailureThreshold(1).SetTimeout(time.Hour).
		SetOnStateChange(func(from, to gobreaker.State) {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, [2]gobreaker.State{from, to})
		}),
		func(c *gin.Context) { c.Status(http.StatusInternalServerError) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != [2]gobreaker.State{gobreaker.StateClosed, gobreaker.StateOpen} {
		t.Fatalf("want one closed→open transition, got %v", seen)
	}
}
