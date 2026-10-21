// Package ginbreaker demonstrates building a Gin middleware with the same
// capability as ginx/middlewares/circuitbreaker on top of the transport-agnostic
// component, following ginx's NewBuilder chaining style. Divergences are
// deliberate and documented per field: gobreaker v2 has no injectable clock,
// and its half-open close threshold is MaxRequests itself, so SuccessThreshold
// cannot be expressed.
package ginbreaker

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	gobreaker "github.com/sony/gobreaker/v2"

	"github.com/apus-run/gala/components/circuitbreaker"
)

// Builder assembles the middleware. Chain setters, then Build; Breaker exposes
// the built breaker for ForceOpen/ForceClose/Reset and Stats. Start from
// NewInternalServiceBuilder or NewExternalAPIBuilder for the keystoneGo
// profiles, or NewBuilder for the ginx middleware's defaults.
type Builder struct {
	failureThreshold int
	timeout          time.Duration
	maxRequests      uint32
	interval         time.Duration
	// rateMinRequests/failureRate switch tripping from consecutive failures to
	// a failure rate once SetFailureRate configured one.
	rateMinRequests uint32
	failureRate     float64
	isFailure       func(c *gin.Context, err error) bool
	onOpen          gin.HandlerFunc
	onHalfOpen      gin.HandlerFunc
	onClose         func()
	onStateChange   func(from, to gobreaker.State)
	breaker         *circuitbreaker.Breaker
}

func NewBuilder() *Builder {
	return &Builder{
		failureThreshold: 5,
		timeout:          5 * time.Second,
		maxRequests:      1,
		isFailure: func(c *gin.Context, err error) bool {
			return err != nil || c.Writer.Status() >= http.StatusInternalServerError
		},
		onOpen: func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "service unavailable"})
		},
		onHalfOpen: func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "service under recovery"})
		},
	}
}

// SetFailureThreshold sets the consecutive failures that trip the circuit.
func (b *Builder) SetFailureThreshold(n int) *Builder { b.failureThreshold = n; return b }

// SetTimeout sets the open duration before half-open probes are admitted.
func (b *Builder) SetTimeout(d time.Duration) *Builder { b.timeout = d; return b }

// SetMaxRequests sets the half-open probes admitted concurrently. Upstream
// coupling: the circuit also closes only after this many consecutive successes.
func (b *Builder) SetMaxRequests(n uint32) *Builder { b.maxRequests = n; return b }

// SetInterval sets the closed-state count window. Zero means failures
// accumulate until tripping.
func (b *Builder) SetInterval(d time.Duration) *Builder { b.interval = d; return b }

// SetIsFailure replaces the failure detector; default counts err or >= 500.
func (b *Builder) SetIsFailure(fn func(*gin.Context, error) bool) *Builder {
	b.isFailure = fn
	return b
}

// SetOnOpen replaces the open refusal response (default 503).
func (b *Builder) SetOnOpen(h gin.HandlerFunc) *Builder { b.onOpen = h; return b }

// SetOnHalfOpen replaces the half-open saturation response (default 429).
func (b *Builder) SetOnHalfOpen(h gin.HandlerFunc) *Builder { b.onHalfOpen = h; return b }

// SetOnClose notifies after a transition to closed, once the upstream lock
// has been released. The callback may query the breaker state and metrics.
func (b *Builder) SetOnClose(fn func()) *Builder { b.onClose = fn; return b }

// SetFailureRate switches tripping from consecutive failures to a failure
// rate: the circuit opens once at least minRequests requests in the current
// window failed at rate or more of the time (0.5 = 50%). ReadyToTrip is
// consulted on failures only, so the trip lands on a failing request.
func (b *Builder) SetFailureRate(minRequests int, rate float64) *Builder {
	b.rateMinRequests, b.failureRate = uint32(minRequests), rate
	return b
}

// SetOnStateChange observes every transition, delivered after the upstream
// lock is released like OnClose. Use it for transition logging.
func (b *Builder) SetOnStateChange(fn func(from, to gobreaker.State)) *Builder {
	b.onStateChange = fn
	return b
}

