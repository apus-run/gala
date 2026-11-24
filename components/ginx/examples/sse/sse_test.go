package sse_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	example "github.com/apus-run/gala/components/ginx/examples/sse"
	"github.com/apus-run/gala/components/sse"
)

// collect returns the raw body and the events a client would read from it.
// collect 返回原始响应体，以及客户端会读到的事件。
func collect(t *testing.T, target string, headers map[string]string) (string, []sse.Event) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	example.New().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("want SSE content type, got %q", got)
	}
	body := w.Body.String()
	reader := sse.NewReader(strings.NewReader(body))
	var events []sse.Event
	for {
		event, err := reader.Next()
		if err != nil {
			break
		}
		events = append(events, event)
	}
	return body, events
}

func TestStreamsEvents(t *testing.T) {
	body, events := collect(t, "/events/lobby?count=2", nil)

	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d", len(events))
	}
	for i, want := range []string{"lobby-0", "lobby-1"} {
		if string(events[i].Data) != want {
			t.Fatalf("event %d: want %q, got %q", i, want, events[i].Data)
		}
		if events[i].Type != "tick" {
			t.Fatalf("event %d: want type tick, got %q", i, events[i].Type)
		}
		if events[i].ID != strconv.Itoa(i) {
			t.Fatalf("event %d: want id %d, got %q", i, i, events[i].ID)
		}
	}
	// The comment frame is sent before the first event, and clients skip it.
	// 首个事件前会发一帧注释，客户端会跳过它。
	if !strings.Contains(body, ": stream open\n") {
		t.Fatalf("missing open comment:\n%s", body)
	}
	// The wrapper must not append a JSON envelope after the stream.
	// 包装器不得在流后面追加 JSON 响应体。
	if strings.Contains(body, `"code"`) {
		t.Fatalf("JSON envelope leaked into the stream:\n%s", body)
	}
}

func TestDefaultCountAndRoom(t *testing.T) {
	// delay=1 exercises the pacing branch without slowing the test noticeably.
	// delay=1 覆盖节流分支，测试耗时几乎不变。
	_, events := collect(t, "/events?delay=1", nil)
	if len(events) != 3 {
		t.Fatalf("default count should be 3, got %d", len(events))
	}
	if string(events[0].Data) != "default-0" {
		t.Fatalf("default room should be used, got %q", events[0].Data)
	}
}

func TestRoomCookieFillsInForThePathParameter(t *testing.T) {
	_, events := collect(t, "/events?count=1", map[string]string{"Cookie": "room=kitchen"})
	if len(events) != 1 || string(events[0].Data) != "kitchen-0" {
		t.Fatalf("cookie room should be used, got %+v", events)
	}
}

func TestResumesAfterLastEventID(t *testing.T) {
	_, events := collect(t, "/events/lobby?count=2", map[string]string{"Last-Event-ID": "5"})
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d", len(events))
	}
	if events[0].ID != "6" || string(events[0].Data) != "lobby-6" {
		t.Fatalf("resume should start at 6, got id=%q data=%q", events[0].ID, events[0].Data)
	}
	if events[1].ID != "7" {
		t.Fatalf("second resumed event should be 7, got %q", events[1].ID)
	}
}

func TestInvalidEventIDFallsBackToZero(t *testing.T) {
	_, events := collect(t, "/events/lobby?count=1", map[string]string{"Last-Event-ID": "not-a-number"})
	if len(events) != 1 || events[0].ID != "0" {
		t.Fatalf("invalid Last-Event-ID should restart at 0, got %+v", events)
	}
}

func TestInvalidCountFallsBackToDefault(t *testing.T) {
	_, events := collect(t, "/events/lobby?count=abc", nil)
	if len(events) != 3 {
		t.Fatalf("invalid count should fall back to 3, got %d", len(events))
	}
}
