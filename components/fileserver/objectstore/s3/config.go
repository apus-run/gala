// Package s3 implements fileserver.Storage and objectstore.ObjectStorage on S3-compatible services via aws-sdk-go-v2 | s3 基于 aws-sdk-go-v2 在 S3 兼容服务上实现 fileserver.Storage 与 objectstore.ObjectStorage.
//
// # Notes | 说明
//
// Presigned PUT/GET requests are computed locally and work with
// S3-compatible endpoints (MinIO, RustFS, ...). Upload reads one part
// first: a small input becomes a single PutObject, larger input becomes a
// multipart upload completed only after every part succeeds; a failed run
// aborts it. Commit-type requests are single-attempt so SDK retries cannot
// mask an unknown result. Memory is bounded by PartSize × Concurrency.
// 预签名 PUT/GET 在本地计算，兼容 MinIO、RustFS 等 S3 兼容端点。上传先读满
// 一片：小输入走单次 PutObject，大输入走分片上传，全部成功才 Complete，失败的
// 上传会被 Abort。提交类请求强制单次尝试，SDK 重试不会掩盖未知结果。内存上界
// 为 PartSize × Concurrency。
//
// Ranged reads HEAD first and then GET with If-Match, so a concurrently
// replaced object returns ErrConflict; services without ETags get
// ErrUnsupported for ranged reads (whole-object reads still work).
// Credentials, region, endpoint and timeouts come from the SDK client the
// caller builds.
// 范围读取先 HEAD 再带 If-Match 发起 GET，对象被并发替换时返回 ErrConflict；
// 无 ETag 的服务范围读取返回 ErrUnsupported（整对象读取不受影响）。凭据、
// Region、端点与超时由调用方构造的 SDK Client 决定。
//
// # Features | 功能
//
// - Multipart upload with SHA-256 checksums; known sizes stretch the part size up to the 5 TiB object limit | 分片上传附带 SHA-256 校验和，已知长度时自动放大分片以覆盖 5 TiB 对象上限
// - Concurrent download with fixed per-worker buffers | 并发下载，每个 worker 使用固定缓冲
// - Configurable Prefix and MaxObjectSize | 可配置 Prefix 前缀与 MaxObjectSize
//
// # Example | 示例
//
//	client := awss3.NewFromConfig(awsCfg)
//	store, err := s3.New(client, s3.Config{Bucket: "files", Prefix: "prod"})
//	err = store.Upload(ctx, "a/b.bin", r)
//	rc, err := store.Read(ctx, "a/b.bin")
package s3

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/objectstore"
)

// API is the narrow interface driving the actual SDK calls; satisfied by *awss3.Client.
// Tests can substitute a fake implementation instead of mocking the whole SDK.
// API 是驱动实际调用的 SDK 方法的窄接口。*awss3.Client 满足它。
// 测试可以用假实现替换，无需模拟整个 SDK。
type API interface {
	PutObject(ctx context.Context, params *awss3.PutObjectInput, optFns ...func(*awss3.Options)) (*awss3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *awss3.GetObjectInput, optFns ...func(*awss3.Options)) (*awss3.GetObjectOutput, error)
	HeadObject(ctx context.Context, params *awss3.HeadObjectInput, optFns ...func(*awss3.Options)) (*awss3.HeadObjectOutput, error)
	DeleteObject(ctx context.Context, params *awss3.DeleteObjectInput, optFns ...func(*awss3.Options)) (*awss3.DeleteObjectOutput, error)
	CreateMultipartUpload(ctx context.Context, params *awss3.CreateMultipartUploadInput, optFns ...func(*awss3.Options)) (*awss3.CreateMultipartUploadOutput, error)
	UploadPart(ctx context.Context, params *awss3.UploadPartInput, optFns ...func(*awss3.Options)) (*awss3.UploadPartOutput, error)
	CompleteMultipartUpload(ctx context.Context, params *awss3.CompleteMultipartUploadInput, optFns ...func(*awss3.Options)) (*awss3.CompleteMultipartUploadOutput, error)
	AbortMultipartUpload(ctx context.Context, params *awss3.AbortMultipartUploadInput, optFns ...func(*awss3.Options)) (*awss3.AbortMultipartUploadOutput, error)
}

