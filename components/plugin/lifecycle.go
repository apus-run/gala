package plugin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"runtime"
	"runtime/debug"
	"slices"
	"sync"
	"time"
)

type (
	// Hook is a single lifecycle callback.
	// Hook 是一个生命周期回调。
	Hook func(context.Context) error

	// HookOption adjusts the behavior of a single hook.
	// HookOption 调整单个钩子的行为。
	HookOption func(*hook)

	// hook 是一个生命周期钩子及其元信息。
	hook struct {
		fn   Hook
		name string // 函数名，用于错误信息和日志
		soft bool
	}

	// Lifecycle 提供四个生命周期钩子，零值可用，并发安全。
	// 它只负责登记和按规则执行，什么时候执行由宿主决定，不绑定任何 server：
	//
	//	type App struct{ Life plugin.Lifecycle }
	//
	//	// 插件在 Install 里挂钩子
	//	func (p *tracingPlugin) Install(a *App) error {
	//	    a.Life.BeforeStart(p.init, plugin.BestEffort()) // 治理钩子：失败不影响宿主
	//	    a.Life.AfterStop(p.flush, plugin.BestEffort())  // 资源清理也放这里，没有单独的 Close 机制
	//	    return nil
	//	}
	//
	//	// 宿主在合适的时刻执行；AfterStop 无条件执行，包括安装或启动中途失败，
	//	// 这样日志、链路的收尾和资源清理不会丢，所以 AfterStop 的钩子要容忍"启动并没有完成"
	//	func run(ctx context.Context, a *App) (err error) {
	//	    defer func() { err = errors.Join(err, a.Life.RunAfterStop(ctx)) }()
	//	    if err := plugin.Install(a, Plugins.All()...); err != nil { return err }
	//	    if err := a.Life.RunBeforeStart(ctx); err != nil { return err }
	//	    /* 启动你自己的服务 */
	//	    ...
	//	}
	//
	// 执行顺序：
	//   - BeforeStart、AfterStart 按登记顺序执行，遇到第一个必须成功的钩子失败就停止并返回。
	//   - BeforeStop、AfterStop 按登记的反序执行（和 defer 一样，后装的先清理），
	//     全部执行完，错误合并返回。
	//
	// 隔离（钩子里的意外不应拖垮宿主）：
	//   - panic 被捕获并变成错误（含堆栈）。
	//   - 每个钩子在独立 goroutine 里执行，宿主最多等 Timeout，或等到传入的 ctx 结束。
	//     Go 无法强杀 goroutine：不理会 ctx、一直阻塞的钩子会在后台继续运行（泄漏），
	//     但宿主不会再等它，所以钩子应当尊重 ctx。
	//   - 一个阶段的总预算由传入的 ctx 控制，用完后剩余钩子会被立刻放弃。
	//     停止阶段请传一个没有被取消的 ctx，例如 context.WithTimeout(context.Background(), 10*time.Second)；
	//     传入已取消的 ctx（比如收到信号后的那个）会让所有钩子立刻被放弃。
	//     取消检查与钩子调用不是原子操作；通过检查后仍可能取消，钩子必须自行响应 ctx。
	//   - 钩子执行时不持锁，所以钩子里可以再登记钩子，不会死锁，但新登记的不会在本轮执行。
	Lifecycle struct {
		// Timeout 是每个钩子的最长执行时间。0 表示不限制（仍受传入 ctx 约束）。
		// 请在首次执行前设置，不要并发修改。
		Timeout time.Duration

		mu          sync.Mutex
		beforeStart []hook
		afterStart  []hook
		beforeStop  []hook
		afterStop   []hook
	}
)

// BestEffort 声明这个钩子"尽力而为"：失败（返回 error、panic、超时）不返回给宿主，
// 也不中断其余钩子，只用 slog.Warn 记录。适合监控、日志、链路这类治理钩子：
// 宁可丢一点遥测，也不能改变宿主的行为。想改变记录的去向，设置 slog 的默认 logger 即可。
func BestEffort() HookOption { return func(h *hook) { h.soft = true } }

// BeforeStart registers a hook for the BeforeStart stage | 登记一个 BeforeStart 阶段的钩子.
func (l *Lifecycle) BeforeStart(fn Hook, opts ...HookOption) { l.add(&l.beforeStart, fn, opts) }