// NewInternalServiceBuilder is the internal service-to-service profile
// (keystoneGo's DefaultBreaker): tolerate noise, trip only after 6
// consecutive failures in a 60s window, then probe 3 at a time after 30s.
func NewInternalServiceBuilder() *Builder {
	return NewBuilder().
		SetFailureThreshold(6).
		SetMaxRequests(3).
		SetTimeout(30 * time.Second).
		SetInterval(60 * time.Second)
}

// NewExternalAPIBuilder is the third-party API profile (keystoneGo's
// ExternalAPIBreaker): fail fast — trip once ≥50% of ≥5 requests in a 60s
// window failed, then probe 5 at a time after a 60s cool-down.
func NewExternalAPIBuilder() *Builder {
	return NewBuilder().
		SetFailureRate(5, 0.5).
		SetMaxRequests(5).
		SetTimeout(60 * time.Second).
		SetInterval(60 * time.Second)
}

// Breaker returns the breaker built by Build; nil before that.
func (b *Builder) Breaker() *circuitbreaker.Breaker { return b.breaker }

// Build assembles the breaker and its middleware.
func (b *Builder) Build() gin.HandlerFunc {
	threshold, minRequests, rate := uint32(b.failureThreshold), b.rateMinRequests, b.failureRate
	readyToTrip := func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= threshold }
	if rate > 0 {
		readyToTrip = func(c gobreaker.Counts) bool {
			return c.Requests >= minRequests && float64(c.TotalFailures)/float64(c.Requests) >= rate
		}
	}
	onClose, onStateChange := b.onClose, b.onStateChange
	// OnStateChange runs under gobreaker's lock. Queue every transition and
	// deliver after Allow/done returns so callbacks can safely inspect the
	// breaker; the flushing request is one of the requests that caused them.
	var mu sync.Mutex
	var queued []func()
	b.breaker = circuitbreaker.New(gobreaker.Settings{
		Name:        "ginbreaker",
		MaxRequests: b.maxRequests,
		Interval:    b.interval,
		Timeout:     b.timeout,
		ReadyToTrip: readyToTrip,
		OnStateChange: func(_ string, from, to gobreaker.State) {
			mu.Lock()
			defer mu.Unlock()
			queued = append(queued, func() {
				if onStateChange != nil {
					onStateChange(from, to)
				}
				if to == gobreaker.StateClosed && onClose != nil {
					onClose()
				}
			})
		},
	})
	return b.middleware(func() {
		mu.Lock()
		drain := queued
		queued = nil
		mu.Unlock()
		for _, notify := range drain {
			notify()
		}
	})
}

// Middleware adapts Gin requests to the breaker. Panics count as failures and
// are rethrown to the application's Recovery middleware, whichever side it is on.
// Built handlers capture a copy: later builder changes do not affect them,
// matching the ginx middleware's builder contract.
func (b *Builder) middleware(notifyClosed func()) gin.HandlerFunc {
	breaker, onOpen, onHalfOpen, isFailure := b.breaker, b.onOpen, b.onHalfOpen, b.isFailure
	return func(c *gin.Context) {
		defer notifyClosed()
		done, err := breaker.Allow()
		if err != nil {
			c.Abort()
			if errors.Is(err, circuitbreaker.ErrTooManyRequests) {
				onHalfOpen(c)
			} else {
				onOpen(c)
			}
			return
		}
		defer func() {
			if value := recover(); value != nil {
				done(fmt.Errorf("panic: %v", value))
				panic(value)
			}
		}()
		previous := len(c.Errors)
		c.Next()
		var failures []error
		if len(c.Errors) > previous {
			for _, failure := range c.Errors[previous:] {
				failures = append(failures, failure.Err)
			}
		}
		if isFailure(c, errors.Join(failures...)) {
			done(errors.New("failure"))
			return
		}
		done(nil)
	}
}

// HealthHandler reports 503 while open, 200 otherwise. Register it outside the
// protected route group so an open circuit cannot hide it.
func HealthHandler(b *circuitbreaker.Breaker) gin.HandlerFunc {
	return func(c *gin.Context) {
		s := b.Stats()
		status := http.StatusOK
		if s.State == gobreaker.StateOpen {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{"state": s.State.String(), "healthy": s.State != gobreaker.StateOpen, "total": s.Total, "rejected": s.Rejected})
	}
}
