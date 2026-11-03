package fileservertest

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/apus-run/gala/components/fileserver"
)

// FailAfter returns a reader that yields the first n bytes of r normally and
// then returns err. Used to simulate an input that fails midway.
// FailAfter 返回一个读取器：先正常产出 r 的前 n 字节，随后返回 err。
// 用于模拟输入中途失败。
func FailAfter(r io.Reader, n int64, err error) io.Reader {
	return &failingReader{r: r, remaining: n, err: err}
}

type failingReader struct {
	r         io.Reader
	remaining int64
	err       error
}

// Read yields bytes until the limit, then reports the failure | Read 先正常产出字节，到达上限后返回错误.
func (f *failingReader) Read(p []byte) (int, error) {
	if f.remaining <= 0 {
		return 0, f.err
	}
	if int64(len(p)) > f.remaining {
		p = p[:f.remaining]
	}
	n, err := f.r.Read(p)
	f.remaining -= int64(n)
	return n, err
}

// BlockingReader signals Started on the first Read and blocks until Release
// is called; afterwards it reads the underlying Reader transparently. Used to
// build "call in progress" concurrency scenarios. The test must guarantee
// Release is called, otherwise the blocked Read never returns.
// BlockingReader 在第一次 Read 时发出 Started 信号并阻塞，直到 Release 被调用；
// 之后透明地读取底层 Reader。用于构造"调用正在进行中"的并发场景。
// 测试必须保证 Release 被调用，否则阻塞的 Read 永不返回。
type BlockingReader struct {
	r         io.Reader
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	relOnce   sync.Once
}

// NewBlockingReader wraps r.
// NewBlockingReader 包装 r。
func NewBlockingReader(r io.Reader) *BlockingReader {
	return &BlockingReader{r: r, started: make(chan struct{}), release: make(chan struct{})}
}

// Started is closed when the first Read begins.
// Started 在第一次 Read 开始时关闭。
func (b *BlockingReader) Started() <-chan struct{} { return b.started }

// Release unblocks; it may be called multiple times.
// Release 解除阻塞，可以多次调用。
func (b *BlockingReader) Release() {
	b.relOnce.Do(func() { close(b.release) })
}

// Read implements io.Reader.
// Read 实现 io.Reader。
func (b *BlockingReader) Read(p []byte) (int, error) {
	b.startOnce.Do(func() { close(b.started) })
	<-b.release
	return b.r.Read(p)
}

// TrackedReader records whether it has been Close-d, to verify that Upload
// does not close its input.
// TrackedReader 记录是否被 Close，用于验证 Upload 不关闭输入。
type TrackedReader struct {
	io.Reader
	mu     sync.Mutex
	closed bool
}

// Close records the close event.
// Close 记录关闭事件。
func (t *TrackedReader) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return nil
}

// Closed reports whether Close has been called.
// Closed 报告 Close 是否被调用过。
func (t *TrackedReader) Closed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

// CancelAfterReader calls cancel after the first Read returns data, to
// simulate cancellation during a read.
// CancelAfterReader 在第一次 Read 返回数据后调用 cancel，用于模拟读取过程中取消。
type CancelAfterReader struct {
	// R is the underlying reader | 底层读取器。
	R io.Reader
	// Cancel is invoked after the first successful read | 第一次读到数据后调用。
	Cancel context.CancelFunc
	once   sync.Once
}

// Read implements io.Reader.
// Read 实现 io.Reader。
func (c *CancelAfterReader) Read(p []byte) (int, error) {
	n, err := c.R.Read(p)
	if n > 0 {
		c.once.Do(c.Cancel)
	}
	return n, err
}

// BufferAt is an in-memory io.WriterAt for collecting Download output. The
// zero value is usable.
// BufferAt 是内存中的 io.WriterAt，用于收集 Download 的输出。零值可用。
type BufferAt struct {
	mu  sync.Mutex
	buf []byte
}

// WriteAt implements io.WriterAt; a write past the current length grows the
// buffer and the hole is zero-filled.
// WriteAt 实现 io.WriterAt，超出当前长度的部分会扩展缓冲区，空洞填零。
func (b *BufferAt) WriteAt(p []byte, off int64) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	end := int(off) + len(p)
	if end > len(b.buf) {
		grown := make([]byte, end)
		copy(grown, b.buf)
		b.buf = grown
	}
	return copy(b.buf[off:], p), nil
}

// Bytes returns a copy of what has been written.
// Bytes 返回已写入内容的副本。
func (b *BufferAt) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buf)
}

// ProgressRecorder records every progress notification; safe for concurrent
// use.
// ProgressRecorder 记录所有进度通知，可安全并发使用。
type ProgressRecorder struct {
	mu      sync.Mutex
	reports []fileserver.ProgressInfo
}

// Report implements fileserver.Progress.
// Report 实现 fileserver.Progress。
func (r *ProgressRecorder) Report(_ context.Context, p fileserver.ProgressInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reports = append(r.reports, p)
}

// Reports returns a copy of the recorded notifications.
// Reports 返回已记录通知的副本。
func (r *ProgressRecorder) Reports() []fileserver.ProgressInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]fileserver.ProgressInfo, len(r.reports))
	copy(out, r.reports)
	return out
}
