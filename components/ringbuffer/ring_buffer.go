// Copyright 2019 smallnest. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// Package ringbuffer provides a fixed-size, concurrency-safe circular byte
// buffer that implements io.Reader and io.Writer | 定长、并发安全的环形字节缓冲，
// 实现 io.Reader / io.Writer.
//
// # Notes | 说明
//
// Based on smallnest/RingBuffer (MIT). Capacity is fixed at New; a single
// Write larger than capacity fails with ErrTooMuchDataToWrite unless
// overwrite mode is on, which keeps the newest bytes and drops the oldest.
// The default mode is non-blocking (ErrIsEmpty / ErrIsFull); SetBlocking makes
// Read and Write wait, and the With*Timeout / WithCancel options close the
// whole buffer on timeout or context cancellation. ReadFrom, WriteTo and Pipe
// work only in blocking mode.
// 基于 smallnest/RingBuffer（MIT）。容量在 New 时固定；单次写入超过容量返回
// ErrTooMuchDataToWrite，开启 overwrite 模式则保留最新数据、丢弃最旧数据。默认
// 非阻塞（空返回 ErrIsEmpty，满返回 ErrIsFull）；SetBlocking 后 Read/Write 会
// 等待，With*Timeout / WithCancel 在超时或 ctx 取消时关闭整个缓冲。ReadFrom、
// WriteTo 与 Pipe 仅阻塞模式可用。
//
// # Features | 功能
//
// - Blocking, non-blocking and overwrite modes | 阻塞、非阻塞与覆盖三种模式
// - TryRead / TryWrite never wait for the lock | TryRead / TryWrite 不等待锁
// - Peek and Bytes inspect without moving the read pointer | Peek / Bytes 不移动读指针
// - Pipe returns io.Pipe-compatible halves | Pipe 返回兼容 io.Pipe 的两端
// - ReadFrom / WriteTo / Copy move data without extra copies | ReadFrom / WriteTo / Copy 搬运数据零额外拷贝
//
// # Example | 示例
//
//	rb := ringbuffer.New(4096)
//	go func() { _, _ = rb.WriteString("hello") }()
//	buf := make([]byte, 5)
//	_, _ = rb.Read(buf)
package ringbuffer

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
	"unsafe"
)

var (
	// ErrTooMuchDataToWrite is returned when the data to write is more than the buffer size.
	// 待写入数据超过缓冲区大小时返回 ErrTooMuchDataToWrite.
	ErrTooMuchDataToWrite = errors.New("too much data to write")

	// ErrIsFull is returned when the buffer is full and not blocking.
	// 缓冲已满且非阻塞模式时返回 ErrIsFull.
	ErrIsFull = errors.New("ringbuffer is full")

	// ErrIsEmpty is returned when the buffer is empty and not blocking.
	// 缓冲为空且非阻塞模式时返回 ErrIsEmpty.
	ErrIsEmpty = errors.New("ringbuffer is empty")

	// ErrIsNotEmpty is returned when the buffer is not empty and not blocking.
	// 缓冲非空且非阻塞模式时返回 ErrIsNotEmpty.
	ErrIsNotEmpty = errors.New("ringbuffer is not empty")

	// ErrAcquireLock is returned when the lock is not acquired on Try operations.
	// Try 系列操作未获取到锁时返回 ErrAcquireLock.
	ErrAcquireLock = errors.New("unable to acquire lock")

	// ErrWriteOnClosed is returned when write on a closed ringbuffer.
	// 向已关闭的 ringbuffer 写入时返回 ErrWriteOnClosed.
	ErrWriteOnClosed = errors.New("write on closed ringbuffer")

	// ErrReaderClosed is returned when a ReadClosed closed the ringbuffer.
	// 读端关闭 ringbuffer 时返回 ErrReaderClosed.
	ErrReaderClosed = errors.New("reader closed")

	// ErrReset is returned when Reset() is called, causing pending operations to abort.
	// 调用 Reset() 使未完成操作中止时返回 ErrReset.
	ErrReset = errors.New("reset called")
)

// RingBuffer is a circular buffer that implements io.ReaderWriter interface.
// It operates like a buffered pipe, where data is written to a RingBuffer
// and can be read back from another goroutine.
// It is safe to concurrently read and write RingBuffer.
// RingBuffer 是实现 io.ReaderWriter 接口的环形缓冲，行为类似带缓冲的管道：数据写入
// RingBuffer，可由另一个 goroutine 读回。并发读写 RingBuffer 是安全的.
type RingBuffer struct {
	buf        []byte
	size       int
	r          int // next position to read | 下一个读取位置
	w          int // next position to write | 下一个写入位置
	isFull     bool
	err        error
	block      bool
	overwrite  bool          // when true, overwrite old data when buffer is full | 为 true 时缓冲满则覆盖旧数据
	rTimeout   time.Duration // Applies to writes (waits for the read condition) | 作用于写（等待读条件）
	wTimeout   time.Duration // Applies to read (wait for the write condition) | 作用于读（等待写条件）
	mu         sync.Mutex
	wg         sync.WaitGroup
	readCond   *sync.Cond // Signaled when data has been read. | 数据被读走时唤醒
	writeCond  *sync.Cond // Signaled when data has been written. | 数据被写入时唤醒
	generation int64      // Incremented on Reset() to invalidate current waiters | Reset() 时递增，使当前等待者失效
}

