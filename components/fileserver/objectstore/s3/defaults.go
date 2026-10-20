package s3

import "time"

// 默认值与范围集中定义。
const (
	// DefaultPartSize 是默认分片大小（8 MiB）。
	DefaultPartSize int64 = 8 << 20
	// MinPartSize 是 S3 对非末尾分片的下限（5 MiB）。
	MinPartSize int64 = 5 << 20
	// MaxPartSize 是 S3 单个分片的上限（5 GiB）。
	MaxPartSize int64 = 5 << 30

	// DefaultConcurrency 是单次 Upload 或 Download 的默认并发度。
	DefaultConcurrency = 1
	// MaxConcurrency 是允许的最大并发度。
	MaxConcurrency = 32

	// maxObjectSize 是 S3 单个对象的上限（5 TiB）。
	maxObjectSize int64 = 5 << 40
	// maxMetadataBytes 是 S3 用户元数据键与值总大小的上限（2 KiB）。
	maxMetadataBytes = 2 << 10
	// maxParts 是单次分片上传允许的最大分片数（S3 上限）。
	maxParts = 10000
	// abortTimeout 限制清理未完成上传的时间，与调用方的 Context 无关。
	abortTimeout = 10 * time.Second
)
