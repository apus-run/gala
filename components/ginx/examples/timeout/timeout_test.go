package timeout_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	example "github.com/apus-run/gala/components/ginx/examples/timeout"
	"github.com/gin-gonic/gin"
)

func TestDeadlineExceededFailsFast(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	example.New().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/report", nil))
	// Not 200 after a full second: the deadline fired and recovery answered.
	if w.Code == http.StatusOK {
		t.Fatal("slow handler should not complete normally")
	}
}
