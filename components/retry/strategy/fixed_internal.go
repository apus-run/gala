package strategy

import (
	"sync/atomic"
	"time"
)

var _ Strategy = (*FixedIntervalRetryStrategy)(nil)

// FixedIntervalRetryStrategy retries at a fixed interval | FixedIntervalRetryStrategy 以固定间隔重试.
type FixedIntervalRetryStrategy struct {
	maxRetries int32         // max retries; 0 or negative means unlimited | 最大重试次数，0 或负数表示无限重试
	interval   time.Duration // interval between retries | 重试间隔时间
	retries    atomic.Int32  // current retry count | 当前重试次数
}

// NewFixedIntervalRetryStrategy creates a fixed-interval strategy | NewFixedIntervalRetryStrategy 创建固定间隔重试策略.
func NewFixedIntervalRetryStrategy(interval time.Duration, maxRetries int32) *FixedIntervalRetryStrategy {
	return &FixedIntervalRetryStrategy{
		maxRetries: maxRetries,
		interval:   interval,
	}
}

// NextWithRetries computes the next interval for the given retry count | NextWithRetries 按给定重试次数计算下一次间隔.
func (s *FixedIntervalRetryStrategy) NextWithRetries(retries int32) (time.Duration, bool) {
	return s.nextWithRetries(retries)
}

func (s *FixedIntervalRetryStrategy) nextWithRetries(retries int32) (time.Duration, bool) {
	if s.maxRetries <= 0 || retries <= s.maxRetries {
		return s.interval, true
	}
	return 0, false
}

// Next advances the retry counter and returns the next interval | Next 递增重试计数并返回下一次间隔.
func (s *FixedIntervalRetryStrategy) Next() (time.Duration, bool) {
	retries := s.retries.Add(1)
	return s.nextWithRetries(retries)
}

// Report ignores the error and keeps this strategy | Report 忽略错误，保持原策略.
func (s *FixedIntervalRetryStrategy) Report(_ error) Strategy {
	return s
}
