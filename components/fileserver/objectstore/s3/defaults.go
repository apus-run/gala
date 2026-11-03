package s3

import "time"

// Defaults and bounds are defined in one place | 默认值与范围集中定义。
const (
	// DefaultPartSize is the default part size (8 MiB) | DefaultPartSize 是默认分片大小（8 MiB）。
	DefaultPartSize int64 = 8 << 20
	// MinPartSize is S3's lower bound for non-terminal parts (5 MiB) | MinPartSize 是 S3 对非末尾分片的下限（5 MiB）。
	MinPartSize int64 = 5 << 20
	// MaxPartSize is S3's upper bound for a single part (5 GiB) | MaxPartSize 是 S3 单个分片的上限（5 GiB）。
	MaxPartSize int64 = 5 << 30

	// DefaultConcurrency is the default concurrency of a single Upload or Download | DefaultConcurrency 是单次 Upload 或 Download 的默认并发度。
	DefaultConcurrency = 1
	// MaxConcurrency is the maximum allowed concurrency | MaxConcurrency 是允许的最大并发度。
	MaxConcurrency = 32

	// maxObjectSize is S3's per-object upper bound (5 TiB) | maxObjectSize 是 S3 单个对象的上限（5 TiB）。
	maxObjectSize int64 = 5 << 40
	// maxMetadataBytes is S3's cap on the total bytes of user metadata keys and values (2 KiB) | maxMetadataBytes 是 S3 用户元数据键与值总大小的上限（2 KiB）。
	maxMetadataBytes = 2 << 10
	// maxParts is the maximum part count of one multipart upload (S3 limit) | maxParts 是单次分片上传允许的最大分片数（S3 上限）。
	maxParts = 10000
	// abortTimeout bounds the time spent cleaning up an incomplete upload, independent of the caller's Context | abortTimeout 限制清理未完成上传的时间，与调用方的 Context 无关。
	abortTimeout = 10 * time.Second
)
