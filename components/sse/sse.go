// Package sse implements Server-Sent Events (text/event-stream) with nothing but
// the standard library | sse 只依赖标准库，实现 Server-Sent Events（text/event-stream）.
//
// # Notes | 说明
//
// The package is deliberately small and framework-neutral: it needs only an
// http.ResponseWriter (server side) or an io.Reader (client side). It does not
// depend on gala's server, does not bind to ServeMux, Gin, Chi or Echo, and
// starts no goroutines — heartbeats, fan-out and reconnection are the caller's
// business.
// 本包刻意保持很小且与框架无关：服务端只需要 http.ResponseWriter，客户端只需要
// io.Reader。不依赖 gala 的 server，不绑定 ServeMux、Gin、Chi、Echo，也不启动任何
// goroutine——心跳、广播、重连都由调用方自己决定。
//
//   - [Writer] (via [NewWriter]) sends events from an HTTP handler.
//     [Writer] 在 HTTP handler 里发送事件。
//   - [Reader] (via [NewReader]) parses a text/event-stream body.
//     [Reader] 解析 text/event-stream 响应体。
//   - [LastEventID] reads the resume point a reconnecting client sends.
//     [LastEventID] 读取重连客户端带来的断点。
//
// # Example | 示例
//
//	func events(w http.ResponseWriter, r *http.Request) {
//		sw, err := sse.NewWriter(w)
//		if err != nil {
//			http.Error(w, err.Error(), http.StatusInternalServerError)
//			return
//		}
//		for i := 0; i < 3; i++ {
//			if err := sw.Send(sse.Event{ID: strconv.Itoa(i), Data: []byte("hello")}); err != nil {
//				return // client gone | 客户端已断开
//			}
//		}
//	}
package sse

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrFlushUnsupported is returned by NewWriter when the ResponseWriter cannot
	// flush, directly or through Unwrap (for example a buffering or compressing
	// wrapper). It wraps http.ErrNotSupported; nothing has been written, so the
	// caller can still send a normal error response.
	// ErrFlushUnsupported 表示 ResponseWriter 无法 Flush（直接或经由 Unwrap 都不行，
	// 例如带缓冲或压缩的包装层）。它包装了 http.ErrNotSupported；此时没有写出任何内容，
	// 调用方仍可返回普通错误响应。
	ErrFlushUnsupported = fmt.Errorf("sse: response does not support flushing: %w", http.ErrNotSupported)

	// ErrInvalidField is returned by Send for an ID or Type that contains a line
	// break (or, for ID, a NUL). Such values would corrupt the stream framing.
	// ErrInvalidField 表示 ID 或 Type 含有换行（ID 还包括 NUL），这会破坏流的分帧。
	ErrInvalidField = errors.New("sse: invalid event field")
)

// Event is one server-sent event | Event 是一个服务端推送事件.
type Event struct {
	// ID sets the last-event-id the client resends on reconnect. Empty means
	// the id line is omitted | 客户端重连时回传的 last-event-id；为空则不写 id 行.
	ID string
	// Type is the event name; empty means the default "message" | 事件名；为空即默认的 "message".
	Type string
	// Data is the payload, UTF-8 text per the spec. Any line break (LF, CRLF or
	// CR) is sent as a separate data line and comes back as LF on the client.
	// 负载（规范要求 UTF-8 文本）。任何换行（LF、CRLF、CR）都会拆成独立的 data 行，
	// 客户端读回时统一为 LF。
	Data []byte
	// Retry is the reconnection delay hint, at millisecond granularity; zero
	// means omitted. An Event holding only Retry sends just the retry line.
	// 重连间隔提示，精度为毫秒；零值表示不发送。只有 Retry 的 Event 只发 retry 行。
	Retry time.Duration
}

