package strategy

import (
	"math"
	"sync/atomic"
	"time"
)

var _ Strategy = (*ExponentialBackoffRetryStrategy)(nil)

// ExponentialBackoffRetryStrategy retries with exponentially growing intervals | ExponentialBackoffRetryStrategy 按指数递增间隔重试.
type ExponentialBackoffRetryStrategy struct {
	// initial retry interval | 初始重试间隔.
	initialInterval time.Duration
	// max retry interval | 最大重试间隔.
	maxInterval time.Duration
	// max retry count | 最大重试次数.
	maxRetries int32
	// current retry count | 当前重试次数.
	retries atomic.Int32
	// whether the max interval has been reached | 是否已经达到最大重试间隔.
	maxIntervalReached atomic.Bool
}

// NewExponentialBackoffRetryStrategy creates an exponential-backoff strategy | NewExponentialBackoffRetryStrategy 创建指数退避重试策略.
func NewExponentialBackoffRetryStrategy(initialInterval, maxInterval time.Duration, maxRetries int32) *ExponentialBackoffRetryStrategy {
	return &ExponentialBackoffRetryStrategy{
		initialInterval: initialInterval,
		maxInterval:     maxInterval,
		maxRetries:      maxRetries,
	}
}

// Report ignores the error and keeps this strategy | Report 忽略错误，保持原策略.
func (s *ExponentialBackoffRetryStrategy) Report(_ error) Strategy {
	return s
}

// NextWithRetries computes the next interval for the given retry count | NextWithRetries 按给定重试次数计算下一次间隔.
func (s *ExponentialBackoffRetryStrategy) NextWithRetries(retries int32) (time.Duration, bool) {
	return s.nextWithRetries(retries)
}

func (s *ExponentialBackoffRetryStrategy) nextWithRetries(retries int32) (time.Duration, bool) {
	if s.maxRetries <= 0 || retries <= s.maxRetries {
		if s.maxIntervalReached.Load() {
			return s.maxInterval, true
		}
		const two = 2
		interval := s.initialInterval * time.Duration(math.Pow(two, float64(retries-1)))
		// overflow, or the computed interval exceeds the max interval
		// 溢出或当前重试间隔大于最大重试间隔
		if interval <= 0 || interval > s.maxInterval {
			s.maxIntervalReached.Store(true)
			return s.maxInterval, true
		}
		return interval, true
	}
	return 0, false
}

// Next advances the retry counter and returns the next interval | Next 递增重试计数并返回下一次间隔.
func (s *ExponentialBackoffRetryStrategy) Next() (time.Duration, bool) {
	retries := s.retries.Add(1)
	return s.nextWithRetries(retries)
}
