package job

import (
	"context"
	"errors"
	"io"
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

func TestHandlerBodyOverMaxBytes(t *testing.T) {
	r := NewRegistry()
	var (
		runs   atomic.Int32
		gotLen atomic.Int64
	)
	if err := r.Register("echo", func(ctx context.Context) error {
		inv, _ := FromContext(ctx)
		gotLen.Store(int64(len(inv.Payload)))
		runs.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	h := r.Handler(WithMaxBodyBytes(16))

	// Exactly at the limit is accepted and delivered whole.
	// 恰好等于上限的请求体被接受并完整交付。
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(strings.Repeat("a", 16)))
	req.Header.Set(defaultJobHeader, "echo")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("at-limit status %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("job ran %d times, want 1", got)
	}
	if got := gotLen.Load(); got != 16 {
		t.Fatalf("payload length %d, want 16", got)
	}

	// One byte over is refused with 413 and the job does not run.
	// 超出 1 字节返回 413 且任务不运行。
	req = httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(strings.Repeat("b", 17)))
	req.Header.Set(defaultJobHeader, "echo")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-limit status %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("job ran on an over-limit body: %d runs, want 1", got)
	}
}

// notReadReader records Read attempts and fails them, so a test can prove a
// body was never touched.
// notReadReader 记录并拒绝 Read 调用，测试因此可以证明请求体从未被读取。
type notReadReader struct{ reads atomic.Int32 }

func (r *notReadReader) Read([]byte) (int, error) {
	r.reads.Add(1)
	return 0, errors.New("body must not be read")
}

func TestHandlerUnknownJobDoesNotReadBody(t *testing.T) {
	body := &notReadReader{}
	req := httptest.NewRequest(http.MethodPost, "/jobs", body)
	req.Header.Set(defaultJobHeader, "missing")
	rec := httptest.NewRecorder()

	NewRegistry().Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := body.reads.Load(); got != 0 {
		t.Fatalf("body read %d times for an unknown job, want 0", got)
	}
}

func TestHandlerUnknownJobCallsErrorHandlerWithoutReadingBody(t *testing.T) {
	var gotErr error
	h := NewRegistry().Handler(WithHTTPErrorHandler(func(w http.ResponseWriter, _ *http.Request, err error) {
		gotErr = err
		http.Error(w, "custom", http.StatusGone)
	}))

	body := &notReadReader{}
	req := httptest.NewRequest(http.MethodPost, "/jobs", body)
	req.Header.Set(defaultJobHeader, "missing")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("status %d, want the handler's %d", rec.Code, http.StatusGone)
	}
	if !errors.Is(gotErr, ErrNotFound) {
		t.Fatalf("handler error %v, want ErrNotFound", gotErr)
	}
	if got := body.reads.Load(); got != 0 {
		t.Fatalf("body read %d times for an unknown job, want 0", got)
	}
}

func TestHandlerDefaultMaxBodyBytes(t *testing.T) {
	if defaultMaxBodyBytes != 10<<20 {
		t.Fatalf("defaultMaxBodyBytes = %d, want 10 MiB", defaultMaxBodyBytes)
	}

	for _, tt := range []struct {
		name string
		opts []HTTPOption
	}{
		{name: "default"},
		{name: "non-positive keeps default", opts: []HTTPOption{WithMaxBodyBytes(0), WithMaxBodyBytes(-1)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			var ran atomic.Bool
			if err := r.Register("echo", func(context.Context) error {
				ran.Store(true)
				return nil
			}); err != nil {
				t.Fatalf("register: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/jobs", &zeroReader{n: defaultMaxBodyBytes + 1})
			req.Header.Set(defaultJobHeader, "echo")
			rec := httptest.NewRecorder()
			r.Handler(tt.opts...).ServeHTTP(rec, req)

			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
			}
			if ran.Load() {
				t.Fatal("job ran on a body over the default limit")
			}
		})
	}
}

func TestWithMaxBodyBytesOption(t *testing.T) {
	c := httpConfig{maxBodyBytes: 42}

	WithMaxBodyBytes(0)(&c)
	WithMaxBodyBytes(-1)(&c)
	if c.maxBodyBytes != 42 {
		t.Fatalf("non-positive limit changed the cap: %d, want 42", c.maxBodyBytes)
	}

	WithMaxBodyBytes(7)(&c)
	if c.maxBodyBytes != 7 {
		t.Fatalf("cap %d, want 7", c.maxBodyBytes)
	}
}

// zeroReader yields n zero bytes without buffering them up front.
// zeroReader 按需产出 n 个零字节，不预先缓冲。
type zeroReader struct{ n int64 }

func (r *zeroReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.n {
		p = p[:r.n]
	}
	clear(p)
	r.n -= int64(len(p))
	return len(p), nil
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
