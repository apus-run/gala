package circuitbreaker

import (
	"errors"
	"testing"
	"time"

	gobreaker "github.com/sony/gobreaker/v2"
)

func failN(t *testing.T, b *Breaker, n int) {
	t.Helper()
	for range n {
		done, err := b.Allow()
		if err != nil {
			t.Fatalf("admission refused before tripping: %v", err)
		}
		done(errors.New("boom"))
	}
}

func TestBreakerTripsAndRecovers(t *testing.T) {
	b := New(gobreaker.Settings{
		MaxRequests: 1,
		Interval:    0,
		Timeout:     20 * time.Millisecond,
		ReadyToTrip: func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= 2 },
	})
	failN(t, b, 2)
	if _, err := b.Allow(); !errors.Is(err, ErrOpenState) {
		t.Fatalf("want ErrOpenState, got %v", err)
	}
	// No injectable clock upstream: wait out the timeout, then the single
	// allowed probe must succeed to close the circuit.
	time.Sleep(30 * time.Millisecond)
	done, err := b.Allow()
	if err != nil {
		t.Fatalf("half-open probe refused: %v", err)
	}
	done(nil)
	if b.State() != gobreaker.StateClosed {
		t.Fatalf("want closed after successful probe, got %v", b.State())
	}
}

func TestBreakerHalfOpenSaturates(t *testing.T) {
	b := New(gobreaker.Settings{
		MaxRequests: 1,
		Timeout:     20 * time.Millisecond,
		ReadyToTrip: func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= 1 },
	})
	failN(t, b, 1)
	time.Sleep(30 * time.Millisecond)
	probe, err := b.Allow()
	if err != nil {
		t.Fatalf("probe refused: %v", err)
	}
	defer probe(nil)
	if _, err := b.Allow(); !errors.Is(err, ErrTooManyRequests) {
		t.Fatalf("want ErrTooManyRequests, got %v", err)
	}
}

func TestBreakerForces(t *testing.T) {
	b := New(gobreaker.Settings{
		MaxRequests: 1,
		Timeout:     time.Hour,
		ReadyToTrip: func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= 1 },
	})
	b.ForceOpen()
	if _, err := b.Allow(); !errors.Is(err, ErrOpenState) {
		t.Fatalf("forced open admitted: %v", err)
	}
	if s := b.Stats(); s.State != gobreaker.StateOpen || s.Total != 1 || s.Rejected != 1 {
		t.Fatalf("forced open not reported: %+v", s)
	}
	b.ForceClose()
	done, err := b.Allow()
	if err != nil {
		t.Fatalf("forced close refused: %v", err)
	}
	done(errors.New("ignored"))
	if got := b.Stats().Counts.Requests; got != 0 {
		t.Fatalf("bypassed call reached the machine's counts: %d", got)
	}
	b.Reset()
	failN(t, b, 1) // the machine's own state still trips on its own terms
	if _, err := b.Allow(); !errors.Is(err, ErrOpenState) {
		t.Fatalf("want ErrOpenState after Reset, got %v", err)
	}
}

func TestBreakerStatsCountsAdmissions(t *testing.T) {
	b := New(gobreaker.Settings{Timeout: time.Hour})
	for range 3 {
		done, err := b.Allow()
		if err != nil {
			t.Fatal(err)
		}
		done(nil)
	}
	if s := b.Stats(); s.Total != 3 || s.Rejected != 0 || s.Counts.Requests != 3 {
		t.Fatalf("unexpected stats: %+v", s)
	}
}
