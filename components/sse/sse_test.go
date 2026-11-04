package sse_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/apus-run/gala/components/sse"
)

// frame sends e through a fresh Writer and returns the bytes on the wire.
func frame(t *testing.T, e sse.Event) string {
	t.Helper()
	rec := httptest.NewRecorder()
	w, err := sse.NewWriter(rec)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.Send(e); err != nil {
		t.Fatalf("Send: %v", err)
	}
	return rec.Body.String()
}

func TestSendFrames(t *testing.T) {
	tests := []struct {
		name string
		in   sse.Event
		want string
	}{
		{"plain", sse.Event{Data: []byte("hello")}, "data: hello\n\n"},
		{"lf", sse.Event{Data: []byte("a\nb")}, "data: a\ndata: b\n\n"},
		{"mixed line endings", sse.Event{Data: []byte("a\r\nb\rc\nd")}, "data: a\ndata: b\ndata: c\ndata: d\n\n"},
		{"trailing newline keeps empty line", sse.Event{Data: []byte("a\n")}, "data: a\ndata: \n\n"},
		{"empty event", sse.Event{}, "data: \n\n"},
		{"leading space preserved", sse.Event{Data: []byte(" x")}, "data:  x\n\n"},
		{"all fields", sse.Event{ID: "7", Type: "tick", Retry: 1500 * time.Millisecond, Data: []byte("x")},
			"id: 7\nevent: tick\nretry: 1500\ndata: x\n\n"},
		{"retry only", sse.Event{Retry: 3 * time.Second}, "retry: 3000\n\n"},
		{"retry with data", sse.Event{Retry: time.Second, Data: []byte("x")}, "retry: 1000\ndata: x\n\n"},
		{"sub-millisecond retry omitted", sse.Event{Retry: time.Microsecond}, "data: \n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := frame(t, tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSendRejectsInvalidFields(t *testing.T) {
	for name, e := range map[string]sse.Event{
		"id lf":   {ID: "a\nb"},
		"id cr":   {ID: "a\rb"},
		"id nul":  {ID: "a\x00b"},
		"type lf": {Type: "a\nb"},
		"type cr": {Type: "a\rb"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			w, err := sse.NewWriter(rec)
			if err != nil {
				t.Fatal(err)
			}
			if err := w.Send(e); !errors.Is(err, sse.ErrInvalidField) {
				t.Fatalf("err = %v, want ErrInvalidField", err)
			}
			if rec.Body.Len() != 0 {
				t.Fatalf("invalid event wrote %q", rec.Body.String())
			}
			// The stream is still usable afterwards | 之后流仍可用
			if err := w.Send(sse.Event{Data: []byte("ok")}); err != nil {
				t.Fatalf("Send after invalid: %v", err)
			}
		})
	}
}

func TestSendJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := sse.NewWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	type token struct {
		Delta string `json:"delta"`
	}
	if err := w.SendJSON(token{Delta: "hi"}); err != nil {
		t.Fatal(err)
	}
	if got, want := rec.Body.String(), "data: {\"delta\":\"hi\"}\n\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// A value json cannot marshal fails without touching the stream | 无法序列化的值
	if err := w.SendJSON(make(chan int)); err == nil {
		t.Fatal("want marshal error")
	}
	if err := w.Send(sse.Event{Data: []byte("ok")}); err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.String(); got != "data: {\"delta\":\"hi\"}\n\ndata: ok\n\n" {
		t.Fatalf("stream corrupted: %q", got)
	}
}

func TestComment(t *testing.T) {
	rec := httptest.NewRecorder()
	w, _ := sse.NewWriter(rec)
	if err := w.Comment("ping"); err != nil {
		t.Fatal(err)
	}
	if err := w.Comment("a\nb"); err != nil {
		t.Fatal(err)
	}
	if err := w.Comment(""); err != nil {
		t.Fatal(err)
	}
	if got, want := rec.Body.String(), ": ping\n\n: a\n: b\n\n: \n\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNewWriterHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	if _, err := sse.NewWriter(rec); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"X-Accel-Buffering": "no",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if rec.Code != http.StatusOK || !rec.Flushed {
		t.Fatalf("header not committed: code=%d flushed=%v", rec.Code, rec.Flushed)
	}
}

// noFlush is a ResponseWriter that cannot flush and has no Unwrap.
type noFlush struct {
	h    http.Header
	body bytes.Buffer
	code int
}

func (n *noFlush) Header() http.Header         { return n.h }
func (n *noFlush) Write(p []byte) (int, error) { return n.body.Write(p) }
func (n *noFlush) WriteHeader(code int)        { n.code = code }

