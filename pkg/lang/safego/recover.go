package safego

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
)

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
