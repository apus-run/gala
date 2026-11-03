package safego

import (
	"github.com/pkg/errors"

	"github.com/apus-run/gala/pkg/lang/must"
)

// RunE executes the given function, and will wrap any panic that it encountered as an error. Any error returned
// from fn() in normal execution is passed through as-is.
// Note: the error is passed through `utils.Should`, resulting in a panic on debug builds and a log message on
// release builds.
// RunE 执行给定函数，把遇到的 panic 包装为 error 返回；fn() 正常返回的 error 原样透传。
// 注意：结果会经过 must.ShouldErr 处理——debug 构建下 panic，release 构建下记录日志。
func RunE(fn func() error) error {
	return must.ShouldErr(runE(fn))
}

// runE is like RunE, but the result is not passed through utils.Should for better testability.
// runE 类似 RunE，但结果不经过 must.ShouldErr 处理，便于测试。
func runE(fn func() error) (err error) {
	panicked := true
	defer func() {
		if !panicked {
			return
		}
		r := recover()
		rErr, _ := r.(error)
		if rErr == nil {
			rErr = errors.Errorf("recovered: %v", r)
		}
		err = errors.Wrap(rErr, "caught panic")
	}()

	err = fn()
	panicked = false
	return
}

// Run executes the given function, and will wrap any panic that it encountered as an error.
// This function returns nil if and only if fn() did not cause a panic.
// Run 执行给定函数，把遇到的 panic 包装为 error 返回；当且仅当 fn() 未 panic 时返回 nil。
func Run(fn func()) error {
	return RunE(func() error {
		fn()
		return nil
	})
}
