// Package expiry checks the application-clock deadlines stored by SQL and MongoDB.
package expiry

import (
	"fmt"
	"time"

	"github.com/apus-run/gala/components/dlock"
)

// Deadline returns a deadline at the millisecond precision used by both stores.
func Deadline(now time.Time, ttl time.Duration) time.Time {
	return now.Add(ttl).Truncate(time.Millisecond)
}

// Check rejects a successful write whose lease expired while awaiting the reply.
func Check(name string, deadline time.Time) error {
	if !time.Now().Before(deadline) {
		return fmt.Errorf("%w: lease for %q expired during write", dlock.ErrLockNotHeld, name)
	}
	return nil
}
