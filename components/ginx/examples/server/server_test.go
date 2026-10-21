package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/apus-run/gala/components/ginx/middlewares/accesslog"
	"github.com/gin-gonic/gin"
)

func newTestServer(t *testing.T) (*Server, *[]accesslog.AccessLog) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var mu sync.Mutex
	logs := make([]accesslog.AccessLog, 0, 8)
	s := New(func(_ context.Context, al *accesslog.AccessLog) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, *al)
	})
	return s, &logs
}

func get(r *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestHappyPathCarriesRequestIDAndLogs(t *testing.T) {
	s, logs := newTestServer(t)
	s.Engine.GET("/api/v1/boom", func(c *gin.Context) { panic("demo") }) // test-only route

	w := get(s.Engine, "/api/v1/orders/42")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if w.Header().Get("X-Request-ID") == "" {
		t.Fatal("X-Request-ID missing")
	}
	var order map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &order); err != nil || order["status"] != "paid" {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}

	// A recovered panic still yields a log line and a 500, not a dead connection.
	if w := get(s.Engine, "/api/v1/boom"); w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 after recovery, got %d", w.Code)
	}
	if n := len(*logs); n != 2 {
		t.Fatalf("want 2 access log entries, got %d", n)
	}
	if first := (*logs)[0]; first.StatusCode != http.StatusOK || first.Path != "/api/v1/orders/42" {
		t.Fatalf("unexpected first log entry: %+v", first)
	}
}

func TestBreakerProtectsGroupButNotLiveness(t *testing.T) {
	s, _ := newTestServer(t)
	s.Breaker.Breaker().ForceOpen()

	if w := get(s.Engine, "/api/v1/orders/42"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 behind an open circuit, got %d", w.Code)
	}
	if w := get(s.Engine, "/healthz"); w.Code != http.StatusOK {
		t.Fatalf("want 200 from liveness outside the group, got %d", w.Code)
	}
	if w := get(s.Engine, "/debug/breaker"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 from breaker health, got %d", w.Code)
	}
}

func TestUpstreamFailureCountsTowardTripping(t *testing.T) {
	s, _ := newTestServer(t)
	for range 3 { // SetFailureThreshold(3); order "0" always fails upstream
		if w := get(s.Engine, "/api/v1/orders/0"); w.Code != http.StatusBadGateway {
			t.Fatalf("want 502 from the dependency, got %d", w.Code)
		}
	}
	if w := get(s.Engine, "/api/v1/orders/42"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want the group refused after three failures, got %d", w.Code)
	}
}

func TestCompressedPanicReturns500JSON(t *testing.T) {
	s, _ := newTestServer(t)
	s.Engine.GET("/boom", func(c *gin.Context) { panic("demo") })
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	s.Engine.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
	reader, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Code != 500 {
		t.Fatalf("unexpected error body: %s", body)
	}
}
