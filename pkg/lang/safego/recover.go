package safego

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
)

// Recover catches a panic in the calling function: if errPtr is non-nil the
// panic is converted to an error and stored there, and the panic is always
// logged with a stacktrace.
// Recover 捕获调用方的 panic：errPtr 非 nil 时把 panic 转为 error 写入；
// 无论何种情况都会带堆栈记录日志。
func Recover(ctx context.Context, errPtr *error) {
	e := recover()
	if e == nil {
		return
	}

	var tmpErr error
	if errPtr != nil && *errPtr != nil {
		tmpErr = fmt.Errorf("panic occured, originErr=%v, reason=%v", *errPtr, e)
	} else {
		tmpErr = fmt.Errorf("panic occurred, reason=%v", e)
	}

	if errPtr != nil {
		*errPtr = tmpErr
	}

	err := fmt.Errorf("%v", e)
	slog.ErrorContext(ctx, "[catch panic]", slog.Any("err", err), slog.String("stacktrace", string(debug.Stack())))
}

// Recovery catches a panic in the calling function and logs it with a
// stacktrace, discarding the panic value.
// Recovery 捕获调用方的 panic 并带堆栈记录日志，不向外传递 panic 值。
func Recovery(ctx context.Context) {
	e := recover()
	if e == nil {
		return
	}

	if ctx == nil {
		ctx = context.Background()
	}

	err := fmt.Errorf("%v", e)
	slog.ErrorContext(ctx, "[catch panic]", slog.Any("err", err), slog.String("stacktrace", string(debug.Stack())))
}