// Writer sends events on one HTTP response. It is safe for concurrent use, so
// a heartbeat goroutine may share it with the event loop, but like the
// http.ResponseWriter it wraps it must not be used after the handler returns.
// Writer 在一次 HTTP 响应上发送事件。并发安全，所以心跳 goroutine 可以和事件循环
// 共用一个 Writer；但与它包装的 http.ResponseWriter 一样，handler 返回后不得再使用。
type Writer struct {
	// WriteTimeout bounds each Send or Comment so that a client that stopped
	// reading cannot block the handler forever. Zero means no bound. Set it
	// before the first write; do not change it concurrently.
	// WriteTimeout 限制每次 Send 或 Comment 的写入时长，防止客户端不再读取时把
	// handler 永远阻塞。零值表示不限制。请在首次写入前设置，不要并发修改。
	WriteTimeout time.Duration

	mu  sync.Mutex
	w   http.ResponseWriter
	rc  *http.ResponseController
	err error // sticky: the first write error | 首个写入错误，之后一直返回
}

// NewWriter prepares w for an event stream and commits the response header.
// NewWriter 把 w 准备成事件流，并提交响应头。
//
// It sets Content-Type: text/event-stream, Cache-Control: no-cache and
// X-Accel-Buffering: no (so nginx does not buffer the stream), clears the
// connection's write deadline for this response — otherwise http.Server's
// WriteTimeout would cut a long-lived stream — and flushes the header so the
// client sees the stream open immediately.
// 它设置 Content-Type: text/event-stream、Cache-Control: no-cache 与
// X-Accel-Buffering: no（避免 nginx 缓冲），清除本次响应的写超时（否则
// http.Server 的 WriteTimeout 会切断长连接），并立刻 Flush 响应头，让客户端马上
// 看到流已建立。
//
// Any framework works as long as its writer is an http.ResponseWriter that
// flushes, directly or through Unwrap. Otherwise ErrFlushUnsupported is
// returned.
// 任何框架只要其 writer 是能 Flush 的 http.ResponseWriter（直接实现或经由 Unwrap）
// 即可使用；否则返回 ErrFlushUnsupported。
func NewWriter(w http.ResponseWriter) (*Writer, error) {
	rc := http.NewResponseController(w)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")

	// Writers that cannot set deadlines (httptest recorders, HTTP/2 wrappers)
	// report ErrNotSupported; that is fine, there is simply nothing to clear.
	// 不支持设置期限的 writer（如 httptest recorder）会返回 ErrNotSupported，
	// 这没问题，说明本来就没有需要清除的期限。
	if err := rc.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return nil, fmt.Errorf("sse: clear write deadline: %w", err)
	}

	// Flush commits the 200 header; when unsupported it writes nothing.
	// Flush 会提交 200 响应头；不支持时不会写出任何内容。
	if err := rc.Flush(); err != nil {
		if errors.Is(err, http.ErrNotSupported) {
			return nil, ErrFlushUnsupported
		}
		return nil, fmt.Errorf("sse: flush header: %w", err)
	}
	return &Writer{w: w, rc: rc}, nil
}

// Send writes one event and flushes it. A write error is sticky: once the
// client is gone every later call returns the same error without writing.
// An invalid Event returns ErrInvalidField and leaves the stream untouched.
// Send 写出一个事件并 Flush。写入错误是"粘性"的：客户端断开后，之后每次调用都直接
// 返回同一个错误而不再写入。非法的 Event 返回 ErrInvalidField，不影响流本身。
func (s *Writer) Send(e Event) error {
	frame, err := encode(e)
	if err != nil {
		return err
	}
	return s.write(frame)
}

// SendJSON marshals v with encoding/json/v2 and sends it as a data-only event —
// the shape JSON-over-SSE APIs (LLM streaming, for example) almost always use.
// Need id/event/retry lines? Marshal yourself and call Send. A marshal error
// leaves the stream untouched.
// SendJSON 用 encoding/json/v2 序列化 v 并发送为仅含 data 的事件——JSON-over-SSE
// 接口（如 LLM 流式输出）几乎都是这个形态。需要 id/event/retry 行时请自己序列化
// 再调 Send。序列化失败不影响流。
func (s *Writer) SendJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("sse: marshal event data: %w", err)
	}
	return s.Send(Event{Data: b})
}