// AfterStart registers a hook for the AfterStart stage | 登记一个 AfterStart 阶段的钩子.
func (l *Lifecycle) AfterStart(fn Hook, opts ...HookOption) { l.add(&l.afterStart, fn, opts) }

// BeforeStop registers a hook for the BeforeStop stage | 登记一个 BeforeStop 阶段的钩子.
func (l *Lifecycle) BeforeStop(fn Hook, opts ...HookOption) { l.add(&l.beforeStop, fn, opts) }

// AfterStop registers a hook for the AfterStop stage | 登记一个 AfterStop 阶段的钩子.
func (l *Lifecycle) AfterStop(fn Hook, opts ...HookOption) { l.add(&l.afterStop, fn, opts) }

// RunBeforeStart runs the BeforeStart hooks in registration order | 按登记顺序执行 BeforeStart 钩子.
func (l *Lifecycle) RunBeforeStart(ctx context.Context) error {
	return l.runStart(ctx, "BeforeStart", l.snapshot(&l.beforeStart))
}

// RunAfterStart runs the AfterStart hooks in registration order | 按登记顺序执行 AfterStart 钩子.
func (l *Lifecycle) RunAfterStart(ctx context.Context) error {
	return l.runStart(ctx, "AfterStart", l.snapshot(&l.afterStart))
}

// RunBeforeStop runs the BeforeStop hooks in reverse registration order | 按登记反序执行 BeforeStop 钩子.
func (l *Lifecycle) RunBeforeStop(ctx context.Context) error {
	return l.runStop(ctx, "BeforeStop", l.snapshot(&l.beforeStop))
}

// RunAfterStop runs the AfterStop hooks in reverse registration order | 按登记反序执行 AfterStop 钩子.
func (l *Lifecycle) RunAfterStop(ctx context.Context) error {
	return l.runStop(ctx, "AfterStop", l.snapshot(&l.afterStop))
}

func (l *Lifecycle) add(dst *[]hook, fn Hook, opts []HookOption) {
	h := hook{fn: fn, name: funcName(fn)}
	for _, o := range opts {
		o(&h)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	*dst = append(*dst, h)
}

func (l *Lifecycle) snapshot(src *[]hook) []hook {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(*src)
}

// runStart：按顺序执行，遇到必须成功的钩子失败就停止。
func (l *Lifecycle) runStart(ctx context.Context, stage string, hs []hook) error {
	for _, h := range hs {
		if err := l.call(ctx, stage, h); err != nil {
			return err
		}
	}
	return nil
}

// runStop：反序执行，全部执行完，错误合并。
func (l *Lifecycle) runStop(ctx context.Context, stage string, hs []hook) error {
	var errs []error
	for i := len(hs) - 1; i >= 0; i-- {
		if err := l.call(ctx, stage, hs[i]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// call 执行一个钩子：隔离 panic 和阻塞；BestEffort 的失败只记录，其余的返回。
func (l *Lifecycle) call(ctx context.Context, stage string, h hook) error {
	err := protect(ctx, l.Timeout, h.fn)
	if err == nil {
		return nil
	}
	if h.soft {
		slog.Warn("plugin: best-effort hook failed", "stage", stage, "hook", h.name, "err", err)
		return nil
	}
	return fmt.Errorf("plugin: %s hook %s: %w", stage, h.name, err)
}

var errHookGoexit = errors.New("plugin: hook called runtime.Goexit")

// protect 在独立 goroutine 里执行 fn：panic 转成错误；
// 超过 timeout（>0 时）或 ctx 结束就不再等待并返回对应错误。
func protect(ctx context.Context, timeout time.Duration, fn Hook) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("abandoned: %w", err)
	}

	done := make(chan error, 1) // 带缓冲：被放弃的 goroutine 之后仍能发送并退出
	go func() {
		var err error
		returned := false
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
			} else if !returned {
				// Goexit 会执行 defer，但不会正常返回。
				err = errHookGoexit
			}
			done <- err // 正常返回、panic、runtime.Goexit 都会走到这里，宿主不会一直等
		}()
		err = fn(ctx)
		returned = true
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("abandoned: %w", ctx.Err())
	}
}

func funcName(fn Hook) string {
	if f := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()); f != nil {
		return f.Name()
	}
	return "unknown"
}
