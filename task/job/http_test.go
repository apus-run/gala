package job

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHandlerRejectsNonPOST(t *testing.T) {
	h := NewRegistry().Handler()

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/jobs", nil))

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status %d, want %d", method, rec.Code, http.StatusMethodNotAllowed)
		}
		if got := rec.Header().Get("Allow"); got != http.MethodPost {
			t.Errorf("%s: Allow %q, want POST", method, got)
		}
	}
}

func TestHandlerMissingJobHeader(t *testing.T) {
	h := NewRegistry().Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/jobs", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandlerUnknownJob(t *testing.T) {
	h := NewRegistry().Handler()

	req := httptest.NewRequest(http.MethodPost, "/jobs", nil)
	req.Header.Set(defaultJobHeader, "missing")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandlerSuccess(t *testing.T) {
	r := NewRegistry()
	var (
		got Invocation
		ok  bool
	)
	if err := r.Register("echo", func(ctx context.Context) error {
		got, ok = FromContext(ctx)
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader("the payload"))
	req.Header.Set(defaultJobHeader, "echo")
	req.Header.Set(defaultRunIDHeader, "run-42")
	req.Header.Set("X-Custom", "custom-value")
	rec := httptest.NewRecorder()

	r.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("204 response has a body: %q", rec.Body.String())
	}
	if !ok {
		t.Fatal("handler did not put the invocation into the context")
	}
	if got.Name != "echo" || got.RunID != "run-42" {
		t.Fatalf("got name/runID %q/%q, want echo/run-42", got.Name, got.RunID)
	}
	if string(got.Payload) != "the payload" {
		t.Fatalf("got payload %q, want %q", got.Payload, "the payload")
	}
	if got.Header.Get("X-Custom") != "custom-value" {
		t.Fatalf("request headers not copied into the invocation: %v", got.Header)
	}
	if echoed := rec.Header().Get(defaultRunIDHeader); echoed != "run-42" {
		t.Fatalf("run id echo %q, want run-42", echoed)
	}
}

func TestHandlerDoesNotEchoEmptyRunID(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("echo", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("register: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/jobs", nil)
	req.Header.Set(defaultJobHeader, "echo")
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want %d", rec.Code, http.StatusNoContent)
	}
	if _, present := rec.Header()[defaultRunIDHeader]; present {
		t.Fatalf("response unexpectedly has %s", defaultRunIDHeader)
	}
}

func TestHandlerCustomHeaders(t *testing.T) {
	r := NewRegistry()
	var runs atomic.Int32
	if err := r.Register("echo", func(context.Context) error {
		runs.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	h := r.Handler(WithJobHeader("X-Custom-Job"), WithRunIDHeader("X-Custom-Run"))

	// The default headers must no longer be understood.
	// 默认请求头不再生效。
	req := httptest.NewRequest(http.MethodPost, "/jobs", nil)
	req.Header.Set(defaultJobHeader, "echo")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("default header accepted after WithJobHeader: status %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/jobs", nil)
	req.Header.Set("X-Custom-Job", "echo")
	req.Header.Set("X-Custom-Run", "run-custom")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("custom header: status %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("X-Custom-Run"); got != "run-custom" {
		t.Fatalf("custom run id echo %q, want run-custom", got)
	}
	if runs.Load() != 1 {
		t.Fatalf("job ran %d times, want 1", runs.Load())
	}
}

func TestHandlerJobErrorIs500(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("fail", func(context.Context) error {
		return errors.New("boom")
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/jobs", nil)
	req.Header.Set(defaultJobHeader, "fail")
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Fatal("500 body leaks the job error")
	}
}

// TestHandlerErrorHandlerSeesRunError covers WithHTTPErrorHandler receiving
// the wrapped Run error (including a *PanicError with its stack) and owning
// the response, so callers can log and map lease contention.
// TestHandlerErrorHandlerSeesRunError 覆盖 WithHTTPErrorHandler 收到包装后的 Run
// 错误（包括带堆栈的 *PanicError）并负责写响应，调用方因此可以记录日志并映射租约
// 竞争。
func TestHandlerErrorHandlerSeesRunError(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("boom", func(context.Context) error {
		panic("kaboom")
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	var gotErr error
	h := r.Handler(WithHTTPErrorHandler(func(w http.ResponseWriter, _ *http.Request, err error) {
		gotErr = err
		http.Error(w, "custom", http.StatusConflict)
	}))

	req := httptest.NewRequest(http.MethodPost, "/jobs", nil)
	req.Header.Set(defaultJobHeader, "boom")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want the handler's %d", rec.Code, http.StatusConflict)
	}
	var pe *PanicError
	if !errors.As(gotErr, &pe) || len(pe.Stack) == 0 {
		t.Fatalf("handler did not receive the *PanicError with its stack: %v", gotErr)
	}
	if strings.Contains(rec.Body.String(), "kaboom") {
		t.Fatal("custom handler body unexpectedly contains the panic value")
	}
}

func TestHandlerRecoversPanic(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("boom", func(context.Context) error {
		panic("kaboom")
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/jobs", nil)
	req.Header.Set(defaultJobHeader, "boom")
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req) // must not panic / crash the test process

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if strings.Contains(rec.Body.String(), "kaboom") {
		t.Fatal("500 body leaks the panic value")
	}
}
