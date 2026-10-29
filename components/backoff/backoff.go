package backoff

import (
	"context"
	"time"

	"github.com/cenk/backoff"
)

const (
	defaultRetryInterval = time.Millisecond * 100

	one     = time.Second * 1
	three   = time.Second * 3
	five    = time.Second * 5
	ten     = time.Second * 10
	OneMin  = time.Minute
	FiveMin = time.Minute * 5
	OneHour = time.Hour
)

func RetryOneSecond(ctx context.Context, fn func() error) error {
	return RetryWithElapsedTime(ctx, one, fn)
}

func RetryThreeSeconds(ctx context.Context, fn func() error) error {
	return RetryWithElapsedTime(ctx, three, fn)
}

func RetryFiveSeconds(ctx context.Context, fn func() error) error {
	return RetryWithElapsedTime(ctx, five, fn)
}

func RetryOneMin(ctx context.Context, fn func() error) error {
	return RetryWithElapsedTime(ctx, OneMin, fn)
}

func RetryFiveMin(ctx context.Context, fn func() error) error {
	return RetryWithElapsedTime(ctx, FiveMin, fn)
}

func RetryOneHour(ctx context.Context, fn func() error) error {
	return RetryWithElapsedTime(ctx, OneHour, fn)
}

func RetryTenSeconds(ctx context.Context, fn func() error) error {
	return RetryWithElapsedTime(ctx, ten, fn)
}

func RetryWithElapsedTime(ctx context.Context, maxElapsedTime time.Duration, fn func() error) error {
	policy := backoff.NewExponentialBackOff()
	policy.InitialInterval = defaultRetryInterval
	policy.MaxElapsedTime = maxElapsedTime

	return backoffFn(ctx, fn, policy)
}

func backoffFn(ctx context.Context, fn func() error, policy *backoff.ExponentialBackOff) error {
	ctxWithCancel, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	backoffCtx := backoff.WithContext(policy, ctxWithCancel)

	return backoff.Retry(fn, backoffCtx)
}

// RetryWithMaxTimes 最多重试 max 次（首次调用之外），重试之间不等待。
// max<=0 表示不重试；ctx 取消后立即返回 ctx.Err()。
func RetryWithMaxTimes(ctx context.Context, max int, fn func() error) error {
	if max <= 0 {
		// cenk/backoff 的 WithMaxRetries(0) 表示不限次数，不能交给它处理
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn()
	}
	policy := backoff.WithContext(backoff.WithMaxRetries(&backoff.ZeroBackOff{}, uint64(max)), ctx)
	return backoff.Retry(fn, policy)
}
