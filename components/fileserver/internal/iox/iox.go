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

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// Reader 返回每次 Read 前都检查 ctx 的读取器。它不能打断已阻塞的底层 Read。
func Reader(ctx context.Context, r io.Reader) io.Reader {
	return ctxReader{ctx: ctx, r: r}
}

// CopyN 最多从 src 复制 n 字节到 dst，每次读取前检查 ctx，返回实际写入的字节数。
// src 提前结束时返回的字节数小于 n 且 err 为 nil，由调用方判断是否为错误。
// buf 必须非空，用作拷贝缓冲。
func CopyN(ctx context.Context, dst io.Writer, src io.Reader, n int64, buf []byte) (int64, error) {
	return io.CopyBuffer(dst, io.LimitReader(Reader(ctx, src), n), buf)
}

// maxEmptyReads 是连续返回 (0, nil) 的上限。超过后读取器被视为卡住。
const maxEmptyReads = 100

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
