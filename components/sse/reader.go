package sse

import (
	"bufio"
	"bytes"
	"io"
	"math"
	"strconv"
	"time"
)

// maxLineSize caps a single line of the stream; a longer line makes Next return
// bufio.ErrTooLong | 单行上限；超过时 Next 返回 bufio.ErrTooLong.
const maxLineSize = 1 << 20

var bom = []byte("\xef\xbb\xbf")

// Reader parses a text/event-stream body into events. It does no I/O of its
// own, never reconnects and is not safe for concurrent use; cancel a read by
// closing the underlying body (for example through a request context).
// Reader 把 text/event-stream 响应体解析成事件。它自己不做 I/O，不会重连，也不是
// 并发安全的；要取消读取，关闭底层响应体即可（例如通过请求的 context）。
type Reader struct {
	sc      *bufio.Scanner
	started bool
	lastID  string
	retry   time.Duration
}

// NewReader returns a Reader over r, typically an http.Response.Body.
// NewReader 返回读取 r 的 Reader，r 通常是 http.Response.Body。
func NewReader(r io.Reader) *Reader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), maxLineSize)
	var skipLF bool
	sc.Split(func(data []byte, atEOF bool) (int, []byte, error) { return splitLines(data, atEOF, &skipLF) })
	return &Reader{sc: sc}
}

// Next returns the next event. It returns io.EOF when the stream ends
// normally; an event still incomplete at that point (no closing blank line) is
// discarded, as the spec requires. Any other error comes from the underlying
// reader, or is bufio.ErrTooLong for an oversized line.
// Next 返回下一个事件。流正常结束时返回 io.EOF；此时尚未完整的事件（缺少结尾空行）
// 按规范丢弃。其他错误来自底层 reader，或是超长行导致的 bufio.ErrTooLong。
//
// Event.Type defaults to "message". Event.ID is this event's own id line (empty
// when it had none); use LastEventID for the resume point.
// Event.Type 缺省为 "message"。Event.ID 是该事件自己的 id 行（没有则为空）；
// 断点请用 LastEventID。
func (r *Reader) Next() (Event, error) {
	var (
		ev      Event
		hasID   bool
		data    []byte
		hasData bool
	)

	for r.sc.Scan() {
		line := r.sc.Bytes()
		if !r.started {
			r.started = true
			line = bytes.TrimPrefix(line, bom)
		}

		if len(line) == 0 { // blank line: dispatch | 空行：分发事件
			if hasID {
				r.lastID = ev.ID
			}
			if !hasData { // nothing to deliver; reset the block | 没有可分发的内容，重置本块
				ev, hasID, data = Event{}, false, nil
				continue
			}
			if ev.Type == "" {
				ev.Type = "message"
			}
			ev.Data = data[:len(data)-1] // drop the trailing LF | 去掉末尾的 LF
			return ev, nil
		}

		name, value, _ := bytes.Cut(line, []byte{':'})
		if len(name) == 0 { // comment line | 注释行
			continue
		}
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}

		switch string(name) {
		case "event":
			ev.Type = string(value)
		case "data":
			data = append(data, value...)
			data = append(data, '\n')
			hasData = true
		case "id":
			if !bytes.Contains(value, []byte{0}) {
				ev.ID = string(value)
				hasID = true
			}
		case "retry":
			if d, ok := parseRetry(value); ok {
				ev.Retry = d
				r.retry = d
			}
		}
	}

	if err := r.sc.Err(); err != nil {
		return Event{}, err
	}
	return Event{}, io.EOF
}

// LastEventID returns the most recent valid id from a completed block,
// including id-only blocks. An empty id clears the resume point.
// LastEventID 返回完整块中最近的有效 id；仅含 id 的块也会更新断点，空 id 会清空断点。
func (r *Reader) LastEventID() string { return r.lastID }

// Retry returns the most recent reconnection delay the server announced, or
// zero. It also captures retry-only blocks, which Next does not deliver as
// events.
// Retry 返回服务器最近一次声明的重连间隔，没有则为零。它也会记录只含 retry 的块，
// 这类块不会被 Next 当作事件返回。
func (r *Reader) Retry() time.Duration { return r.retry }

// parseRetry accepts ASCII digits only, as the spec does | 与规范一致，只接受 ASCII 数字.
func parseRetry(v []byte) (time.Duration, bool) {
	if len(v) == 0 {
		return 0, false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	ms, err := strconv.ParseInt(string(v), 10, 64)
	if err != nil || ms > math.MaxInt64/int64(time.Millisecond) {
		return 0, false
	}
	return time.Duration(ms) * time.Millisecond, true
}

// splitLines is a bufio.SplitFunc that ends a line at LF, CRLF or CR, the three
// line endings the spec allows | 以 LF、CRLF 或 CR 结束一行，即规范允许的三种换行.
func splitLines(data []byte, atEOF bool, skipLF *bool) (advance int, token []byte, err error) {
	if len(data) == 0 {
		return 0, nil, nil
	}
	offset := 0
	if *skipLF {
		*skipLF = false
		if data[0] == '\n' {
			offset = 1
			data = data[1:]
		}
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		n := i + 1
		if data[i] == '\r' {
			if n == len(data) {
				*skipLF = true
			} else if data[n] == '\n' {
				n++
			}
		}
		return offset + n, data[:i], nil
	}
	if atEOF && len(data) > 0 { // unterminated last line | 没有结尾换行的最后一行
		return len(data), data, nil
	}
	return offset, nil, nil
}
