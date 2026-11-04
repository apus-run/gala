package sse_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/apus-run/gala/components/sse"
)

// startServer runs h on a real http.Server whose WriteTimeout is far shorter
// than the streams below, the way gala's server/http defaults to 30s.
// startServer 用真实的 http.Server 运行 h，WriteTimeout 远短于下面的流，
// 对应 gala server/http 默认的 30 秒写超时。
func startServer(t *testing.T, writeTimeout time.Duration, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(h)
	s.Config.WriteTimeout = writeTimeout
	s.Start()
	t.Cleanup(s.Close)
	return s
}

func TestEndToEnd(t *testing.T) {
	s := startServer(t, 0, func(w http.ResponseWriter, r *http.Request) {
		sw, err := sse.NewWriter(w)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		last := sse.LastEventID(r)
		for i := 1; i <= 3; i++ {
			if err := sw.Send(sse.Event{ID: strconv.Itoa(i), Type: "tick", Data: []byte("last=" + last)}); err != nil {
				return
			}
		}
	})

	req, _ := http.NewRequest(http.MethodGet, s.URL, nil)
	req.Header.Set("Last-Event-ID", "9")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	r := sse.NewReader(resp.Body)
	for i := 1; i <= 3; i++ {
		e, err := r.Next()
		if err != nil {
			t.Fatalf("event %d: %v", i, err)
		}
		if e.ID != strconv.Itoa(i) || e.Type != "tick" || string(e.Data) != "last=9" {
			t.Fatalf("event %d = %+v", i, e)
		}
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("after last event: %v, want io.EOF", err)
	}
	if r.LastEventID() != "3" {
		t.Fatalf("LastEventID = %q, want 3", r.LastEventID())
	}
}

// TestWriteTimeoutDoesNotCutStream proves NewWriter clears http.Server's
// WriteTimeout for the stream, and that a plain Flush-based handler without it
// is cut, which is why NewWriter does this.
// 证明 NewWriter 会为这条流清除 http.Server 的 WriteTimeout；并用对照组说明：
// 只做 Flush 而不清除期限的 handler 会被切断，这正是 NewWriter 要这么做的原因。
func TestWriteTimeoutDoesNotCutStream(t *testing.T) {
	const serverTimeout, idle = 150 * time.Millisecond, 400 * time.Millisecond
	client := &http.Client{Timeout: 5 * time.Second}

	t.Run("with sse.Writer the late event arrives", func(t *testing.T) {
		s := startServer(t, serverTimeout, func(w http.ResponseWriter, r *http.Request) {
			sw, err := sse.NewWriter(w)
			if err != nil {
				return
			}
			time.Sleep(idle)
			_ = sw.Send(sse.Event{Data: []byte("late")})
		})
		resp, err := client.Get(s.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		e, err := sse.NewReader(resp.Body).Next()
		if err != nil || string(e.Data) != "late" {
			t.Fatalf("got (%+v, %v), want the late event", e, err)
		}
	})

	t.Run("control: plain Flush loses it", func(t *testing.T) {
		s := startServer(t, serverTimeout, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			rc := http.NewResponseController(w)
			_ = rc.Flush()
			time.Sleep(idle)
			_, _ = w.Write([]byte("data: late\n\n"))
			_ = rc.Flush()
		})
		resp, err := client.Get(s.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if e, err := sse.NewReader(resp.Body).Next(); err == nil {
			t.Fatalf("got event %+v, expected the stream to be cut by WriteTimeout", e)
		}
	})
}

func TestHandlerStopsWhenClientLeaves(t *testing.T) {
	done := make(chan error, 1)
	s := startServer(t, 0, func(w http.ResponseWriter, r *http.Request) {
		sw, err := sse.NewWriter(w)
		if err != nil {
			done <- err
			return
		}
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-r.Context().Done():
				done <- nil
				return
			case <-tick.C:
				if err := sw.Comment("hb"); err != nil {
					done <- nil // write failure is also a valid exit | 写入失败同样是合法的退出方式
					return
				}
			}
		}
	})

	resp, err := http.Get(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close() // the client walks away | 客户端离开

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not stop after the client disconnected")
	}
}
