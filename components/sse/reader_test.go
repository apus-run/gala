package sse_test

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/apus-run/gala/components/sse"
)

// ev is a comparable view of sse.Event | ev 是 sse.Event 的可比较视图.
type ev struct {
	ID, Type, Data string
	Retry          time.Duration
}

func readAll(t *testing.T, r *sse.Reader) []ev {
	t.Helper()
	var out []ev
	for {
		e, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		out = append(out, ev{e.ID, e.Type, string(e.Data), e.Retry})
	}
}

func TestReaderParsing(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []ev
	}{
		{"basic", "data: a\n\n", []ev{{Type: "message", Data: "a"}}},
		{"all fields", "event: tick\nid: 5\nretry: 100\ndata: x\n\n",
			[]ev{{ID: "5", Type: "tick", Data: "x", Retry: 100 * time.Millisecond}}},
		{"multi-line data", "data: a\ndata: b\n\n", []ev{{Type: "message", Data: "a\nb"}}},
		{"comments and unknown fields", ": ping\nfoo: bar\ndata: z\n\n", []ev{{Type: "message", Data: "z"}}},
		{"no space after colon", "data:z\n\n", []ev{{Type: "message", Data: "z"}}},
		{"only one leading space stripped", "data:  z\n\n", []ev{{Type: "message", Data: " z"}}},
		{"field without colon", "data\n\n", []ev{{Type: "message", Data: ""}}},
		{"crlf", "data: a\r\n\r\n", []ev{{Type: "message", Data: "a"}}},
		{"cr", "data: a\r\rdata: b\r\r", []ev{{Type: "message", Data: "a"}, {Type: "message", Data: "b"}}},
		{"mixed endings", "data: a\r\ndata: b\ndata: c\r\r", []ev{{Type: "message", Data: "a\nb\nc"}}},
		{"bom", "\xef\xbb\xbfdata: a\n\n", []ev{{Type: "message", Data: "a"}}},
		{"block without data is skipped and resets type", "event: x\n\ndata: a\n\n", []ev{{Type: "message", Data: "a"}}},
		{"id with NUL ignored", "id: a\x00b\ndata: x\n\n", []ev{{Type: "message", Data: "x"}}},
		{"unterminated final event discarded", "data: a\n\ndata: b", []ev{{Type: "message", Data: "a"}}},
		{"invalid retry ignored", "retry: abc\ndata: x\n\nretry: 12x\ndata: y\n\n",
			[]ev{{Type: "message", Data: "x"}, {Type: "message", Data: "y"}}},
		{"empty stream", "", nil},
	}
	for _, tt := range tests {
		for _, mode := range []struct {
			name string
			wrap func(io.Reader) io.Reader
		}{
			{"whole", func(r io.Reader) io.Reader { return r }},
			// One byte at a time: exercises a CRLF split across reads.
			{"one-byte", iotest.OneByteReader},
		} {
			t.Run(tt.name+"/"+mode.name, func(t *testing.T) {
				got := readAll(t, sse.NewReader(mode.wrap(strings.NewReader(tt.in))))
				if len(got) != len(tt.want) {
					t.Fatalf("got %d events %+v, want %d %+v", len(got), got, len(tt.want), tt.want)
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("event %d = %+v, want %+v", i, got[i], tt.want[i])
					}
				}
			})
		}
	}
}

func TestReaderLastEventIDAndRetry(t *testing.T) {
	in := "id: 1\ndata: a\n\n" + // delivered, carries id
		"data: b\n\n" + //          delivered, no id: lastID must persist
		"retry: 250\n\n" + //       retry-only block: not an event, but remembered
		"id: 9\ndata: c" //         unterminated: discarded, must not move lastID
	r := sse.NewReader(strings.NewReader(in))

	if r.LastEventID() != "" || r.Retry() != 0 {
		t.Fatal("fresh reader must have no state")
	}
	got := readAll(t, r)
	if len(got) != 2 || got[0].ID != "1" || got[1].ID != "" {
		t.Fatalf("events = %+v", got)
	}
	if r.LastEventID() != "1" {
		t.Errorf("LastEventID = %q, want 1", r.LastEventID())
	}
	if r.Retry() != 250*time.Millisecond {
		t.Errorf("Retry = %v, want 250ms", r.Retry())
	}
}

func TestReaderLineTooLong(t *testing.T) {
	in := "data: " + strings.Repeat("a", 2<<20) + "\n\n"
	_, err := sse.NewReader(strings.NewReader(in)).Next()
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("err = %v, want bufio.ErrTooLong", err)
	}
}

func TestReaderPropagatesReadError(t *testing.T) {
	boom := errors.New("connection reset")
	r := sse.NewReader(io.MultiReader(strings.NewReader("data: a\n\ndata: b\n"), iotest.ErrReader(boom)))
	if e, err := r.Next(); err != nil || string(e.Data) != "a" {
		t.Fatalf("first event = %+v, %v", e, err)
	}
	if _, err := r.Next(); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the underlying read error", err)
	}
}

// TestRoundTrip checks that whatever the Writer sends, the Reader gives back.
func TestRoundTrip(t *testing.T) {
	datas := []string{
		"", "x", " leading", "trailing ", "a\nb", "a\n", "\n", "\n\n", "a\r\nb", "a\rb", "\r\n\r\n",
		"中文 ✓", strings.Repeat("long line ", 1000), "data: not a field", ": not a comment", "id: 1",
	}
	for _, d := range datas {
		rec := httptest.NewRecorder()
		w, err := sse.NewWriter(rec)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Send(sse.Event{ID: "42", Type: "kind", Retry: 2 * time.Second, Data: []byte(d)}); err != nil {
			t.Fatal(err)
		}
		wire := rec.Body.Bytes()

		// "\r" is normalized to "\n" on the way through | "\r" 在往返中被统一为 "\n"
		want := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(d)

		for name, src := range map[string]io.Reader{
			"whole":    bytes.NewReader(wire),
			"one-byte": iotest.OneByteReader(bytes.NewReader(wire)),
		} {
			got := readAll(t, sse.NewReader(src))
			if len(got) != 1 {
				t.Fatalf("%q/%s: got %d events: %+v", d, name, len(got), got)
			}
			if got[0].Data != want || got[0].ID != "42" || got[0].Type != "kind" || got[0].Retry != 2*time.Second {
				t.Errorf("%q/%s: got %+v, want data %q", d, name, got[0], want)
			}
		}
	}
}

func TestBareCRDispatchesOnOpenStream(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	done := make(chan string, 1)
	go func() { ev, _ := sse.NewReader(r).Next(); done <- string(ev.Data) }()
	if _, err := io.WriteString(w, "data: x\r\r"); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-done:
		if data != "x" {
			t.Fatalf("got %q", data)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting for bytes after complete event")
	}
}

func TestIDOnlyBlocksSetAndClearResumePoint(t *testing.T) {
	r := sse.NewReader(strings.NewReader("id: 42\n\ndata: first\n\nid:\n\n"))
	if _, err := r.Next(); err != nil || r.LastEventID() != "42" {
		t.Fatalf("resume point %q, err %v", r.LastEventID(), err)
	}
	if _, err := r.Next(); err != io.EOF || r.LastEventID() != "" {
		t.Fatalf("resume point %q, err %v", r.LastEventID(), err)
	}
}
