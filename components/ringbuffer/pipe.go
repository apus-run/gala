// Copyright 2019 smallnest. All rights reserved. //nolint:lll
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package ringbuffer

import "io"

// Pipe creates an asynchronous in-memory pipe compatible with io.Pipe
// It can be used to connect code expecting an [io.Reader]
// with code expecting an [io.Writer].
//
// Reads and Writes will go to the ring buffer.
// Writes will complete as long as the data fits within the ring buffer.
// Reads will attempt to satisfy reads with data from the ring buffer.
// Only if the ring buffer is empty will the read block.
//
// It is safe (and intended) to call Read and Write in parallel with each other or with Close.
// Pipe 创建与 io.Pipe 兼容的异步内存管道，可连接期望 [io.Reader] 的代码与期望
// [io.Writer] 的代码。读写都落在 ring buffer 上：只要数据能装进缓冲写入即完成；
// 读优先用缓冲中的数据，仅当缓冲为空时才阻塞。可以（且推荐）并行调用 Read、Write
// 与 Close.
func (r *RingBuffer) Pipe() (*PipeReader, *PipeWriter) {
	r.SetBlocking(true)
	pr := PipeReader{pipe: r}
	return &pr, &PipeWriter{pipe: r}
}

// A PipeReader is the read half of a pipe | PipeReader 是管道的读端.
type PipeReader struct{ pipe *RingBuffer }

// Read implements the standard Read interface:
// it reads data from the pipe, blocking until a writer
// arrives or the write end is closed.
// If the write end is closed with an error, that error is
// returned as err; otherwise err is io.EOF.
// Read 实现标准 Read 接口：从管道读数据，阻塞直到有写入者或写端关闭。
// 写端带错误关闭时返回该错误，否则返回 io.EOF.
func (r *PipeReader) Read(data []byte) (n int, err error) {
	return r.pipe.Read(data)
}

// Close closes the reader; subsequent writes to the
// write half of the pipe will return the error [io.ErrClosedPipe].
// Close 关闭读端；之后向管道写端写入会返回 [io.ErrClosedPipe] 错误.
func (r *PipeReader) Close() error {
	r.pipe.setErr(io.ErrClosedPipe, false) //nolint:errcheck
	return nil
}

// CloseWithError closes the reader; subsequent writes
// to the write half of the pipe will return the error err.
//
// CloseWithError never overwrites the previous error if it exists
// and always returns nil.
// CloseWithError 关闭读端；之后向写端写入会返回错误 err.
// 已有错误不会被覆盖，且始终返回 nil.
func (r *PipeReader) CloseWithError(err error) error {
	if err == nil {
		return r.Close()
	}
	r.pipe.setErr(err, false) //nolint:errcheck
	return nil
}

// A PipeWriter is the write half of a pipe | PipeWriter 是管道的写端.
type PipeWriter struct{ pipe *RingBuffer }

// Write implements the standard Write interface:
// it writes data to the pipe.
// The Write will block until all data has been written to the ring buffer.
// If the read end is closed with an error, that err is
// returned as err; otherwise err is [io.ErrClosedPipe].
// Write 实现标准 Write 接口：向管道写数据，阻塞直到全部数据写入 ring buffer。
// 读端带错误关闭时返回该错误，否则返回 [io.ErrClosedPipe].
func (w *PipeWriter) Write(data []byte) (n int, err error) {
	if n, err = w.pipe.Write(data); err == ErrWriteOnClosed {
		// Replace error.
		// 替换错误.
		err = io.ErrClosedPipe
	}
	return n, err
}

// Close closes the writer; subsequent reads from the
// read half of the pipe will return no bytes and EOF.
// Close 关闭写端；之后从读端读取返回 0 字节和 EOF.
func (w *PipeWriter) Close() error {
	w.pipe.setErr(io.EOF, false) //nolint:errcheck
	return nil
}

// CloseWithError closes the writer; subsequent reads from the
// read half of the pipe will return no bytes and the error err,
// or EOF if err is nil.
//
// CloseWithError never overwrites the previous error if it exists
// and always returns nil.
// CloseWithError 关闭写端；之后从读端读取返回 0 字节和错误 err（err 为 nil 时为 EOF）.
// 已有错误不会被覆盖，且始终返回 nil.
func (w *PipeWriter) CloseWithError(err error) error {
	if err == nil {
		return w.Close()
	}
	w.pipe.setErr(err, false) //nolint:errcheck
	return nil
}
