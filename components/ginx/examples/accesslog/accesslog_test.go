package accesslog_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/apus-run/gala/components/ginx/examples/accesslog"
	middleware "github.com/apus-run/gala/components/ginx/middlewares/accesslog"
	"github.com/gin-gonic/gin"
)

func TestEntryCapturedAndHealthIgnored(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var mu sync.Mutex
	var got []middleware.AccessLog
	r := accesslog.New(func(_ context.Context, al *middleware.AccessLog) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, *al)
	})

	for _, path := range []string{"/echo", "/healthz", "/echo"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("want 200 for %s, got %d", path, w.Code)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 { // /healthz is ignored
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	if got[0].StatusCode != http.StatusOK || got[0].RespBody == "" || got[0].Duration == "" {
		t.Fatalf("incomplete entry: %+v", got[0])
	}
}
