package locallimit_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	example "github.com/apus-run/gala/components/ginx/examples/locallimit"
	"github.com/gin-gonic/gin"
)

func TestExcessConcurrencyRefused429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := example.New()

	// Two slow requests saturate the limit; a third in-flight one gets 429.
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/import", nil))
		}()
	}
	time.Sleep(20 * time.Millisecond) // let both reach the handler

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/import", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("third concurrent request should be refused with 429, got %d", w.Code)
	}
	wg.Wait()
}
