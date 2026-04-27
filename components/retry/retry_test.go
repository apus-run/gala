package retry

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/apus-run/gala/components/retry/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trackingStrategy struct {
	nextCalled            int
	nextWithRetriesCalled []int32
	reported              []error
}

func (s *trackingStrategy) Next() (time.Duration, bool) {
	s.nextCalled++
	return time.Nanosecond, true
}

func (s *trackingStrategy) NextWithRetries(retries int32) (time.Duration, bool) {
	s.nextWithRetriesCalled = append(s.nextWithRetriesCalled, retries)
	return time.Nanosecond, true
}

func (s *trackingStrategy) Report(err error) strategy.Strategy {
	s.reported = append(s.reported, err)
	return s
}

func TestNewRetry(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "fixed config missing",
			cfg: Config{
				Type: "fixed",
			},
			wantErr: "fixed 重试配置不能为空",
		},
		{
			name: "exponential config missing",
			cfg: Config{
				Type: "exponential",
			},
			wantErr: "exponential 重试配置不能为空",
		},
		{
			name: "unknown retry type",
			cfg: Config{
				Type: "unknown",
			},
			wantErr: "未知重试类型: unknown",
		},
	}

	for _, tt := range testCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewRetry(tt.cfg)
			require.Error(t, err)
			assert.Nil(t, got)
			assert.Equal(t, tt.wantErr, err.Error())
		})
	}
}

func TestRetry_ReportsResultsAndUsesExplicitRetryCount(t *testing.T) {
	t.Parallel()

	s := &trackingStrategy{}
	attempts := 0

	err := Retry(context.Background(), s, func() error {
		attempts++
		if attempts == 1 {
			return errors.New("boom")
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 2, attempts)
	assert.Equal(t, []int32{1}, s.nextWithRetriesCalled)
	assert.Equal(t, 0, s.nextCalled)
	require.Len(t, s.reported, 2)
	assert.EqualError(t, s.reported[0], "boom")
	assert.NoError(t, s.reported[1])
}

func TestRetry_StopsBeforeFirstAttemptWhenContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := 0
	err := Retry(ctx, &trackingStrategy{}, func() error {
		called++
		return nil
	})

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, called)
}

func TestRetry_AdaptiveStrategyCanStopFurtherRetries(t *testing.T) {
	t.Parallel()

	base := &trackingStrategy{}
	adaptive := strategy.NewAdaptiveTimeoutRetryStrategy(base, 1, 3)
	attempts := 0

	err := Retry(context.Background(), adaptive, func() error {
		attempts++
		return errors.New("still failing")
	})

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "重试耗尽"))
	assert.Equal(t, 2, attempts)
	assert.Equal(t, []int32{1}, base.nextWithRetriesCalled)
}
