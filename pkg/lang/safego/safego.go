// Package safego provides panic-safe helpers: goroutines and pools with
// panic recovery, and functions that convert panics into errors.
// 包 safego 提供带 panic 恢复的安全协程、协程池，
// 以及把 panic 转换为 error 的函数。
package safego

import (
	"context"
)

// Go runs fn in a new goroutine, recovering from any panic inside it and
// logging it with the context.
// Go 在新 goroutine 中运行 fn，内部通过 Recovery 捕获 panic 并记录日志。
func Go(ctx context.Context, fn func()) {
	go func() {
		defer Recovery(ctx)
		fn()
	}()
}
