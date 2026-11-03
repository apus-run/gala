package safego

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/panjf2000/ants/v2"
)

// NewPool creates a goroutine pool backed by ants with the given size.
// NewPool 创建一个由 ants 支撑的指定大小的协程池。
func NewPool(size int) (IPool, error) {
	if size <= 0 {
		return nil, fmt.Errorf("pool size must be greater than 0")
	}
	p, err := ants.NewPool(size)
	if err != nil {
		return nil, fmt.Errorf("ants new pool fail, size=%d, err=%w", size, err)
	}
	return &pool{
		p:     p,
		tasks: make([]task, 0),
	}, nil
}

// IPool is a task pool: tasks are added with Add and executed with Exec or
// ExecAll.
// IPool 是任务池：用 Add 添加任务，用 Exec 或 ExecAll 执行。
type IPool interface {
	// Add appends a task to the pool | 向池中追加一个任务
	Add(task func() error)
	// Exec runs tasks, returning on the first error | 执行任务，遇到首个错误即返回
	Exec(ctx context.Context) error
	// ExecAll runs all tasks, ignoring errors | 执行全部任务，忽略错误
	ExecAll(ctx context.Context) error
}

type task = func() error

type pool struct {
	p     *ants.Pool
	tasks []task
}

// Add implements IPool.Add | 实现 IPool.Add。
func (p *pool) Add(task func() error) {
	p.tasks = append(p.tasks, task)
}

// Exec implements IPool.Exec | 实现 IPool.Exec。
func (p *pool) Exec(ctx context.Context) error {
	return p.exec(ctx, false)
}

// ExecAll implements IPool.ExecAll | 实现 IPool.ExecAll。
func (p *pool) ExecAll(ctx context.Context) error {
	return p.exec(ctx, true)
}

func (p *pool) exec(ctx context.Context, ignoreErr bool) error {
	defer p.p.Release()

	var (
		gerr atomic.Value
		wg   sync.WaitGroup
	)

	for idx := range p.tasks {
		if !ignoreErr && gerr.Load() != nil {
			return gerr.Load().(error)
		}

		t := p.tasks[idx]

		wg.Add(1)
		if err := p.p.Submit(func() {
			defer wg.Done()
			defer Recovery(ctx)

			select {
			case <-ctx.Done():
				gerr.Store(ctx.Err())
				return

			default:
				if !ignoreErr && gerr.Load() != nil {
					return
				}
				if err := t(); err != nil {
					gerr.Store(err)
				}
				return
			}
		}); err != nil {
			return fmt.Errorf("pool submit fail, err=%w", err)
		}
	}

	wg.Wait()
	if gerr.Load() != nil {
		return gerr.Load().(error)
	}

	return nil
}