// Presigner generates presigned requests, satisfied by *awss3.PresignClient | Presigner 生成预签名请求，由 *awss3.PresignClient 满足。
type Presigner interface {
	PresignPutObject(ctx context.Context, params *awss3.PutObjectInput, optFns ...func(*awss3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
	PresignGetObject(ctx context.Context, params *awss3.GetObjectInput, optFns ...func(*awss3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

var (
	_ API       = (*awss3.Client)(nil)
	_ Presigner = (*awss3.PresignClient)(nil)
)

// Config configures the S3 driver. Credentials, region, endpoint, timeouts
// and default retries come from the SDK client the caller builds.
// PutObject, CreateMultipartUpload and CompleteMultipartUpload are forced
// single-attempt so retries cannot mask an unknown result.
// Config 是 S3 驱动的配置。凭据、Region、Endpoint、超时与默认重试由调用方构造的 SDK Client 决定。
// PutObject、CreateMultipartUpload 和 CompleteMultipartUpload 强制单次尝试，避免重试掩盖未知结果。
type Config struct {
	Bucket string
	// Name of the target S3 bucket | 目标 S3 桶名。
	// Logical-key prefix joined internally by the driver; leading and
	// trailing '/' are stripped.
	// Prefix 是逻辑 Key 的前缀，驱动在内部拼接。它会去掉首尾的 '/'。
	Prefix string
	// Part size for multipart uploads and the buffer bound the driver
	// allocates for a single part. Defaults to DefaultPartSize, range
	// [MinPartSize, MaxPartSize]. Uploads of unknown length are bound by
	// S3's 10000-part limit (about 78 GiB with defaults); with a known
	// length the driver stretches the part size to cover up to 5 TiB.
	// PartSize 是分片大小，也是驱动为单个分片分配的缓冲上界。默认 DefaultPartSize，范围 [MinPartSize, MaxPartSize]。
	// 长度未知的上传受 S3 的 10000 片上限约束（默认约 78 GiB）；已知长度时驱动会放大分片以覆盖到 5 TiB。
	PartSize int64
	// Parallelism inside a single Upload or Download. Memory is bounded
	// by PartSize × Concurrency. Defaults to 1, range [1, 32].
	// Concurrency 是单次 Upload 或 Download 内部的并发度。内存上界为 PartSize × Concurrency。默认 1，范围 [1, 32]。
	Concurrency int
	// Optional cap on object size; 0 means unlimited | MaxObjectSize 是可选的对象大小上限，0 表示不限。
	MaxObjectSize int64
	// Signing capability, usually awss3.NewPresignClient(client). When
	// nil the signing methods return ErrUnsupported.
	// Presigner 提供签名能力，通常是 awss3.NewPresignClient(client)。为 nil 时，签名方法返回 ErrUnsupported。
	Presigner Presigner
}

// Store is an object store on S3 implementing Storage and ObjectStorage.
// Concurrent calls of its methods on different keys are safe.
// Store 是 S3 上的对象存储，实现 Storage 与 ObjectStorage。
// 方法对不同 Key 的并发调用是安全的。
type Store struct {
	api         API
	presign     Presigner
	bucket      string
	prefix      string
	partSize    int64
	concurrency int
	maxObject   int64
}

var (
	_ fileserver.Storage        = (*Store)(nil)
	_ objectstore.ObjectStorage = (*Store)(nil)
)

// New creates an S3 driver. api is usually *awss3.Client; invalid values
// in cfg return ErrInvalidArgument.
// New 创建 S3 驱动。api 通常是 *awss3.Client；cfg 中的非法值返回 ErrInvalidArgument。
func New(api API, cfg Config) (*Store, error) {
	if api == nil {
		return nil, invalidArgument("API is nil")
	}
	if cfg.Bucket == "" || strings.ContainsAny(cfg.Bucket, "/ \t\r\n") {
		return nil, invalidArgument("bucket name is empty or malformed")
	}
	prefix := strings.Trim(cfg.Prefix, "/")
	if prefix != "" {
		if err := fileserver.ValidateKey(prefix); err != nil {
			return nil, fmt.Errorf("%w: prefix: %w", fileserver.ErrInvalidArgument, err)
		}
	}

	partSize := cfg.PartSize
	if partSize == 0 {
		partSize = DefaultPartSize
	}
	if partSize < MinPartSize || partSize > MaxPartSize {
		return nil, invalidArgument("part size must be between 5 MiB and 5 GiB")
	}
	if int64(int(partSize)) != partSize {
		return nil, invalidArgument("part size exceeds the platform int range")
	}

	concurrency := cfg.Concurrency
	if concurrency == 0 {
		concurrency = DefaultConcurrency
	}
	if concurrency < 1 || concurrency > MaxConcurrency {
		return nil, invalidArgument("concurrency must be between 1 and 32")
	}
	if cfg.MaxObjectSize < 0 {
		return nil, invalidArgument("max object size must not be negative")
	}

	return &Store{
		api:         api,
		presign:     cfg.Presigner,
		bucket:      cfg.Bucket,
		prefix:      prefix,
		partSize:    partSize,
		concurrency: concurrency,
		maxObject:   cfg.MaxObjectSize,
	}, nil
}

// objectKey validates the logical key and joins the prefix into the S3
// object key. The combined total length must likewise not exceed 1024
// bytes.
// objectKey 校验逻辑 Key，并拼接前缀得到 S3 对象键。拼接后的总长同样不得超过 1024 字节。
func (s *Store) objectKey(key string) (string, error) {
	if err := fileserver.ValidateKey(key); err != nil {
		return "", err
	}
	if s.prefix == "" {
		return key, nil
	}
	full := s.prefix + "/" + key
	if len(full) > fileserver.MaxKeyLength {
		return "", fmt.Errorf("%w: prefixed key exceeds %d bytes", fileserver.ErrInvalidKey, fileserver.MaxKeyLength)
	}
	return full, nil
}

func invalidArgument(msg string) error {
	return fmt.Errorf("%w: %s", fileserver.ErrInvalidArgument, msg)
}
