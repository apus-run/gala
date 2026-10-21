package circuitbreaker

import (
	"time"

	"github.com/gin-gonic/gin"
)

// Builder assembles a CircuitBreaker middleware with ginx's chaining style.
// New applies DefaultConfig to every unset field, so the zero Builder is a
// valid starting point; Build constructs the circuit and Breaker exposes it
// for ForceOpen/ForceClose and metrics. Do not reuse a Builder after Build.
type Builder struct {
	config  Config
	breaker *CircuitBreaker
}

func NewBuilder() *Builder {
	return &Builder{}
}

// SetFailureThreshold sets the failure count that trips the circuit.
func (b *Builder) SetFailureThreshold(n int) *Builder { b.config.FailureThreshold = n; return b }

// SetTimeout sets how long the circuit stays open before recovery probes;
// it is not an execution timeout.
func (b *Builder) SetTimeout(d time.Duration) *Builder { b.config.Timeout = d; return b }

// SetSuccessThreshold sets the successful half-open probes that close the circuit.
func (b *Builder) SetSuccessThreshold(n int) *Builder { b.config.SuccessThreshold = n; return b }

// SetHalfOpenMaxConcurrent sets the maximum concurrent probes in half-open state.
func (b *Builder) SetHalfOpenMaxConcurrent(n int) *Builder {
	b.config.HalfOpenMaxConcurrent = n
	return b
}

// SetInterval sets the failure-count window; zero means failures accumulate
// until the circuit opens.
func (b *Builder) SetInterval(d time.Duration) *Builder { b.config.Interval = d; return b }

// SetIsFailure replaces the failure detector (default: err or status >= 500).
func (b *Builder) SetIsFailure(fn func(*gin.Context, error) bool) *Builder {
	b.config.IsFailure = fn
	return b
}

// SetClock replaces the time source, which drives every state transition; for
// tests only.
func (b *Builder) SetClock(fn func() time.Time) *Builder { b.config.Clock = fn; return b }

// SetOnOpen replaces the response for requests refused by an open circuit.
func (b *Builder) SetOnOpen(h gin.HandlerFunc) *Builder { b.config.OnOpen = h; return b }

// SetOnHalfOpen replaces the response for requests refused by half-open saturation.
func (b *Builder) SetOnHalfOpen(h gin.HandlerFunc) *Builder { b.config.OnHalfOpen = h; return b }

// SetOnClose registers the notification that runs after the probe closing the
// circuit. It is a notification, not a response writer.
func (b *Builder) SetOnClose(h gin.HandlerFunc) *Builder { b.config.OnClose = h; return b }

// Breaker returns the circuit built by Build; nil before that.
func (b *Builder) Breaker() *CircuitBreaker { return b.breaker }

// Build constructs the circuit from the chained settings and returns its middleware.
func (b *Builder) Build() gin.HandlerFunc {
	b.breaker = New(b.config)
	return Middleware(b.breaker)
}
