// Package backoff retries failing functions with policies built on
// cenk/backoff | 基于 cenk/backoff 重试失败的函数。
//
// # Notes | 说明
//
// Two fixed policies, no configuration surface. RetryWithElapsedTime retries
// with exponential backoff (100ms initial interval) until the total elapsed
// time reaches the limit; presets range from one second to one hour.
// RetryWithMaxTimes retries up to max extra attempts with no wait between
// them; max<=0 runs the function once. Cancelling the context stops the wait
// immediately and the last operation error is returned; wrap an error with
// backoff.Permanent to stop retrying early.
// 只有两种固定策略、没有配置面。RetryWithElapsedTime 以指数退避（初始间隔
// 100ms）重试，直到累计耗时到达上限；预设从 1 秒到 1 小时。RetryWithMaxTimes
// 最多额外重试 max 次、中间不等待；max<=0 只执行一次。ctx 取消会立即结束等待
// 并返回最后一次操作的错误；用 backoff.Permanent 包装错误可提前终止重试。
//
// # Example | 示例
//
//	err := RetryWithElapsedTime(ctx, 5*time.Second, func() error {
//		return fetch(ctx)
//	})
package backoff

import (
	"context"
	"time"

	"github.com/cenk/backoff"
)

const (
	defaultRetryInterval = time.Millisecond * 100

	one   = time.Second * 1
	three = time.Second * 3
	five  = time.Second * 5
	ten   = time.Second * 10

	// OneMin is one minute | OneMin 表示 1 分钟.
	OneMin = time.Minute
	// FiveMin is five minutes | FiveMin 表示 5 分钟.
	FiveMin = time.Minute * 5
	// OneHour is one hour | OneHour 表示 1 小时.
	OneHour = time.Hour
)

// RetryOneSecond retries with exponential backoff for up to one second
// | RetryOneSecond 以指数退避重试，累计耗时上限 1 秒.
func RetryOneSecond(ctx context.Context, fn func() error) error {
	return RetryWithElapsedTime(ctx, one, fn)
}

// RetryThreeSeconds retries with exponential backoff for up to three seconds
// | RetryThreeSeconds 以指数退避重试，累计耗时上限 3 秒.
func RetryThreeSeconds(ctx context.Context, fn func() error) error {
	return RetryWithElapsedTime(ctx, three, fn)
}

// RetryFiveSeconds retries with exponential backoff for up to five seconds
// | RetryFiveSeconds 以指数退避重试，累计耗时上限 5 秒.
func RetryFiveSeconds(ctx context.Context, fn func() error) error {
	return RetryWithElapsedTime(ctx, five, fn)
}

// RetryOneMin retries with exponential backoff for up to one minute
// | RetryOneMin 以指数退避重试，累计耗时上限 1 分钟.
func RetryOneMin(ctx context.Context, fn func() error) error {
	return RetryWithElapsedTime(ctx, OneMin, fn)
}

// RetryFiveMin retries with exponential backoff for up to five minutes
// | RetryFiveMin 以指数退避重试，累计耗时上限 5 分钟.
func RetryFiveMin(ctx context.Context, fn func() error) error {
	return RetryWithElapsedTime(ctx, FiveMin, fn)
}

// RetryOneHour retries with exponential backoff for up to one hour
// | RetryOneHour 以指数退避重试，累计耗时上限 1 小时.
func RetryOneHour(ctx context.Context, fn func() error) error {
	return RetryWithElapsedTime(ctx, OneHour, fn)
}

// RetryTenSeconds retries with exponential backoff for up to ten seconds
// | RetryTenSeconds 以指数退避重试，累计耗时上限 10 秒.
func RetryTenSeconds(ctx context.Context, fn func() error) error {
	return RetryWithElapsedTime(ctx, ten, fn)
}

// RetryWithElapsedTime retries fn with exponential backoff until the total
// elapsed time reaches maxElapsedTime | RetryWithElapsedTime 以指数退避重试 fn，
// 直到累计耗时到达 maxElapsedTime.
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

// RetryWithMaxTimes retries fn at most max extra times with no wait between
// attempts | RetryWithMaxTimes 最多重试 max 次（首次调用之外），重试之间不等待。
//
// max<=0 means no retry; a cancelled ctx returns ctx.Err() immediately.
// max<=0 表示不重试；ctx 取消后立即返回 ctx.Err()。
func RetryWithMaxTimes(ctx context.Context, max int, fn func() error) error {
	if max <= 0 {
		// cenk/backoff's WithMaxRetries(0) means unlimited, so max<=0 cannot be
		// delegated to it
		// cenk/backoff 的 WithMaxRetries(0) 表示不限次数，不能交给它处理
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn()
	}
	policy := backoff.WithContext(backoff.WithMaxRetries(&backoff.ZeroBackOff{}, uint64(max)), ctx)
	return backoff.Retry(fn, policy)
}
