package eventbus

import (
	"context"
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
			logger.Info("Handling event", "type", event.Type, "id", event.ID, "source", event.Source)
			err := next.Handle(ctx, event)
			if err != nil {
				logger.Error("Error handling event", "id", event.ID, "error", err)
			} else {
				logger.Debug("Successfully handled event", "id", event.ID)
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
					logger.Error("Panic recovered in event handler", "type", event.Type, "panic", r)
					err = &PanicError{Value: r}
				}
			}()
			return next.Handle(ctx, event)
		})
	}
}

// TimeoutMiddleware adds timeout to event handling
func TimeoutMiddleware(timeout time.Duration) Middleware {
	return func(next Handler) Handler {
		return EventHandlerFunc(func(ctx context.Context, event *Event) error {
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			done := make(chan error, 1)
			go func() {
				done <- next.Handle(ctx, event)
			}()

			select {
			case err := <-done:
				return err
			case <-ctx.Done():
				return &TimeoutError{
					EventID:   event.ID,
					EventType: event.Type,
					Timeout:   timeout,
				}
			}
		})
	}
}

// RetryMiddleware retries failed event handling
func RetryMiddleware(maxRetries int, delay time.Duration) Middleware {
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
						return ctx.Err()
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

			logger.Info("Event handling metrics", "type", event.Type, "duration", duration, "success", err == nil)

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
	Value interface{}
}

func (e *PanicError) Error() string {
	return "panic in event handler"
}

// TimeoutError represents a timeout during event handling
type TimeoutError struct {
	EventID   string
	EventType string
	Timeout   time.Duration
}

func (e *TimeoutError) Error() string {
	return "event handling timeout"
}