// New returns a new RingBuffer whose buffer has the given size | New 返回指定大小的 RingBuffer.
func New(size int) *RingBuffer {
	return &RingBuffer{
		buf:  make([]byte, size),
		size: size,
	}
}

// NewBuffer returns a new RingBuffer whose buffer is provided | NewBuffer 用给定缓冲返回 RingBuffer.
func NewBuffer(b []byte) *RingBuffer {
	return &RingBuffer{
		buf:  b,
		size: len(b),
	}
}

// SetBlocking sets the blocking mode of the ring buffer.
// If block is true, Read and Write will block when there is no data to read or no space to write.
// If block is false, Read and Write will return ErrIsEmpty or ErrIsFull immediately.
// By default, the ring buffer is not blocking.
// This setting should be called before any Read or Write operation or after a Reset.
// SetBlocking 设置阻塞模式。block 为 true 时，无数据可读或无空间可写时 Read/Write
// 阻塞；为 false 时立即返回 ErrIsEmpty 或 ErrIsFull。默认非阻塞。应在任何
// Read/Write 之前或 Reset 之后调用.
func (r *RingBuffer) SetBlocking(block bool) *RingBuffer {
	r.block = block
	if block {
		r.readCond = sync.NewCond(&r.mu)
		r.writeCond = sync.NewCond(&r.mu)
	}
	return r
}

// SetOverwrite sets the overwrite mode of the ring buffer.
// If overwrite is true, Write operations will overwrite the oldest data when the buffer is full,
// similar to a traditional circular buffer. The read pointer will advance to skip overwritten data.
// If overwrite is false (default), Write will return ErrIsFull or block (if blocking mode is enabled).
// SetOverwrite 设置覆盖模式。overwrite 为 true 时，缓冲满则写入覆盖最旧的数据（类似传统
// 环形缓冲），读指针前移跳过被覆盖数据；为 false（默认）时 Write 返回 ErrIsFull 或阻塞
// （若开启阻塞模式）.
func (r *RingBuffer) SetOverwrite(overwrite bool) *RingBuffer {
	r.overwrite = overwrite
	return r
}

// WithCancel sets a context to cancel the ring buffer.
// When the context is canceled, the ring buffer will be closed with the context error.
// A goroutine will be started and run until the provided context is canceled.
// WithCancel 设置用于取消 ring buffer 的 ctx。ctx 取消时缓冲以该错误关闭。
// 会启动一个 goroutine 运行到 ctx 取消为止.
func (r *RingBuffer) WithCancel(ctx context.Context) *RingBuffer {
	go func() {
		<-ctx.Done()
		r.CloseWithError(ctx.Err())
	}()
	return r
}

// WithTimeout will set a blocking read/write timeout.
// If no reads or writes occur within the timeout,
// the ringbuffer will be closed and context.DeadlineExceeded will be returned.
// A timeout of 0 or less will disable timeouts (default).
// WithTimeout 设置阻塞读写的超时。超时内没有读或写发生时，缓冲被关闭并返回
// context.DeadlineExceeded。超时为 0 或负数表示禁用（默认）.
func (r *RingBuffer) WithTimeout(d time.Duration) *RingBuffer {
	r.mu.Lock()
	r.rTimeout = d
	r.wTimeout = d
	r.mu.Unlock()
	return r
}

// WithReadTimeout will set a blocking read timeout.
// Reads refers to any call that reads data from the buffer.
// If no writes occur within the timeout,
// the ringbuffer will be closed and context.DeadlineExceeded will be returned.
// A timeout of 0 or less will disable timeouts (default).
// WithReadTimeout 设置阻塞读超时。Read 指任何从缓冲读数据的调用。超时内没有写发生时，
// 缓冲被关闭并返回 context.DeadlineExceeded。超时为 0 或负数表示禁用（默认）.
func (r *RingBuffer) WithReadTimeout(d time.Duration) *RingBuffer {
	r.mu.Lock()
	// Read operations wait for writes to complete,
	// therefore we set the wTimeout.
	// 读操作等待写完成，因此设置的是 wTimeout.
	r.wTimeout = d
	r.mu.Unlock()
	return r
}

