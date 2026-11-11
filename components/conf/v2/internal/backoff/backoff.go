// Package backoff provides a per-loop, capped retry delay with equal jitter.
package backoff

import (
	"context"
	"math/rand/v2"
	"time"
)

// Delay keeps the exponential ceiling between attempts. Zero starts at min.
type Delay time.Duration

func (d *Delay) Next(min, max time.Duration) time.Duration {
	n := time.Duration(*d)
	switch {
	case n == 0:
		n = min
	case n >= max-n:
		n = max
	default:
		n *= 2
	}
	*d = Delay(n)
	half := n / 2
	return n - time.Duration(rand.Int64N(int64(half)+1))
}

// Wait is the interruptible retry policy for source reconnections.
func (d *Delay) Wait(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d.Next(time.Second, 30*time.Second)):
		return ctx.Err() == nil
	}
}
