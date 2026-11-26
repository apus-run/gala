// Package cleanup builds contexts for best-effort lock release operations |
// cleanup 为尽力而为的锁释放操作构建上下文.
package cleanup

import (
	"context"
	"time"
)

// Timeout bounds one cleanup request.
// Timeout 是一次清理请求的上限。
const Timeout = 5 * time.Second

// Context keeps the caller's values but ignores cancellation, so a canceled
// request or job context does not leave a lock held until its TTL. The result
// is bounded by Timeout.
// Context 保留调用方的值但忽略取消，避免请求/任务取消后锁一直留到 TTL；
// 返回的上下文最多存活 Timeout。
func Context(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), Timeout)
}
