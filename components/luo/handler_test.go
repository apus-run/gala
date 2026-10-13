package luo

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdaptErrorAndWriterIdentity(t *testing.T) {
	want := errors.New("domain error")
	for _, failure := range []error{nil, want} {
		t.Run(failureName(failure), func(t *testing.T) {
			w := httptest.NewRecorder()
			var seenRequest *http.Request
			calls := 0
			root := New(http.NewServeMux())
			root.Mount("/api").GET("/{id}", Adapt(func(writer http.ResponseWriter, r *http.Request) error {
				if writer != w || r.PathValue("id") != "42" {
					t.Fatal("writer identity or path parameters lost")
				}
				seenRequest = r
				if failure == nil {
					writer.(http.Flusher).Flush()
				}
				return failure
			}, func(writer http.ResponseWriter, r *http.Request, err error) {
				calls++
				if writer != w || r != seenRequest || err != want {
					t.Fatal("error callback lost request, writer or error identity")
				}
				writer.WriteHeader(http.StatusConflict)
			}))
			root.ServeHTTP(w, httptest.NewRequest("GET", "/api/42", nil))
			if failure == nil {
				if calls != 0 || !w.Flushed {
					t.Fatalf("calls=%d flushed=%v", calls, w.Flushed)
				}
			} else if calls != 1 || w.Code != http.StatusConflict {
				t.Fatalf("calls=%d code=%d", calls, w.Code)
			}
		})
	}
}

func failureName(err error) string {
	if err == nil {
		return "success"
	}
	return "error"
}
