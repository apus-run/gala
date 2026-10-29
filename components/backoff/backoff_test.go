package backoff

import (
	"context"
	"errors"
	"testing"
)

var errFail = errors.New("fail")

func TestRetryWithMaxTimesNonPositiveDoesNotRetry(t *testing.T) {
	for _, max := range []int{0, -1} {
		calls := 0
		err := RetryWithMaxTimes(context.Background(), max, func() error { calls++; return errFail })
		if !errors.Is(err, errFail) || calls != 1 {
			t.Fatalf("max=%d: calls=%d err=%v, want 1 call and errFail", max, calls, err)
		}
	}
}

func TestRetryWithMaxTimesRetriesExactly(t *testing.T) {
	calls := 0
	_ = RetryWithMaxTimes(context.Background(), 3, func() error { calls++; return errFail })
	if calls != 4 {
		t.Fatalf("calls = %d, want 4 (1 + 3 retries)", calls)
	}
}

func TestRetryWithMaxTimesStopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := RetryWithMaxTimes(ctx, 1000, func() error { calls++; return errFail })
	if err == nil || calls > 1 {
		t.Fatalf("calls=%d err=%v, want at most 1 call and an error after cancel", calls, err)
	}
}
