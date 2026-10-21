package redislimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	example "github.com/apus-run/gala/components/ginx/examples/redislimit"
	"github.com/redis/go-redis/v9"
)

func TestClusterLimitSharedThroughRedis(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	// Two separate processes (engines) share the same Redis key and cap.
	procA := example.New(client, 1)
	procB := example.New(client, 1)

	done := make(chan struct{})
	go func() { // procA takes the only lease
		w := httptest.NewRecorder()
		procA.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/import", nil))
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)

	w := httptest.NewRecorder()
	procB.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/import", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("other process's request should be refused, got %d", w.Code)
	}
	<-done
}
