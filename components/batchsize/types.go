package batchsize

import (
	"context"
	"time"
)

// Adjuster 根据响应时间动态调整批处理大小。
// Adjuster computes the next batch size from the previous operation's latency.
type Adjuster interface {
	// Adjust 根据上次操作的响应时间计算下一批次的大小。
	Adjust(ctx context.Context, responseTime time.Duration) (int, error)
}
