// Package opctx bounds one lock operation without hiding the caller's
// deadline | opctx 为单次锁操作限时，同时保留调用方自己的截止时间.
package opctx

import (
	"context"
	"time"
)

// WithTimeout applies timeout to ctx, but never extends a deadline the caller
// already set: the earlier of the two wins.
// WithTimeout 用 timeout 限制 ctx，但不会延长调用方已设置的截止时间：两者取更早者。
func WithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= timeout {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}
