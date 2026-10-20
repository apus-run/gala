package fileserver

import (
	"context"
	"io"
	"time"
)

// Uploader 上传一个对象。返回 nil 表示对象已完整可见（本地按耐久策略）。
// 不关闭 r。
type Uploader interface {
	Upload(ctx context.Context, key string, r io.Reader, opts ...UploadOpt) error
}

// Downloader 把对象字节写入 w：对象的第 i 个字节写到 w 的偏移 i 处，范围下载亦然。
// 不关闭 w。
type Downloader interface {
	Download(ctx context.Context, key string, w io.WriterAt, opts ...DownloadOpt) error
}

// Storage 是普通对象操作的完整能力集合。
type Storage interface {
	Uploader
	Downloader
	Stat(ctx context.Context, key string) (*ObjectInfo, error)
	// Read 返回对象内容的流。调用方必须关闭返回值。
	Read(ctx context.Context, key string, opts ...ReadOpt) (io.ReadCloser, error)
	// Remove 删除对象；对象不存在时返回 nil。
	Remove(ctx context.Context, key string) error
}

// ObjectInfo 是对象的元信息。后端未提供的字段为对应的零值。后续操作使用 FKey。
type ObjectInfo struct {
	FKey         string            // 逻辑 Key（不含后端前缀）
	FSize        int64             // 字节数
	FModTime     time.Time         // 后端不提供时为零值
	FContentType string            // 后端未持久化时为空
	FMetadata    map[string]string // 后端未持久化时为 nil；键统一小写
}