// Comment writes a comment line (": text"), which clients ignore. Use it as a
// heartbeat so idle proxies do not drop the connection.
// Comment 写出注释行（": text"），客户端会忽略它。用作心跳，避免空闲时被代理断开。
func (s *Writer) Comment(text string) error {
	buf := appendLines(nil, ": ", []byte(text))
	buf = append(buf, '\n')
	return s.write(buf)
}

func (s *Writer) write(p []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}

	if s.WriteTimeout > 0 {
		if err := s.rc.SetWriteDeadline(time.Now().Add(s.WriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			s.err = fmt.Errorf("sse: set write deadline: %w", err)
			return s.err
		}
		// Clear it again afterwards: an expired deadline would also break the
		// server writing the terminating chunk when the handler returns.
		// 写完再清除：残留的过期期限会让 handler 返回时服务器写结束块也失败。
		defer func() { _ = s.rc.SetWriteDeadline(time.Time{}) }()
	}

	if _, err := s.w.Write(p); err != nil {
		s.err = err
		return err
	}
	if err := s.rc.Flush(); err != nil {
		s.err = err
		return err
	}
	return nil
}

// LastEventID returns the Last-Event-ID header a reconnecting client sends, or
// "" when the client is connecting for the first time.
// LastEventID 返回重连客户端带来的 Last-Event-ID 头；首次连接时为空串。
func LastEventID(r *http.Request) string {
	return r.Header.Get("Last-Event-ID")
}

// encode renders e as one wire frame, terminated by a blank line.
// encode 把 e 渲染成一个以空行结尾的线上帧。
func encode(e Event) ([]byte, error) {
	if strings.ContainsAny(e.ID, "\r\n\x00") {
		return nil, fmt.Errorf("%w: id contains a line break or NUL", ErrInvalidField)
	}
	if strings.ContainsAny(e.Type, "\r\n") {
		return nil, fmt.Errorf("%w: type contains a line break", ErrInvalidField)
	}

	buf := make([]byte, 0, 64+len(e.Data))
	if e.ID != "" {
		buf = append(buf, "id: "...)
		buf = append(buf, e.ID...)
		buf = append(buf, '\n')
	}
	if e.Type != "" {
		buf = append(buf, "event: "...)
		buf = append(buf, e.Type...)
		buf = append(buf, '\n')
	}
	ms := e.Retry.Milliseconds()
	if ms > 0 {
		buf = append(buf, "retry: "...)
		buf = strconv.AppendInt(buf, ms, 10)
		buf = append(buf, '\n')
	}

	retryOnly := ms > 0 && e.ID == "" && e.Type == "" && len(e.Data) == 0
	if !retryOnly {
		buf = appendLines(buf, "data: ", e.Data)
	}
	return append(buf, '\n'), nil
}

// appendLines appends data split on LF, CRLF or CR, one prefixed line each.
// Empty data yields a single empty line, and a trailing line break yields a
// trailing empty line, so the client reconstructs data exactly.
// appendLines 按 LF、CRLF、CR 把 data 拆成行，每行加上 prefix。空数据产生一个空行，
// 末尾的换行产生末尾的空行，所以客户端能原样还原 data。
func appendLines(buf []byte, prefix string, data []byte) []byte {
	for {
		i := bytes.IndexAny(data, "\r\n")
		line := data
		if i >= 0 {
			line = data[:i]
		}
		buf = append(buf, prefix...)
		buf = append(buf, line...)
		buf = append(buf, '\n')
		if i < 0 {
			return buf
		}
		if data[i] == '\r' && i+1 < len(data) && data[i+1] == '\n' {
			i++
		}
		data = data[i+1:]
	}
}
