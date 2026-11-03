// Package must provides must-style helpers that panic (or crash) on errors.
// 包 must 提供 must 风格的助手函数：出错即 panic（或崩溃）。
package must

import (
	"fmt"
	"time"

	"github.com/apus-run/gala/pkg/lang/buildinfo"
	"github.com/apus-run/gala/pkg/lang/debug"
)

const (
	hardPanicDelay = 5 * time.Second
)

// hardPanic is like panic, but on debug builds additionally ensures that the panic will cause a crash with a full
// goroutine dump, independently of any recovery handlers.
// hardPanic 类似 panic，但在 debug 构建下会额外确保最终以完整 goroutine 转储崩溃，
// 不受任何 recover 处理器影响。
func hardPanic(v any) {
	if !buildinfo.ReleaseBuild {
		trace := debug.GetLazyStacktrace(2)
		time.AfterFunc(hardPanicDelay, func() {
			panic(fmt.Sprintf("Re-triggering panic %v as unrecoverable. Original stacktrace:\n%s", v, trace))
		})
	}
	panic(v)
}

// Must panics if any of the given errors is non-nil, and does nothing otherwise.
// Must 在任一给定 error 非 nil 时 panic，否则什么都不做。
func Must(errs ...error) {
	CrashOnError(errs...)
}

// CrashOnError is an alternative to `Must`. It was introduced because `Must(err)` looks confusing.
// CrashOnError 是 Must 的替代名——Must(err) 的写法容易误解，故提供此函数。
func CrashOnError(errs ...error) {
	for _, err := range errs {
		if err != nil {
			hardPanic(fmt.Sprintf("%+v", err))
		}
	}
}

// ShouldErr panics on development builds and logs on release builds
// The expectation is that this function will be called with an error wrapped by errors.Wrap
// so that tracing is easier
// ShouldErr 在开发构建下 panic、在 release 构建下返回错误；
// 预期传入经 errors.Wrap 包装的错误，便于追踪。
func ShouldErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			if buildinfo.ReleaseBuild {
				return fmt.Errorf("%+v", err)
			} else {
				hardPanic(err)
			}
			return err
		}
	}
	return nil
}

// Should wraps ShouldErr without returning the error. This removes gosec G104.
// Should 包装 ShouldErr 但不返回错误，以此消除 gosec G104 告警。
func Should(errs ...error) {
	_ = ShouldErr(errs...)
}
