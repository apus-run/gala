package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Middleware is a function that wraps a Handler
type Middleware func(Handler) Handler

// LoggingMiddleware logs event handling
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

// RecoveryMiddleware recovers from panics in handlers
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

// RetryMiddleware retries failed event handling
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

// MetricsMiddleware collects metrics for event handling
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

// Chain chains multiple middlewares together
func Chain(middlewares ...Middleware) Middleware {
	return func(handler Handler) Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			handler = middlewares[i](handler)
		}
		return handler
	}
}

// PanicError represents a panic that occurred during event handling
type PanicError struct {
	Value any
}

func (e *PanicError) Error() string {
	return "panic in event handler: " + fmt.Sprint(e.Value)
}

// TimeoutError represents a timeout during event handling
type TimeoutError struct {
	EventID   string
	EventType EventType
	Timeout   time.Duration
}

func (e *TimeoutError) Error() string {
	return "event handling timeout"
}