// WithWriteTimeout will set a blocking write timeout.
// Write refers to any call that writes data into the buffer.
// If no reads occur within the timeout,
// the ringbuffer will be closed and context.DeadlineExceeded will be returned.
// A timeout of 0 or less will disable timeouts (default).
// WithWriteTimeout 设置阻塞写超时。Write 指任何向缓冲写数据的调用。超时内没有读发生时，
// 缓冲被关闭并返回 context.DeadlineExceeded。超时为 0 或负数表示禁用（默认）.
func (r *RingBuffer) WithWriteTimeout(d time.Duration) *RingBuffer {
	r.mu.Lock()
	// Write operations wait for reads to complete,
	// therefore we set the rTimeout.
	// 写操作等待读完成，因此设置的是 rTimeout.
	r.rTimeout = d
	r.mu.Unlock()
	return r
}

func (r *RingBuffer) setErr(err error, locked bool) error {
	if !locked {
		r.mu.Lock()
		defer r.mu.Unlock()
	}
	if r.err != nil && r.err != io.EOF {
		return r.err
	}

	switch err {
	// Internal errors are transient
	// 内部错误是瞬态的
	case nil, ErrIsEmpty, ErrIsFull, ErrAcquireLock, ErrTooMuchDataToWrite, ErrIsNotEmpty:
		return err
	default:
		r.err = err
		if r.block {
			r.readCond.Broadcast()
			r.writeCond.Broadcast()
		}
	}
	return err
}

func (r *RingBuffer) readErr(locked bool) error {
	if !locked {
		r.mu.Lock()
		defer r.mu.Unlock()
	}
	if r.err != nil {
		if r.err == io.EOF {
			if r.w == r.r && !r.isFull {
				return io.EOF
			}
			return nil
		}
		return r.err
	}
	return nil
}

// Read reads up to len(p) bytes into p. It returns the number of bytes read (0 <= n <= len(p)) and any error encountered.
// Even if Read returns n < len(p), it may use all of p as scratch space during the call.
// If some data is available but not len(p) bytes, Read conventionally returns what is available instead of waiting for more.
// When Read encounters an error or end-of-file condition after successfully reading n > 0 bytes, it returns the number of bytes read.
// It may return the (non-nil) error from the same call or return the error (and n == 0) from a subsequent call.
// Callers should always process the n > 0 bytes returned before considering the error err.
// Doing so correctly handles I/O errors that happen after reading some bytes and also both of the allowed EOF behaviors.
// Read 将最多 len(p) 字节读入 p，返回读取的字节数与遇到的错误。有数据但不足 len(p) 时
// 按惯例返回现有数据而不等待更多；读到 n>0 字节后遇到错误时返回该字节数，错误可能在
// 同一次或下一次调用返回；调用方应先处理 n>0 的字节再考虑错误.
func (r *RingBuffer) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, r.readErr(false)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.readErr(true); err != nil {
		return 0, err
	}

	r.wg.Add(1)
	defer r.wg.Done()
	n, err = r.read(p)
	for err == ErrIsEmpty && r.block {
		// Record the generation before waiting. If Reset() increments the generation,
		// we will detect it after waking up and continue waiting (as if nothing happened).
		// 等待前记录 generation。若 Reset() 递增了 generation，醒来后会发现并继续等待
		// （如同什么都没发生）.
		myGen := r.generation
		if !r.waitWrite() {
			return 0, context.DeadlineExceeded
		}
		// If generation changed, Reset() happened while we were waiting.
		// Go back to waiting - the buffer was reset and we should continue as normal.
		// generation 变化说明等待期间发生了 Reset()。回到等待——缓冲已被重置，按正常流程继续.
		if myGen != r.generation {
			continue
		}
		if err = r.readErr(true); err != nil {
			break
		}
		n, err = r.read(p)
	}
	if r.block && n > 0 {
		r.readCond.Broadcast()
	}
	return n, err
}

// TryRead read up to len(p) bytes into p like Read, but it is never blocking.
// If it does not succeed to acquire the lock, it returns ErrAcquireLock.
// TryRead 像 Read 一样把最多 len(p) 字节读入 p，但从不阻塞。未获取到锁时返回
// ErrAcquireLock.
func (r *RingBuffer) TryRead(p []byte) (n int, err error) {
	ok := r.mu.TryLock()
	if !ok {
		return 0, ErrAcquireLock
	}
	defer r.mu.Unlock()
	if err := r.readErr(true); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, r.readErr(true)
	}

	n, err = r.read(p)
	if r.block && n > 0 {
		r.readCond.Broadcast()
	}
	return n, err
}

// copyFromBuffer copies data from the ring buffer to dst without modifying the read pointer.
// Returns the number of bytes copied. Does not check for errors.
// copyFromBuffer 把数据从环形缓冲复制到 dst，不移动读指针。返回复制的字节数，不检查错误.
func (r *RingBuffer) copyFromBuffer(dst []byte) int {
	if r.w == r.r && !r.isFull {
		return 0
	}

	var n int
	if r.w > r.r {
		n = min(r.w-r.r, len(dst))
		copy(dst, r.buf[r.r:r.r+n])
		return n
	}

	n = min(r.size-r.r+r.w, len(dst))

	if r.r+n <= r.size {
		copy(dst, r.buf[r.r:r.r+n])
	} else {
		c1 := r.size - r.r
		copy(dst, r.buf[r.r:r.size])
		c2 := n - c1
		copy(dst[c1:], r.buf[0:c2])
	}
	return n
}

