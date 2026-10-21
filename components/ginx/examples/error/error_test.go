package error_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	example "github.com/apus-run/gala/components/ginx/examples/error"
	"github.com/gin-gonic/gin"
)

func TestTypedErrorBecomes400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	example.New().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/orders/7", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ORDER_NOT_FOUND" || body["message"] != "order does not exist" {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestHappyPathUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	example.New().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/orders/42", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}
