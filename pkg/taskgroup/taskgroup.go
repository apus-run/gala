package taskgroup

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

type TaskGroup interface {
	Go(f func() error)
	Wait() error
}

type taskGroup struct {
	errGroup    *errgroup.Group
	ctx         context.Context
	execAllTask atomic.Bool
}

// NewTaskGroup if one task return error, the rest task will stop
func NewTaskGroup(ctx context.Context, concurrentCount int) TaskGroup {
	t := &taskGroup{}
	t.errGroup, t.ctx = errgroup.WithContext(ctx)
	t.errGroup.SetLimit(max(concurrentCount, 1)) // 0 会让 Go 永久阻塞
	t.execAllTask.Store(false)

	return t
}

// NewUninterruptibleTaskGroup if one task return error, the rest task will continue
func NewUninterruptibleTaskGroup(ctx context.Context, concurrentCount int) TaskGroup {
	t := &taskGroup{}
	t.errGroup, t.ctx = errgroup.WithContext(ctx)
	t.errGroup.SetLimit(max(concurrentCount, 1)) // 0 会让 Go 永久阻塞
	t.execAllTask.Store(true)

	return t
}

func (t *taskGroup) Go(f func() error) {
	t.errGroup.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(t.ctx, "[TaskGroup] exec panic recover", "panic", r)
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

func (t *taskGroup) Wait() error {
	return t.errGroup.Wait()
}
