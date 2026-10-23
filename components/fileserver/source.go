package fileserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"os"
)

// UploadTaskFromPath 延迟打开本地文件，每次 Open 都创建独立句柄，由 UploadAll 关闭。
// 不预读或保存文件大小；如需准确长度检查，可传 WithSize。重试时调用方须保证来源未改变。
func UploadTaskFromPath(key, filename string, opts ...UploadOpt) UploadTask {
	return UploadTask{
		Key: key, Opts: append([]UploadOpt(nil), opts...),
		Open: func(ctx context.Context) (io.ReadCloser, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			f, err := os.Open(filename)
			if err != nil {
				return nil, sourceError(err)
			}
			info, err := f.Stat()
			if err == nil && !info.Mode().IsRegular() {
				err = invalidArgument("upload source is not a regular file")
			}
			if err != nil {
				return nil, errors.Join(sourceError(err), sourceError(f.Close()))
			}
			return f, nil
		},
	}
}

// UploadTaskFromBytes 保存 data 的副本，每次 Open 返回从头开始的独立流。
// 自动声明长度（包括空对象）；显式传入的 opts 在默认长度之后应用。
func UploadTaskFromBytes(key string, data []byte, opts ...UploadOpt) UploadTask {
	data = bytes.Clone(data)
	return UploadTask{
		Key: key, Opts: append([]UploadOpt{WithSize(int64(len(data)))}, opts...),
		Open: func(ctx context.Context) (io.ReadCloser, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return io.NopCloser(bytes.NewReader(data)), nil
		},
	}
}

// UploadTaskFromMultipart 延迟打开 multipart 文件，并以 Header.Size 声明长度。
// 必须在 multipart.Form.RemoveAll 之前完成上传；本函数不会延长临时文件的生命周期。
// nil Header 的任务在 Open 时返回 ErrInvalidArgument。
func UploadTaskFromMultipart(key string, header *multipart.FileHeader, opts ...UploadOpt) UploadTask {
	var saved *multipart.FileHeader
	defaults := []UploadOpt(nil)
	if header != nil {
		copy := *header
		saved = &copy
		defaults = append(defaults, WithSize(copy.Size))
	}
	return UploadTask{
		Key: key, Opts: append(defaults, opts...),
		Open: func(ctx context.Context) (io.ReadCloser, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if saved == nil {
				return nil, invalidArgument("multipart header is nil")
			}
			r, err := saved.Open()
			return r, sourceError(err)
		},
	}
}

// sourceError 保留操作系统错误类别，不在错误文本中暴露本地绝对路径。
func sourceError(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
