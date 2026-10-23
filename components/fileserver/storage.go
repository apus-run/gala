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

// Opener 是读取内容及其同版本属性的可选能力，不扩展 Storage 的必需方法。
// 实现不得用未加版本保护的 Stat + Read 拼接结果。
type Opener interface {
	Open(ctx context.Context, key string, opts ...ReadOpt) (*OpenResult, error)
}

// OpenResult 的 Body 与 Info 来自同一对象版本。调用方必须关闭 Body；其生命周期与 Read 相同。
// Info.Size 是完整对象的大小；Offset 和 Length 是本次流的起点与实际长度，范围超出末尾时截短。
// 大小必须已知；无法提供时 Open 返回 ErrUnsupported，调用方仍可使用 Read。
// 修改 Info（包括 Metadata）不会影响存储或其他读取结果。
type OpenResult struct {
	Body   io.ReadCloser
	Info   ObjectInfo
	Offset int64
	Length int64
}

// ObjectInfo 是对象的元信息。后端未提供的字段为对应的零值。后续操作使用 Key。
type ObjectInfo struct {
	Key         string            // 逻辑 Key（不含后端前缀）
	Size        int64             // 字节数
	ModTime     time.Time         // 后端不提供时为零值
	ContentType string            // 后端未持久化时为空
	Metadata    map[string]string // 后端未持久化时为 nil；键统一小写
	ETag        string            // 不透明的相等性令牌：同一对象内容不变则不变。只用于相等性比较，不得当作内容 MD5
}
