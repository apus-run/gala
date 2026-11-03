// Package graceful awaits a shutdown signal or context cancellation, then
// waits for registered goroutines to finish | 等待关停信号或 context 取消，
// 再等待已登记的 goroutine 退出.
//
// # Notes | 说明
//
// NewGraceful takes the application context and its cancel function. Await
// blocks until SIGINT, SIGTERM, SIGQUIT or ctx.Done, then cancels the context
// and waits for every goroutine registered with Add/Done, bounded by
// SetGracefulTimeout (default 10s, k8s-friendly); past the deadline it
// returns ErrTimeout. Progress is logged via log/slog. Await is synchronous
// and belongs at the end of main, never in a goroutine.
// NewGraceful 接收应用 context 与其 cancel 函数。Await 阻塞等待 SIGINT、
// SIGTERM、SIGQUIT 或 ctx.Done，随后 cancel 并等待所有经 Add/Done 登记的
// goroutine，上限由 SetGracefulTimeout 设置（默认 10s，对 k8s 友好），超时
// 返回 ErrTimeout。过程经 log/slog 记录。Await 是同步调用，放在 main 末尾，
// 不要放进 goroutine。
//
// # Features | 功能
//
// - Add/Done register goroutines to await | Add/Done 登记需等待的 goroutine
// - SetGracefulTimeout bounds the shutdown wait | 限定关停等待时长
// - Gracefuller abstracts the contract | Gracefuller 接口抽象该契约
//
// # Example | 示例
//
//	ctx, cancel := context.WithCancel(context.Background())
//	g := graceful.NewGraceful(ctx, cancel)
//	g.Add(1)
//	go func() { defer g.Done(); <-ctx.Done(); shutdown() }()
//	if err := g.Await(); err != nil { /* timed out */ }
package graceful

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// ErrTimeout is returned when not all registered goroutines finish within the
// graceful timeout | 超过优雅关停时限仍未全部退出时返回 ErrTimeout.
var ErrTimeout = errors.New("timeout error")

// Gracefuller is the contract of a graceful shutdown helper | Gracefuller 是优雅关停辅助器的契约.
type Gracefuller interface {
	// Add is a clone of (sync.WaitGroup).Add() which must be called on main goroutines
	// Add 等价于 (sync.WaitGroup).Add()，须在主 goroutine 上调用.
	Add(n int)
	// Done is a clone of (sync.WaitGroup).Done() which must be called on closing main goroutines
	// Done 等价于 (sync.WaitGroup).Done()，须在主 goroutine 收尾时调用.
	Done()
	// SetGracefulTimeout sets the timeout used when waiting for all goroutines
	// to finish after the context is closed (default 10s)
	// SetGracefulTimeout 设置 context 关闭后等待全部 goroutine 退出的超时（默认 10s）.
	SetGracefulTimeout(timeout time.Duration)
	// Await catches one of the channels (osSigsCh:[syscall.SIGINT, syscall.SIGTERM])
	// or ctx.Done() and awaits while all main goroutines will be finished by sync.WaitGroup.
	// NOTE: Await is synchronous (blocking) and must not be called from a goroutine.
	// Also, it must be called at the end of the main function.
	// Await 监听信号通道（osSigsCh:[syscall.SIGINT, syscall.SIGTERM]）或 ctx.Done()，
	// 然后按 sync.WaitGroup 等待所有主 goroutine 结束。
	// 注意：Await 是同步（阻塞）调用，不能放进 goroutine，应放在 main 函数末尾。
	Await() error
}

// Graceful is the default Gracefuller implementation | Graceful 是 Gracefuller 的默认实现.
type Graceful struct {
	wg       *sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
	timeout  time.Duration
	osSigsCh chan os.Signal
}

// NewGraceful is a constructor of new Graceful shutdown implementation.
// Accepts main context.Context and context.CancelFunc.
// NewGraceful 是新的 Graceful 关停实现的构造器，接收主 context.Context 与
// context.CancelFunc。
func NewGraceful(ctx context.Context, cancel context.CancelFunc) *Graceful {
	gsh := &Graceful{
		wg:     &sync.WaitGroup{},
		ctx:    ctx,
		cancel: cancel,
		// default value, k8s friendly
		// 默认值，对 k8s 友好
		timeout:  time.Second * 10,
		osSigsCh: make(chan os.Signal, 1),
	}

	signal.Notify(gsh.osSigsCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	return gsh
}

// Add is a clone of (sync.WaitGroup).Add() which must be called on main goroutines
// Add 等价于 (sync.WaitGroup).Add()，须在主 goroutine 上调用.
func (g *Graceful) Add(n int) {
	g.wg.Add(n)
}

// Done is a clone of (sync.WaitGroup).Done() which must be called on closing main goroutines
// Done 等价于 (sync.WaitGroup).Done()，须在主 goroutine 收尾时调用.
func (g *Graceful) Done() {
	g.wg.Done()
}

// SetGracefulTimeout sets the timeout used when waiting for all goroutines
// to finish after the context is closed (default 10s)
// SetGracefulTimeout 设置 context 关闭后等待全部 goroutine 退出的超时（默认 10s）.
func (g *Graceful) SetGracefulTimeout(timeout time.Duration) {
	g.timeout = timeout
}

// Await catches one of the channels (osSigsCh:[syscall.SIGINT, syscall.SIGTERM])
// or ctx.Done() and awaits while all main goroutines will be finished by sync.WaitGroup.
// NOTE: Await is synchronous (blocking) and must not be called from a goroutine.
// Also, it must be called at the end of the main function.
// Await 监听信号通道（osSigsCh:[syscall.SIGINT, syscall.SIGTERM]）或 ctx.Done()，
// 然后按 sync.WaitGroup 等待所有主 goroutine 结束。
// 注意：Await 是同步（阻塞）调用，不能放进 goroutine，应放在 main 函数末尾。
func (g *Graceful) Await() error {
	select {
	case <-g.ctx.Done():
		slog.Info("context done, graceful shutdown started")
	case sig := <-g.osSigsCh:
		slog.Info("os signal received, graceful shutdown started", slog.Any("signal", sig))
	}
	return g.cancelAndAwaitWithTimeout()
}

func (g *Graceful) cancelAndAwaitWithTimeout() error {
	// cancel context of all application
	// cancel 掉整个应用的 context
	g.cancel()

	// timeout timer
	// 超时定时器
	ttrCh := time.NewTimer(g.timeout)
	defer ttrCh.Stop()

	// is success channel
	// 成功信号通道
	sucCh := make(chan struct{})
	go func() {
		g.wg.Wait()
		close(sucCh)
	}()

	// result channel
	// 结果通道
	errCh := make(chan error)

	go func() {
		// this goroutine is writer-side of channel and correctly close the channel is here
		// 本 goroutine 是通道的写方，正确关闭通道的位置在这里
		defer close(errCh)

		select {
		case <-sucCh:
			slog.Info("service was gracefully shut down")
			errCh <- nil
		case <-ttrCh.C:
			slog.Warn("not all goroutines were closed within timeout", slog.Duration("timeout", g.timeout))
			errCh <- ErrTimeout
		}
	}()

	return <-errCh
}
