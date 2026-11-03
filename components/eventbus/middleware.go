package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"
)

// Middleware is a function that wraps a Handler | Middleware 是包装 Handler 的函数。
type Middleware func(Handler) Handler

// LoggingMiddleware logs event handling | LoggingMiddleware 记录事件处理的日志。
func LoggingMiddleware(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next Handler) Handler {
		return EventHandlerFunc(func(ctx context.Context, event *Event) error {
			logger.InfoContext(ctx, "handling event", "type", event.Type, "id", event.ID, "source", event.Source)
			err := next.Handle(ctx, event)
			if err != nil {
				logger.ErrorContext(ctx, "error handling event", "id", event.ID, "error", err)
			} else {
				logger.DebugContext(ctx, "successfully handled event", "id", event.ID)
			}
			return err
		})
	}
}

// RecoveryMiddleware recovers from panics in handlers | RecoveryMiddleware 从 handler 的 panic 中恢复。
func RecoveryMiddleware(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next Handler) Handler {
		return EventHandlerFunc(func(ctx context.Context, event *Event) (err error) {
			defer func() {
				if r := recover(); r != nil {
					logger.ErrorContext(ctx, "panic recovered in event handler", "type", event.Type, "panic", r)
					err = &PanicError{Value: r}
				}
			}()
			return next.Handle(ctx, event)
		})
	}
}

// TimeoutMiddleware adds a cooperative timeout to event handling. The wrapped
// handler must observe ctx for prompt cancellation; no orphan goroutine is
// created when it does not.
// TimeoutMiddleware 为事件处理添加协作式超时。被包装的 handler 必须感知 ctx
// 才能及时取消；不感知时也不会产生孤儿 goroutine。
func TimeoutMiddleware(timeout time.Duration) Middleware {
	if timeout <= 0 {
		panic("eventbus: timeout must be positive")
	}
	return func(next Handler) Handler {
		return EventHandlerFunc(func(ctx context.Context, event *Event) error {
			timeoutErr := &TimeoutError{EventID: event.ID, EventType: event.Type, Timeout: timeout}
			ctx, cancel := context.WithTimeoutCause(ctx, timeout, timeoutErr)
			defer cancel()
			err := next.Handle(ctx, event)
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return err
		})
	}
}

// RetryMiddleware retries failed event handling | RetryMiddleware 对失败的事件处理进行重试。
func RetryMiddleware(maxRetries int, delay time.Duration) Middleware {
	if maxRetries < 0 {
		panic("eventbus: max retries must not be negative")
	}
	if delay < 0 {
		panic("eventbus: retry delay must not be negative")
	}
	return func(next Handler) Handler {
		return EventHandlerFunc(func(ctx context.Context, event *Event) error {
			var err error
			for i := 0; i <= maxRetries; i++ {
				err = next.Handle(ctx, event)
				if err == nil {
					return nil
				}

				if i < maxRetries {
					timer := time.NewTimer(delay)
					select {
					case <-ctx.Done():
						timer.Stop()
						return context.Cause(ctx)
					case <-timer.C:
					}
				}
			}
			return err
		})
	}
}

// MetricsMiddleware collects metrics for event handling | MetricsMiddleware 收集事件处理的指标。
func MetricsMiddleware(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next Handler) Handler {
		return EventHandlerFunc(func(ctx context.Context, event *Event) error {
			start := time.Now()
			err := next.Handle(ctx, event)
			duration := time.Since(start)

			logger.InfoContext(ctx, "event handling metrics", "type", event.Type, "duration", duration, "success", err == nil)

			return err
		})
	}
}

// Chain chains multiple middlewares together | Chain 将多个中间件串联起来。
func Chain(middlewares ...Middleware) Middleware {
	return func(handler Handler) Handler {
		for _, middleware := range slices.Backward(middlewares) {
			handler = middleware(handler)
		}
		return handler
	}
}

// PanicError represents a panic that occurred during event handling | PanicError 表示事件处理期间发生的 panic。
type PanicError struct {
	// Value is the recovered panic value | Value 是恢复出的 panic 值。
	Value any
}

// Error implements the error interface | Error 实现 error 接口。
func (e *PanicError) Error() string {
	return "panic in event handler: " + fmt.Sprint(e.Value)
}

// TimeoutError represents a timeout during event handling | TimeoutError 表示事件处理期间发生的超时。
type TimeoutError struct {
	// EventID is the id of the event that timed out | EventID 是超时事件的 id。
	EventID string
	// EventType is the type of the event that timed out | EventType 是超时事件的类型。
	EventType EventType
	// Timeout is the configured timeout | Timeout 是配置的超时时长。
	Timeout time.Duration
}

// Error implements the error interface | Error 实现 error 接口。
func (e *TimeoutError) Error() string {
	return "event handling timeout"
}
