// Package taskgroup runs tasks under a concurrency limit on top of
// golang.org/x/sync/errgroup | taskgroup 基于 errgroup 按并发上限运行任务.
//
// # Notes | 说明
//
// Two modes share one type: NewTaskGroup cancels the group context after the
// first error, so not-yet-started tasks return the context error instead of
// running (already running tasks are not interrupted); NewUninterruptibleTaskGroup
// lets every task finish and Wait still returns the first error. A panicking
// task is recovered and reported as an error, never counted as success. A
// concurrency count below 1 is clamped to 1, because 0 would block forever.
// 两种模式共用一个类型：NewTaskGroup 在首个错误后取消组上下文，尚未启动的任务直接
// 返回上下文错误而不再执行（已在运行的任务不会被中断）；NewUninterruptibleTaskGroup
// 让所有任务跑完，Wait 仍返回首个错误。panic 的任务会被 recover 并转为错误，不会
// 被视为成功。并发数小于 1 时按 1 处理，因为 0 会永久阻塞。
//
// # Example | 示例
//
//	g := taskgroup.NewTaskGroup(ctx, 4)
//	g.Go(func() error { return fetchA() })
//	g.Go(func() error { return fetchB() })
//	if err := g.Wait(); err != nil {
//		return err
//	}
package taskgroup

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

// TaskGroup runs tasks under a concurrency limit and collects their errors
// TaskGroup 按并发上限运行任务并收集错误.
type TaskGroup interface {
	// Go runs f under the group's concurrency limit | Go 在组的并发上限内运行 f.
	Go(f func() error)
	// Wait blocks until all tasks finish and returns the first error
	// Wait 阻塞至全部任务结束并返回首个错误.
	Wait() error
}

type taskGroup struct {
	errGroup    *errgroup.Group
	ctx         context.Context
	execAllTask atomic.Bool
}

// NewTaskGroup returns a group where the rest tasks stop after one task
// returns an error | NewTaskGroup 创建一个任务组：某个任务出错后其余任务停止.
func NewTaskGroup(ctx context.Context, concurrentCount int) TaskGroup {
	t := &taskGroup{}
	t.errGroup, t.ctx = errgroup.WithContext(ctx)
	// 0 would make Go block forever, so clamp to at least 1
	// 0 会让 Go 永久阻塞，因此至少按 1 处理
	t.errGroup.SetLimit(max(concurrentCount, 1))
	t.execAllTask.Store(false)

	return t
}

// NewUninterruptibleTaskGroup returns a group where the rest tasks continue
// after one task returns an error | NewUninterruptibleTaskGroup 创建一个任务组：
// 某个任务出错后其余任务仍继续执行.
func NewUninterruptibleTaskGroup(ctx context.Context, concurrentCount int) TaskGroup {
	t := &taskGroup{}
	t.errGroup, t.ctx = errgroup.WithContext(ctx)
	// 0 would make Go block forever, so clamp to at least 1
	// 0 会让 Go 永久阻塞，因此至少按 1 处理
	t.errGroup.SetLimit(max(concurrentCount, 1))
	t.execAllTask.Store(true)

	return t
}

// Go implements TaskGroup, recovering panics into errors | 实现 TaskGroup，把 panic 恢复为错误.
func (t *taskGroup) Go(f func() error) {
	t.errGroup.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(t.ctx, "[TaskGroup] exec panic recover", "panic", r)
				// a panicked task must not count as success, otherwise Wait
				// returns nil while the task is half-done
				// panic 的任务不能被当作成功，否则 Wait 返回 nil 而任务做了一半
				err = fmt.Errorf("taskgroup: task panicked: %v", r)
			}
		}()

		if !t.execAllTask.Load() {
			select {
			case <-t.ctx.Done():
				return t.ctx.Err()
			default:
			}
		}

		return f()
	})
}

// Wait implements TaskGroup | 实现 TaskGroup.
func (t *taskGroup) Wait() error {
	return t.errGroup.Wait()
}