func (r *RingBuffer) read(p []byte) (n int, err error) {
	if r.w == r.r && !r.isFull {
		return 0, ErrIsEmpty
	}

	n = r.copyFromBuffer(p)
	if n == 0 {
		return 0, ErrIsEmpty
	}

	r.r = (r.r + n) % r.size
	r.isFull = false

	return n, r.readErr(true)
}

// Returns true if a read may have happened.
// Returns false if waited longer than rTimeout.
// Must be called when locked and returns locked.
// 若可能发生了读则返回 true；等待超过 rTimeout 返回 false。必须在持锁时调用，返回时仍持锁.
func (r *RingBuffer) waitRead() (ok bool) {
	if r.rTimeout <= 0 {
		r.readCond.Wait()
		return true
	}
	start := time.Now()
	defer time.AfterFunc(r.rTimeout, r.readCond.Broadcast).Stop()

	r.readCond.Wait()
	if time.Since(start) >= r.rTimeout {
		r.setErr(context.DeadlineExceeded, true) //nolint errcheck
		return false
	}
	return true
}

// ReadByte reads and returns the next byte from the input or ErrIsEmpty | ReadByte 读取并返回下一个字节，缓冲为空时返回 ErrIsEmpty.
func (r *RingBuffer) ReadByte() (b byte, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err = r.readErr(true); err != nil {
		return 0, err
	}
	for r.w == r.r && !r.isFull {
		if r.block {
			// Record the generation before waiting. If Reset() increments the generation,
			// we will detect it after waking up and continue waiting.
			// 等待前记录 generation。若 Reset() 递增了 generation，醒来后会发现并继续等待.
			myGen := r.generation
			if !r.waitWrite() {
				return 0, context.DeadlineExceeded
			}
			// If generation changed, Reset() happened while we were waiting.
			// Go back to waiting - the buffer was reset.
			// generation 变化说明等待期间发生了 Reset()。回到等待——缓冲已被重置.
			if myGen != r.generation {
				continue
			}
			err = r.readErr(true)
			if err != nil {
				return 0, err
			}
			continue
		}
		return 0, ErrIsEmpty
	}
	b = r.buf[r.r]
	r.r++
	if r.r == r.size {
		r.r = 0
	}

	r.isFull = false
	return b, r.readErr(true)
}

// checkWriteErr checks if the buffer has an error that should prevent writes.
// Returns the appropriate error to return (nil if write can proceed).
// Must be called with mutex held.
// checkWriteErr 检查缓冲是否有应阻止写入的错误。返回应返回的错误（可写则为 nil）。
// 必须持锁调用.
func (r *RingBuffer) checkWriteErr() error {
	if r.err == nil {
		return nil
	}
	if r.err == io.EOF {
		return ErrWriteOnClosed
	}
	return r.err
}

// Write writes len(p) bytes from p to the underlying buf.
// It returns the number of bytes written from p (0 <= n <= len(p))
// and any error encountered that caused the write to stop early.
// If blocking n < len(p) will be returned only if an error occurred.
// Write returns a non-nil error if it returns n < len(p).
// Write will not modify the slice data, even temporarily.
// Write 将 p 的 len(p) 字节写入底层缓冲，返回写入字节数与导致提前停止的错误。
// 仅在出错时才会返回 n < len(p)，且此时返回非 nil 错误；Write 即使临时也不修改切片数据.
func (r *RingBuffer) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, r.setErr(nil, false)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkWriteErr(); err != nil {
		return 0, err
	}
	wrote := 0
	for len(p) > 0 {
		n, err = r.write(p)
		wrote += n
		if !r.block || err == nil {
			break
		}
		err = r.setErr(err, true)
		if r.block && (err == ErrIsFull || err == ErrTooMuchDataToWrite) {
			// Record the generation before waiting. If Reset() increments the generation,
			// we will detect it after waking up and continue waiting.
			// 等待前记录 generation。若 Reset() 递增了 generation，醒来后会发现并继续等待.
			myGen := r.generation
			r.writeCond.Broadcast()
			r.waitRead()
			// If generation changed, Reset() happened while we were waiting.
			// Go back to waiting - the buffer was reset.
			// generation 变化说明等待期间发生了 Reset()。回到等待——缓冲已被重置.
			if myGen != r.generation {
				p = p[n:]
				err = nil
				continue
			}
			p = p[n:]
			err = nil
			continue
		}
		break
	}
	if r.block && wrote > 0 {
		r.writeCond.Broadcast()
	}

	return wrote, r.setErr(err, true)
}

