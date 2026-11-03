// Package shutdown stops a server gracefully within a deadline; internal to
// the server tree | shutdown 在限期之内优雅停止服务，为 server 体系内部包.
//
// # Notes | 说明
//
// [ShutdownWithContext] runs shutdownFunc while watching the context: when
// the deadline hits first, forceCloseFunc runs and both errors are joined
// in the result. Internal package, not for external use.
// [ShutdownWithContext] 执行 shutdownFunc 并监听 context：先到截止时间则调用
// forceCloseFunc，两个错误合并在结果中返回。内部包，不对外使用。
package shutdown

import (
	"context"
	"errors"
)

// ShutdownWithContext handles the shutdown process with context timeout.
// It takes a shutdown function and a force close function as parameters.
// If the context times out, the force close function is called.
// ShutdownWithContext 带超时地处理关闭流程：先执行 shutdown，ctx 超时则调用
// 强制关闭函数，两者的错误合并返回.
func ShutdownWithContext(ctx context.Context, shutdownFunc func(ctx context.Context) error, forceCloseFunc func() error) error {
	errCh := make(chan error, 1) // Channel to receive shutdown error | 接收关闭错误的 channel

	go func() {
		errCh <- shutdownFunc(ctx) // Run shutdownFunc in a goroutine and send any error to errCh | 在 goroutine 中执行 shutdownFunc，错误发入 errCh
	}()

	// Wait for either the context to be done or shutdownFunc to complete
	// 等待 ctx 结束或 shutdownFunc 完成
	select {
	case <-ctx.Done(): // Context timeout reached | ctx 超时
		err := ctx.Err()

		if forceCloseFunc != nil {
			err = errors.Join(err, forceCloseFunc()) // Attempt force close if available | 可用时尝试强制关闭
		}

		return err
	case err := <-errCh:
		return err
	}
}
