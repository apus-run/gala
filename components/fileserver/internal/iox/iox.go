// Package iox provides read/write helpers shared by drivers: a Context check
// before every read, absorption of empty reads, and range resolution.
// Package iox 提供驱动共用的读写辅助：每次读取前检查 Context、吸收空读，以及范围解析。
package iox

import (
	"context"
	"fmt"
	"io"

	"github.com/apus-run/gala/components/fileserver"
)

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

// Read checks the context, then delegates | Read 先检查 context 再委托底层读取.
func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// Reader returns a reader that checks ctx before every Read. It cannot
// interrupt a Read already blocked in the underlying reader.
// Reader 返回每次 Read 前都检查 ctx 的读取器。它不能打断已阻塞的底层 Read。
func Reader(ctx context.Context, r io.Reader) io.Reader {
	return ctxReader{ctx: ctx, r: r}
}

// CopyN copies at most n bytes from src to dst, checking ctx before every
// read, and returns the number of bytes actually written. If src ends early,
// the returned count is less than n with a nil err; the caller decides whether
// that is an error. buf must be non-empty and is used as the copy buffer.
// CopyN 最多从 src 复制 n 字节到 dst，每次读取前检查 ctx，返回实际写入的字节数。
// src 提前结束时返回的字节数小于 n 且 err 为 nil，由调用方判断是否为错误。
// buf 必须非空，用作拷贝缓冲。
func CopyN(ctx context.Context, dst io.Writer, src io.Reader, n int64, buf []byte) (int64, error) {
	return io.CopyBuffer(dst, io.LimitReader(Reader(ctx, src), n), buf)
}

// maxEmptyReads is the limit of consecutive (0, nil) returns. Beyond it the
// reader is considered stuck.
// maxEmptyReads 是连续返回 (0, nil) 的上限。超过后读取器被视为卡住。
const maxEmptyReads = 100

// ReadSome reads until at least one byte arrives or an error occurs.
// Occasional (0, nil) from the reader is absorbed; after maxEmptyReads
// consecutive (0, nil) it returns io.ErrNoProgress.
// ReadSome 读取直到得到至少一个字节或遇到错误。读取器偶发的 (0, nil) 会被吸收；
// 连续 maxEmptyReads 次为 (0, nil) 时返回 io.ErrNoProgress。
func ReadSome(r io.Reader, p []byte) (int, error) {
	for range maxEmptyReads {
		if n, err := r.Read(p); n > 0 || err != nil {
			return n, err
		}
	}
	return 0, io.ErrNoProgress
}

// ResolveRange applies the requested range (off, length) to an object of the
// given size and returns the actual start and byte count. length -1 means "to
// the end"; a range past the end is clipped to the end. off > size returns
// ErrInvalidArgument; off == size yields an empty result.
// ResolveRange 把请求的范围 (off, length) 应用到大小为 size 的对象上，返回实际起点与字节数。
// length 为 -1 表示到末尾；超出末尾的部分截到末尾。off > size 返回 ErrInvalidArgument；
// off == size 得到空结果。
func ResolveRange(size, off, length int64) (start, n int64, err error) {
	if off > size {
		return 0, 0, fmt.Errorf("%w: range offset %d is beyond size %d", fileserver.ErrInvalidArgument, off, size)
	}
	n = size - off
	if length >= 0 && length < n {
		n = length
	}
	return off, n, nil
}