// waitWrite will wait for a write event.
// Returns true if a write may have happened.
// Returns false if waited longer than wTimeout.
// Must be called when locked and returns locked.
// waitWrite 等待写事件。若可能发生了写则返回 true；等待超过 wTimeout 返回 false。
// 必须在持锁时调用，返回时仍持锁.
func (r *RingBuffer) waitWrite() (ok bool) {
	if r.wTimeout <= 0 {
		r.writeCond.Wait()
		return true
	}

	start := time.Now()
	defer time.AfterFunc(r.wTimeout, r.writeCond.Broadcast).Stop()

	r.writeCond.Wait()
	if time.Since(start) >= r.wTimeout {
		r.setErr(context.DeadlineExceeded, true) //nolint errcheck
		return false
	}
	return true
}

// ReadFrom will fulfill the write side of the ringbuffer.
// This will do writes directly into the buffer,
// therefore avoiding a mem-copy when using the Write.
//
// ReadFrom will not automatically close the buffer even after returning.
// For that call CloseWriter().
//
// ReadFrom reads data from r until EOF or error.
// The return value n is the number of bytes read.
// Any error except EOF encountered during the read is also returned,
// and the error will cause the Read side to fail as well.
// ReadFrom only available in blocking mode.
// ReadFrom 满足 ringbuffer 的写侧，直接写入底层缓冲，避免 Write 的内存拷贝。
// 返回后不会自动关闭缓冲，需要时调用 CloseWriter()。它从 r 读到 EOF 或错误为止，
// n 为读取字节数；除 EOF 外的错误也会返回，且会导致读侧同样失败。
// ReadFrom 仅在阻塞模式可用.
func (r *RingBuffer) ReadFrom(rd io.Reader) (n int64, err error) {
	if !r.block {
		return 0, errors.New("RingBuffer: ReadFrom only available in blocking mode")
	}
	zeroReads := 0
	r.mu.Lock()
	defer r.mu.Unlock()
	for {
		if err = r.readErr(true); err != nil {
			return n, err
		}
		if r.isFull {
			// Wait for a read
			// 等待一次读
			if !r.waitRead() {
				return 0, context.DeadlineExceeded
			}
			continue
		}

		// Calculate available space to read into
		// 计算可写入的空间
		var toRead []byte
		if r.w >= r.r {
			// After reader, read until end of buffer
			// 读指针之后：读到缓冲末尾
			toRead = r.buf[r.w:]
		} else {
			// Before reader, read until reader.
			// 读指针之前：读到读指针处
			if r.w >= r.size || r.r > r.size {
				// Pointers are corrupted, return error to prevent panic
				// 指针已损坏，返回错误以防 panic
				return n, errors.New("RingBuffer: internal state corrupted")
			}
			toRead = r.buf[r.w:r.r]
		}

		nr, rerr := rd.Read(toRead)
		if rerr != nil && rerr != io.EOF {
			err = r.setErr(rerr, true)
			break
		}
		if nr == 0 && rerr == nil {
			zeroReads++
			if zeroReads >= 100 {
				err = r.setErr(io.ErrNoProgress, true)
			}
			continue
		}
		zeroReads = 0

		// Update write pointer with proper wrap-around using modulo
		// 用取模正确回绕地更新写指针
		r.w = (r.w + nr) % r.size
		r.isFull = r.r == r.w && nr > 0
		n += int64(nr)
		r.writeCond.Broadcast()
		if rerr == io.EOF {
			// We do not close.
			// 这里不主动关闭.
			break
		}
	}
	return n, err
}

