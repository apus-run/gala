package authz

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrPolicyStale means policy freshness has expired. Callers must deny access.
var ErrPolicyStale = errors.New("authz: policy is stale")

func (a *Authorizer) policyFresh() error {
	if a.maxPolicyAge > 0 {
		loaded := a.lastLoad.Load()
		if loaded == nil || time.Since(*loaded) >= a.maxPolicyAge {
			return ErrPolicyStale
		}
	}
	return nil
}

// SyncPolicy reloads locally and independently notifies peers after a committed
// write. Both errors are preserved. Success is NOT an acknowledgement from peers.
// Update runs outside the reload lock, so synchronous self callbacks are safe.
func (a *Authorizer) SyncPolicy() error {
	loadErr := a.LoadPolicy()
	var notifyErr error
	if a.watcher != nil {
		if err := a.watcher.Update(); err != nil {
			notifyErr = fmt.Errorf("authz: notify policy: %w", err)
		}
	}
	return errors.Join(loadErr, notifyErr)
}

// RunPolicyReload reloads immediately and periodically until ctx is cancelled.
// The ticker starts before the first load; this is not a sleep-after-load loop.
// An overdue tick can start the next load immediately. The interval must be less
// than maxPolicyAge when enabled; individual load duration is not checked here.
// It blocks: the application owns the goroutine and must wait for it on shutdown.
// Missed watcher events are repaired without broadcasting. Callback errors are
// reported and retried on the next tick. Cancellation cannot interrupt a running
// Adapter.LoadPolicy; configure storage timeouts. onError must return promptly.
func (a *Authorizer) RunPolicyReload(ctx context.Context, interval time.Duration, onError func(error)) error {
	if ctx == nil || interval <= 0 || onError == nil {
		return errors.New("authz: reload loop requires context, positive interval and error handler")
	}
	if a.maxPolicyAge > 0 && interval >= a.maxPolicyAge {
		return errors.New("authz: reload interval must be shorter than max policy age")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.LoadPolicy(); err != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
