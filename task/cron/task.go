// Lifecycle adapter for the root gala package: expose a Cron through the
// Start(context.Context) error / Stop(context.Context) error contract used by
// gala.WithTasks.
// 根 gala 包的生命周期适配：以 gala.WithTasks 所需的
// Start(context.Context) error / Stop(context.Context) error 契约暴露 Cron。

package cron

import (
	"context"
	"errors"
)

// Task adapts a Cron to the lifecycle contract used by gala.WithTasks.
//
// It deliberately does not import the root module: task/cron stays
// independent of github.com/apus-run/gala, and the adapter satisfies the
// gala.Task interface structurally.
// Task 把 Cron 适配为 gala.WithTasks 使用的生命周期契约。
//
// 它刻意不 import 根模块：task/cron 保持独立于 github.com/apus-run/gala，
// 适配器以结构化方式满足 gala.Task 接口。
type Task struct {
	cron *Cron
}

// Compile-time check of the gala.Task method set; the root module cannot be
// imported here, so the contract is duplicated structurally.
// 编译期检查 gala.Task 的方法集；此处不能 import 根模块，因此以结构化方式
// 复述该契约。
var _ interface {
	Start(context.Context) error
	Stop(context.Context) error
} = (*Task)(nil)

// NewTask wraps c in a lifecycle adapter. It performs no I/O and leaves the
// scheduler stopped; Start starts it.
//
// A nil c yields a Task whose Start returns an error and whose Stop is a
// no-op, so a missing scheduler is reported instead of panicking during
// shutdown.
// NewTask 用生命周期适配器包装 c。它不做 I/O，且不启动调度器，由 Start 启动。
//
// c 为 nil 时返回的 Task，其 Start 返回错误、Stop 为空操作，从而在停机前报告
// 缺失的调度器而不是 panic。
func NewTask(c *Cron) *Task {
	return &Task{cron: c}
}

// Start starts the scheduler and returns immediately; the ticker runs in the
// background until Stop. It implements the gala.Task Start contract.
// Start 启动调度器后立即返回；ticker 在后台运行直到 Stop。实现 gala.Task 的
// Start 契约。
func (t *Task) Start(context.Context) error {
	if t == nil || t.cron == nil {
		return errors.New("cron: nil Cron")
	}
	t.cron.Start()

	return nil
}

// Stop stops the ticker, waits for in-flight jobs or ctx, then releases every
// lease, mirroring [Cron.Shutdown]. It is safe to call when Start never ran
// and when called more than once.
// Stop 停止 ticker，等待运行中的任务或 ctx，然后释放所有租约，语义同
// [Cron.Shutdown]。即使 Start 未曾调用或重复调用，它也是安全的。
func (t *Task) Stop(ctx context.Context) error {
	if t == nil || t.cron == nil {
		return nil
	}

	return t.cron.Shutdown(ctx)
}