// WriteTo writes data to w until there's no more data to write or
// when an error occurs. The return value n is the number of bytes
// written. Any error encountered during the write is also returned.
//
// If a non-nil error is returned the write side will also see the error.
// WriteTo 把数据写入 w，直到无数据可写或发生错误。n 为写入字节数，写入期间遇到的
// 错误也会返回。返回非 nil 错误时写侧同样能看到该错误.
func (r *RingBuffer) WriteTo(w io.Writer) (n int64, err error) {
	if !r.block {
		return 0, errors.New("RingBuffer: WriteTo only available in blocking mode")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	// Don't write more than half, to unblock reads earlier.
	// 单次不超过一半，让读更早解除阻塞
	maxWrite := len(r.buf) / 2
	// But write at least 8K if possible
	// 但尽可能至少写 8K
	if maxWrite < 8<<10 {
		maxWrite = len(r.buf)
	}
	for {
		if err = r.readErr(true); err != nil {
			break
		}
		if r.r == r.w && !r.isFull {
			// Wait for a write to make space
			// 等待写腾出空间
			if !r.waitWrite() {
				return 0, context.DeadlineExceeded
			}
			continue
		}

		var toWrite []byte
		if r.r >= r.w {
			// After writer, we can write until end of buffer
			// 写指针之后：可写到缓冲末尾
			toWrite = r.buf[r.r:]
		} else {
			// Before reader, we can read until writer.
			// 读指针之前：可读到写指针处
			toWrite = r.buf[r.r:r.w]
		}
		if len(toWrite) > maxWrite {
			toWrite = toWrite[:maxWrite]
		}
		// Unlock while reading
		// 读出期间解锁
		r.mu.Unlock()
		nr, werr := w.Write(toWrite)
		r.mu.Lock()
		if werr != nil {
			n += int64(nr)
			err = r.setErr(werr, true)
			break
		}
		if nr != len(toWrite) {
			err = r.setErr(io.ErrShortWrite, true)
			break
		}
		r.r += nr
		if r.r == r.size {
			r.r = 0
		}
		r.isFull = false
		n += int64(nr)
		r.readCond.Broadcast()
	}
	if err == io.EOF {
		err = nil
	}
	return n, err
}

// Copy will pipe all data from the reader to the writer through the ringbuffer.
// The ringbuffer will switch to blocking mode.
// Reads and writes will be done async.
// No internal mem-copies are used for the transfer.
//
// Calling CloseWithError will cancel the transfer and make the function return when
// any ongoing reads or writes have finished.
//
// Calling Read or Write functions concurrently with running this will lead to unpredictable results.
// Copy 通过 ringbuffer 把 reader 的全部数据搬运到 writer，缓冲会切换为阻塞模式，
// 读写异步进行，搬运过程无额外内存拷贝。调用 CloseWithError 会取消搬运，正在进行的
// 读写结束后函数返回。与运行中的 Copy 并发调用 Read/Write 会导致不可预测的结果.
func (r *RingBuffer) Copy(dst io.Writer, src io.Reader) (written int64, err error) {
	r.SetBlocking(true)
	var wg sync.WaitGroup
	wg.Go(func() {
		_, _ = r.ReadFrom(src) //nolint errcheck
		r.CloseWriter()
	})
	defer wg.Wait()
	return r.WriteTo(dst)
}

// TryWrite writes len(p) bytes from p to the underlying buf like Write, but it is not blocking.
// If it does not succeed to acquire the lock, it returns ErrAcquireLock.
// TryWrite 像 Write 一样把 p 的 len(p) 字节写入底层缓冲，但不阻塞。未获取到锁时返回
// ErrAcquireLock.
func (r *RingBuffer) TryWrite(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, r.setErr(nil, false)
	}
	ok := r.mu.TryLock()
	if !ok {
		return 0, ErrAcquireLock
	}
	defer r.mu.Unlock()
	if err := r.checkWriteErr(); err != nil {
		return 0, err
	}

	n, err = r.write(p)
	if r.block && n > 0 {
		r.writeCond.Broadcast()
	}
	return n, r.setErr(err, true)
}

func (r *RingBuffer) write(p []byte) (n int, err error) {
	// In overwrite mode, we always allow writing by discarding old data
	// 覆盖模式下丢弃旧数据，始终允许写入
	if r.overwrite && len(p) > 0 {
		// A single write larger than the total capacity keeps only the first size bytes (see the TwiceFull test);
		// 一次写入超过总容量时，按既有约定只保留前 size 字节（见 TwiceFull 测试）；
		// truncate first, then advance the read pointer; otherwise the freed space is still
		// short and never-written slots can be read out
		// 先截断再推进读指针，否则推进后可用空间仍不足，会把从未写入的槽位读出来
		if len(p) > r.size {
			p = p[:r.size]
		}
		var avail int
		if r.w == r.r && r.isFull {
			// Buffer is full, no space available
			// 缓冲已满，无可用空间
			avail = 0
		} else if r.w >= r.r {
			avail = r.size - r.w + r.r
		} else {
			avail = r.r - r.w
		}

		// If we need more space than available, discard old data
		// 需要的空间超过可用空间时，丢弃旧数据
		if len(p) > avail {
			// Advance read pointer to make room
			// 推进读指针腾出空间
			needed := len(p) - avail
			r.r = (r.r + needed) % r.size
			// If buffer was full, it's no longer full after advancing read pointer
			// 原本已满的缓冲在推进读指针后不再满
			r.isFull = false
		}
	}

	if r.isFull {
		return 0, ErrIsFull
	}

	var avail int
	if r.w >= r.r {
		avail = r.size - r.w + r.r
	} else {
		avail = r.r - r.w
	}

	if len(p) > avail {
		err = ErrTooMuchDataToWrite
		p = p[:avail]
	}
	n = len(p)

	if r.w >= r.r {
		c1 := r.size - r.w
		if c1 >= n {
			copy(r.buf[r.w:], p)
			r.w += n
		} else {
			copy(r.buf[r.w:], p[:c1])
			c2 := n - c1
			copy(r.buf[0:], p[c1:])
			r.w = c2
		}
	} else {
		copy(r.buf[r.w:], p)
		r.w += n
	}

	if r.w == r.size {
		r.w = 0
	}
	if r.w == r.r {
		r.isFull = true
	}

	return n, err
}

