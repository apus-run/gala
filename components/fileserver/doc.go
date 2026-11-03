// Package fileserver defines the Go contract for object storage: upload, download, read, stat and remove | fileserver 定义对象存储的 Go 契约：上传、下载、读取、查询与删除.
//
// # Notes | 说明
//
// Interfaces, types and semantics only — no backend or third-party SDK is
// imported here. Local disk implementation: filestore; S3-compatible cloud:
// objectstore/s3; conformance suite and Fake: fileservertest.
// 本包只有接口、类型与语义，不导入任何具体后端或第三方 SDK。本地磁盘实现见
// filestore，S3 兼容云端实现见 objectstore/s3，测试套件与 Fake 见 fileservertest。
//
// A Key is a logical id inside a root/bucket — not a filesystem path, not
// the user's original filename. ValidateKey runs at every public entry:
// UTF-8, "/"-separated non-empty segments, no "." / "..", no control or
// Windows-forbidden characters, total length ≤ 1024 bytes. Errors are
// sentinel values tested with errors.Is (ErrNotFound, ErrConflict, ...);
// ErrIndeterminate means the outcome is unknown and must be verified with
// Stat before any retry. ETag is an opaque equality token, never a content
// MD5.
// Key 是根目录或 Bucket 内的逻辑标识——不是文件系统路径，也不是用户原始文件名。
// ValidateKey 在每个公共入口执行：UTF-8、"/" 分隔的非空段、不含 "." / ".."、
// 不含控制字符与 Windows 禁用字符、总长 ≤ 1024 字节。错误是可用 errors.Is 判断
// 的哨兵值（ErrNotFound、ErrConflict 等）；ErrIndeterminate 表示结果未知，
// 重试前必须先 Stat 核实。ETag 只是不透明的相等性令牌，绝不是内容 MD5。
//
// # Features | 功能
//
// - Uploader / Downloader split; Storage combines them, callers depend only on what they use | Uploader / Downloader 分离，Storage 聚合两者，调用方按需依赖
// - Batch via UploadAll / DownloadAll; presigned URLs via ObjectStorage (objectstore) | 批量走 UploadAll / DownloadAll，预签名 URL 走 ObjectStorage（objectstore）
// - Per-operation options: WithSize, WithRange, WithIfNotExists, WithProgress, metadata | 每操作独立选项：WithSize、WithRange、WithIfNotExists、WithProgress、元数据
//
// # Example | 示例
//
//	var s fileserver.Storage
//	err := s.Upload(ctx, "docs/a.txt", r, fileserver.WithSize(n))
//	rc, err := s.Read(ctx, "docs/a.txt")
//	defer rc.Close()
package fileserver
