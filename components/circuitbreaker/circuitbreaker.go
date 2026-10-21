// Package circuitbreaker wraps github.com/sony/gobreaker/v2's two-step breaker
// for transport-agnostic admission control, adding the operations surface the
// ginx middleware keeps: force controls and cumulative metrics. Write your HTTP
// middleware (or any caller) against Allow and the done it returns; for
// outbound calls prefer gobreaker's CircuitBreaker[T].Execute directly.
//
// Known upstream limits, kept honest on purpose: there is no injectable clock
// (tests use short Timeout values and sleep), and the half-open close threshold
// is MaxRequests itself (every probe must succeed to close).
package circuitbreaker

import (
	"sync/atomic"

	gobreaker "github.com/sony/gobreaker/v2"
)

// Settings is gobreaker's own; every field and hook applies unchanged.
type Settings = gobreaker.Settings

// Refusal reasons. Map ErrOpenState to 503 and ErrTooManyRequests to 429 in
// an HTTP middleware; both are also returned while ForceOpen is active.
var (
	ErrOpenState       = gobreaker.ErrOpenState
	ErrTooManyRequests = gobreaker.ErrTooManyRequests
)

// Admission result of a forced breaker, reported before the machine's own.
const (
	forceAuto int32 = iota
	forceOpen
	forceClose
)

// Breaker is a two-step circuit breaker: Allow decides, the returned done
// reports the outcome. Construct with New; do not copy. A done call belongs to
// the Allow that returned it; on a panic in the protected work the caller must
// still reach done with a non-nil error (defer is the usual shape).
type Breaker struct {
	cb       *gobreaker.TwoStepCircuitBreaker[struct{}]
	forced   atomic.Int32
	total    atomic.Uint64
	rejected atomic.Uint64
}

// New assembles a breaker from gobreaker settings.
func New(s Settings) *Breaker {
	return &Breaker{cb: gobreaker.NewTwoStepCircuitBreaker[struct{}](s)}
}

// Allow admits one request and returns its outcome reporter. A refused call
// returns ErrOpenState or ErrTooManyRequests and a nil done.
func (b *Breaker) Allow() (func(error), error) {
	b.total.Add(1)
	switch b.forced.Load() {
	case forceOpen:
		b.rejected.Add(1)
		return nil, ErrOpenState
	case forceClose:
		return func(error) {}, nil // bypassed: outcomes are not recorded
	}
	done, err := b.cb.Allow()
	if err != nil {
		b.rejected.Add(1)
		return nil, err
	}
	return done, nil
}

// State reports the effective state, forced modes included.
func (b *Breaker) State() gobreaker.State {
	switch b.forced.Load() {
	case forceOpen:
		return gobreaker.StateOpen
	case forceClose:
		return gobreaker.StateClosed
	}
	return b.cb.State()
}

// ForceOpen refuses every Allow with ErrOpenState until Reset.
func (b *Breaker) ForceOpen() { b.forced.Store(forceOpen) }

// ForceClose admits everything without recording outcomes until Reset.
func (b *Breaker) ForceClose() { b.forced.Store(forceClose) }

// Reset returns control to the breaker's own state machine.
// shortcut: gobreaker v2 has no count reset, so the underlying window keeps its history.
func (b *Breaker) Reset() { b.forced.Store(forceAuto) }

// Stats is a snapshot: the effective state, cumulative admission counts and
// gobreaker's current window counts.
type Stats struct {
	State    gobreaker.State
	Total    uint64 // Allow calls since construction, forced modes included
	Rejected uint64 // refusals, forced ones included
	Counts   gobreaker.Counts
}

// Stats snapshots the breaker. Counters are atomic, not transactional.
func (b *Breaker) Stats() Stats {
	return Stats{
		State:    b.State(),
		Total:    b.total.Load(),
		Rejected: b.rejected.Load(),
		Counts:   b.cb.Counts(),
	}
}