// WriteByte writes one byte into buffer, and returns ErrIsFull if the buffer is full | WriteByte 写入一个字节，缓冲满时返回 ErrIsFull.
func (r *RingBuffer) WriteByte(c byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkWriteErr(); err != nil {
		return err
	}
	err := r.writeByte(c)
	for err == ErrIsFull && r.block {
		// Record the generation before waiting. If Reset() increments the generation,
		// we will detect it after waking up and continue waiting.
		// 等待前记录 generation。若 Reset() 递增了 generation，醒来后会发现并继续等待.
		myGen := r.generation
		if !r.waitRead() {
			return context.DeadlineExceeded
		}
		// If generation changed, Reset() happened while we were waiting.
		// Go back to waiting - the buffer was reset.
		// generation 变化说明等待期间发生了 Reset()。回到等待——缓冲已被重置.
		if myGen != r.generation {
			err = r.writeByte(c)
			continue
		}
		err = r.setErr(r.writeByte(c), true)
	}
	if r.block && err == nil {
		r.writeCond.Broadcast()
	}
	return err
}

// TryWriteByte writes one byte into buffer without blocking.
// If it does not succeed to acquire the lock, it returns ErrAcquireLock.
// TryWriteByte 向缓冲写入一个字节且不阻塞。未获取到锁时返回 ErrAcquireLock.
func (r *RingBuffer) TryWriteByte(c byte) error {
	ok := r.mu.TryLock()
	if !ok {
		return ErrAcquireLock
	}
	defer r.mu.Unlock()
	if err := r.checkWriteErr(); err != nil {
		return err
	}

	err := r.writeByte(c)
	if err == nil && r.block {
		r.writeCond.Broadcast()
	}
	return err
}

func (r *RingBuffer) writeByte(c byte) error {
	if r.err != nil {
		return r.err
	}
	if r.w == r.r && r.isFull {
		// In overwrite mode, discard the oldest byte and write the new one
		// 覆盖模式下丢弃最旧字节再写入新字节
		if r.overwrite {
			r.r++
			if r.r == r.size {
				r.r = 0
			}
		} else {
			return ErrIsFull
		}
	}
	r.buf[r.w] = c
	r.w++

	if r.w == r.size {
		r.w = 0
	}
	if r.w == r.r {
		r.isFull = true
	}

	return nil
}

// Length returns the number of bytes that can be read without blocking | Length 返回可不阻塞读取的字节数.
func (r *RingBuffer) Length() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.w == r.r {
		if r.isFull {
			return r.size
		}
		return 0
	}

	if r.w > r.r {
		return r.w - r.r
	}

	return r.size - r.r + r.w
}

// Capacity returns the size of the underlying buffer | Capacity 返回底层缓冲的大小.
func (r *RingBuffer) Capacity() int {
	return r.size
}

// Free returns the number of bytes that can be written without blocking | Free 返回可不阻塞写入的字节数.
func (r *RingBuffer) Free() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.w == r.r {
		if r.isFull {
			return 0
		}
		return r.size
	}

	if r.w < r.r {
		return r.r - r.w
	}

	return r.size - r.w + r.r
}

// WriteString writes the contents of the string s to buffer, which accepts a slice of bytes | WriteString 把字符串 s 的内容写入缓冲（内部转为字节切片）.
func (r *RingBuffer) WriteString(s string) (n int, err error) {
	x := (*[2]uintptr)(unsafe.Pointer(&s))
	h := [3]uintptr{x[0], x[1], x[1]}
	buf := *(*[]byte)(unsafe.Pointer(&h))
	return r.Write(buf)
}

// Bytes returns all available read bytes.
// It does not move the read pointer and only copy the available data.
// If the dst is big enough, it will be used as destination,
// otherwise a new buffer will be allocated.
// Bytes 返回所有可读字节。不移动读指针，仅复制可用数据；dst 足够大时复用，
// 否则分配新缓冲.
func (r *RingBuffer) Bytes(dst []byte) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	getDst := func(n int) []byte {
		if cap(dst) < n {
			return make([]byte, n)
		}
		return dst[:n]
	}

	if r.w == r.r {
		if r.isFull {
			buf := getDst(r.size)
			copy(buf, r.buf[r.r:])
			copy(buf[r.size-r.r:], r.buf[:r.w])
			return buf
		}
		return nil
	}

	if r.w > r.r {
		buf := getDst(r.w - r.r)
		copy(buf, r.buf[r.r:r.w])
		return buf
	}

	n := r.size - r.r + r.w
	buf := getDst(n)

	if r.r+n < r.size {
		copy(buf, r.buf[r.r:r.r+n])
	} else {
		c1 := r.size - r.r
		copy(buf, r.buf[r.r:r.size])
		c2 := n - c1
		copy(buf[c1:], r.buf[0:c2])
	}

	return buf
}

// IsFull returns true when the ringbuffer is full | IsFull 报告缓冲是否已满.
func (r *RingBuffer) IsFull() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.isFull
}

// IsEmpty returns true when the ringbuffer is empty | IsEmpty 报告缓冲是否为空.
func (r *RingBuffer) IsEmpty() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return !r.isFull && r.w == r.r
}