func TestNewWriterWithoutFlush(t *testing.T) {
	n := &noFlush{h: http.Header{}}
	w, err := sse.NewWriter(n)
	if w != nil || !errors.Is(err, sse.ErrFlushUnsupported) {
		t.Fatalf("got (%v, %v), want ErrFlushUnsupported", w, err)
	}
	if !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("ErrFlushUnsupported should wrap http.ErrNotSupported: %v", err)
	}
	if n.code != 0 || n.body.Len() != 0 {
		t.Fatalf("response must stay untouched, code=%d body=%q", n.code, n.body.String())
	}
}

// unwrapper hides Flush but exposes the wrapped writer, like many middlewares.
type unwrapper struct{ http.ResponseWriter }

func (u unwrapper) Unwrap() http.ResponseWriter { return u.ResponseWriter }

func TestNewWriterThroughUnwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := sse.NewWriter(unwrapper{rec})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Send(sse.Event{Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if !rec.Flushed || rec.Body.String() != "data: x\n\n" {
		t.Fatalf("flushed=%v body=%q", rec.Flushed, rec.Body.String())
	}
}

// deadlineWriter records SetWriteDeadline calls | 记录 SetWriteDeadline 调用.
type deadlineWriter struct {
	*httptest.ResponseRecorder
	mu        sync.Mutex
	deadlines []time.Time
}

func (d *deadlineWriter) SetWriteDeadline(t time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deadlines = append(d.deadlines, t)
	return nil
}

func TestWriteDeadlines(t *testing.T) {
	t.Run("cleared on creation, untouched by Send", func(t *testing.T) {
		d := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
		w, err := sse.NewWriter(d)
		if err != nil {
			t.Fatal(err)
		}
		_ = w.Send(sse.Event{})
		if len(d.deadlines) != 1 || !d.deadlines[0].IsZero() {
			t.Fatalf("deadlines = %v, want one zero value", d.deadlines)
		}
	})

	t.Run("WriteTimeout sets then clears around each write", func(t *testing.T) {
		d := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
		w, _ := sse.NewWriter(d)
		w.WriteTimeout = 5 * time.Second
		before := time.Now()
		if err := w.Send(sse.Event{}); err != nil {
			t.Fatal(err)
		}
		if len(d.deadlines) != 3 {
			t.Fatalf("deadlines = %v, want [zero, future, zero]", d.deadlines)
		}
		set := d.deadlines[1]
		if set.Before(before.Add(5*time.Second)) || set.After(time.Now().Add(5*time.Second)) {
			t.Fatalf("deadline %v not ~5s after the write", set)
		}
		if !d.deadlines[0].IsZero() || !d.deadlines[2].IsZero() {
			t.Fatalf("first and last must be zero: %v", d.deadlines)
		}
	})
}

var errBoom = errors.New("boom")

type errWriter struct {
	*httptest.ResponseRecorder
	fail   bool
	writes int
}

func (e *errWriter) Write(p []byte) (int, error) {
	e.writes++
	if e.fail {
		return 0, errBoom
	}
	return e.ResponseRecorder.Write(p)
}

func TestWriteErrorIsSticky(t *testing.T) {
	ew := &errWriter{ResponseRecorder: httptest.NewRecorder()}
	w, err := sse.NewWriter(ew)
	if err != nil {
		t.Fatal(err)
	}
	ew.fail = true
	if err := w.Send(sse.Event{}); !errors.Is(err, errBoom) {
		t.Fatalf("first Send err = %v", err)
	}
	ew.fail = false // even if the writer recovers, the stream is considered dead
	if err := w.Send(sse.Event{}); !errors.Is(err, errBoom) {
		t.Fatalf("second Send err = %v, want the sticky error", err)
	}
	if err := w.Comment("x"); !errors.Is(err, errBoom) {
		t.Fatalf("Comment err = %v, want the sticky error", err)
	}
	if ew.writes != 1 {
		t.Fatalf("writes = %d, want 1 (no writes after failure)", ew.writes)
	}
}

func TestConcurrentSends(t *testing.T) {
	const goroutines, each = 8, 50
	rec := httptest.NewRecorder()
	w, err := sse.NewWriter(rec)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < each; n++ {
				if n%10 == 0 {
					_ = w.Comment("hb")
				}
				if err := w.Send(sse.Event{Data: []byte(fmt.Sprintf("g%d-n%d", g, n))}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()

	got := readData(t, rec.Body.String())
	if len(got) != goroutines*each {
		t.Fatalf("got %d events, want %d (frames interleaved?)", len(got), goroutines*each)
	}
	want := make([]string, 0, goroutines*each)
	for g := 0; g < goroutines; g++ {
		for n := 0; n < each; n++ {
			want = append(want, fmt.Sprintf("g%d-n%d", g, n))
		}
	}
	sort.Strings(got)
	sort.Strings(want)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// readData parses body and returns every event's Data | 解析 body，返回所有事件的 Data.
func readData(t *testing.T, body string) []string {
	t.Helper()
	r := sse.NewReader(bytes.NewBufferString(body))
	var out []string
	for {
		e, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		out = append(out, string(e.Data))
	}
}
