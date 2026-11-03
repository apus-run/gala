package authz

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrPolicyStale means policy freshness has expired. Callers must deny access.
// ErrPolicyStale 表示策略新鲜度已过期，调用方必须拒绝访问。
var ErrPolicyStale = errors.New("authz: policy is stale")

func (a *Authorizer) policyFresh() error {
	if a.maxPolicyAge > 0 {
		loaded := a.lastLoad.Load()
		if loaded == nil || time.Since(*loaded) >= a.maxPolicyAge {
			return ErrPolicyStale
		}
	}
	return nil
}

// SyncPolicy reloads locally and independently notifies peers after a committed
// write. Both errors are preserved. Success is NOT an acknowledgement from peers.
// Update runs outside the reload lock, so synchronous self callbacks are safe.
// SyncPolicy 在写入提交后本地重载并独立通知对端，两个错误都会保留。
// 成功并不代表对端已确认。Update 在重载锁之外执行，同步自回调也安全。
func (a *Authorizer) SyncPolicy() error {
	loadErr := a.LoadPolicy()
	var notifyErr error
	if a.watcher != nil {
		if err := a.watcher.Update(); err != nil {
			notifyErr = fmt.Errorf("authz: notify policy: %w", err)
		}
	}
	return errors.Join(loadErr, notifyErr)
}

// RunPolicyReload reloads immediately and periodically until ctx is cancelled.
// The ticker starts before the first load; this is not a sleep-after-load loop.
// An overdue tick can start the next load immediately. The interval must be less
// than maxPolicyAge when enabled; individual load duration is not checked here.
// It blocks: the application owns the goroutine and must wait for it on shutdown.
// Missed watcher events are repaired without broadcasting. Callback errors are
// reported and retried on the next tick. Cancellation cannot interrupt a running
// Adapter.LoadPolicy; configure storage timeouts. onError must return promptly.
// RunPolicyReload 立即重载并周期性重载，直到 ctx 被取消。
// ticker 在首次加载前启动，这不是“加载后睡眠”的循环；
// 迟到的 tick 可立即触发下一次加载。启用 maxPolicyAge 时间隔必须小于它，
// 此处不检查单次加载耗时。该函数会阻塞：goroutine 由应用持有，
// 关闭时必须等它退出。错过的 watcher 事件靠本循环补齐，不再广播；
// 回调错误会上报并在下一个 tick 重试。取消不能中断正在执行的
// Adapter.LoadPolicy，请为存储配置超时。onError 必须迅速返回。
func (a *Authorizer) RunPolicyReload(ctx context.Context, interval time.Duration, onError func(error)) error {
	if ctx == nil || interval <= 0 || onError == nil {
		return errors.New("authz: reload loop requires context, positive interval and error handler")
	}
	if a.maxPolicyAge > 0 && interval >= a.maxPolicyAge {
		return errors.New("authz: reload interval must be shorter than max policy age")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.LoadPolicy(); err != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