// CloseWithError closes the writer; reads will return
// no bytes and the error err, or EOF if err is nil.
//
// CloseWithError never overwrites the previous error if it exists
// and always returns nil.
// CloseWithError 关闭写端；之后读返回 0 字节和错误 err（err 为 nil 时为 EOF）。
// 已有错误不会被覆盖，且始终返回 nil.
func (r *RingBuffer) CloseWithError(err error) {
	if err == nil {
		err = io.EOF
	}
	r.setErr(err, false) //nolint errcheck
}

// CloseWriter closes the writer.
// Reads will return any remaining bytes and io.EOF.
// CloseWriter 关闭写端。之后读返回剩余字节和 io.EOF.
func (r *RingBuffer) CloseWriter() {
	r.setErr(io.EOF, false) //nolint errcheck
}

// Flush waits for the buffer to be empty and fully read.
// If not blocking ErrIsNotEmpty will be returned if the buffer still contains data.
// Flush 等待缓冲被读空。非阻塞模式下缓冲仍有数据时返回 ErrIsNotEmpty.
func (r *RingBuffer) Flush() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for r.w != r.r || r.isFull {
		err := r.readErr(true)
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return err
		}
		if !r.block {
			return ErrIsNotEmpty
		}
		if !r.waitRead() {
			return context.DeadlineExceeded
		}
	}

	err := r.readErr(true)
	if err == io.EOF {
		return nil
	}
	return err
}

// Reset the read pointer and writer pointer to zero | Reset 把读写指针都归零.
func (r *RingBuffer) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.block {
		// In blocking mode, increment generation to invalidate current waiters.
		// They will wake up, see the generation changed, and go back to waiting.
		// This makes Reset() transparent to waiters - they continue waiting as if nothing happened.
		//
		// Note: We don't wait for wg.Wait() in blocking mode since waiters may
		// block indefinitely. The generation mechanism ensures they handle the reset correctly.
		// 阻塞模式下递增 generation 使当前等待者失效。它们醒来后发现 generation 变化，
		// 继续等待，因此 Reset() 对等待者是透明的——如同什么都没发生。
		//
		// 注意：阻塞模式下不等待 wg.Wait()，因为等待者可能无限阻塞。generation 机制
		// 保证它们能正确处理重置.
		r.generation++
		r.readCond.Broadcast()
		r.writeCond.Broadcast()
		r.r = 0
		r.w = 0
		r.err = nil
		r.isFull = false
		return
	}

	// In non-blocking mode, set error to return immediately to any readers/writers.
	// 非阻塞模式下设置错误，让读者/写者立即返回.
	r.setErr(ErrReset, true) //nolint errcheck

	// Unlock the mutex so readers/writers can finish.
	// 先解锁让读者/写者执行完毕.
	r.mu.Unlock()
	r.wg.Wait()
	r.mu.Lock()
	r.r = 0
	r.w = 0
	r.err = nil
	r.isFull = false
}

// WriteCloser returns a WriteCloser that writes to the ring buffer.
// When the returned WriteCloser is closed, it will wait for all data to be read before returning.
// WriteCloser 返回写入 ring buffer 的 WriteCloser。关闭时会等所有数据被读走后才返回.
func (r *RingBuffer) WriteCloser() io.WriteCloser {
	return &writeCloser{RingBuffer: r}
}

type writeCloser struct {
	*RingBuffer
}

// Close provides a close method for the WriteCloser | Close 为 WriteCloser 提供关闭方法.
func (wc *writeCloser) Close() error {
	wc.CloseWriter()
	return wc.Flush()
}

// ReadCloser returns a io.ReadCloser that reads to the ring buffer.
// When the returned ReadCloser is closed, ErrReaderClosed will be returned on any writes done afterwards.
// ReadCloser 返回从 ring buffer 读取的 io.ReadCloser。关闭后再写入会返回 ErrReaderClosed.
func (r *RingBuffer) ReadCloser() io.ReadCloser {
	return &readCloser{RingBuffer: r}
}

type readCloser struct {
	*RingBuffer
}

// Close provides a close method for the ReadCloser | Close 为 ReadCloser 提供关闭方法.
func (rc *readCloser) Close() error {
	rc.CloseWithError(ErrReaderClosed)
	err := rc.readErr(false)
	if err == ErrReaderClosed {
		err = nil
	}
	return err
}

// Peek reads up to len(p) bytes into p without moving the read pointer | Peek 把最多 len(p) 字节读入 p，不移动读指针.
func (r *RingBuffer) Peek(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, r.readErr(false)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.readErr(true); err != nil {
		return 0, err
	}

	return r.peek(p)
}

func (r *RingBuffer) peek(p []byte) (n int, err error) {
	n = r.copyFromBuffer(p)
	if n == 0 {
		return 0, ErrIsEmpty
	}
	return n, r.readErr(true)
}
