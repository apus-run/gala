package strategy

import (
	"math"
	"math/bits"
	"sync/atomic"
	"time"
)

var _ Strategy = (*AdaptiveTimeoutRetryStrategy)(nil)

// AdaptiveTimeoutRetryStrategy stops retrying adaptively when the recent
// failure count in a sliding bit window exceeds a threshold.
// AdaptiveTimeoutRetryStrategy 用比特滑动窗口统计近期失败数，超过阈值时自适应停止重试.
type AdaptiveTimeoutRetryStrategy struct {
	strategy   Strategy        // base retry strategy | 基础重试策略
	threshold  int             // timeout ratio threshold, in bits | 超时比率阈值（单位：比特数量）
	ringBuffer []atomic.Uint64 // bit ring storing recent failures as a sliding window | 比特环（滑动窗口存储超时信息）
	reqCount   atomic.Uint64   // total request count | 请求数量
	bufferLen  int             // sliding window length in uint64 words | 滑动窗口长度
	bitCnt     uint64          // total bit count | 比特位总数
}

// NextWithRetries implements Strategy | NextWithRetries 实现 Strategy 接口.
func (s *AdaptiveTimeoutRetryStrategy) NextWithRetries(retries int32) (time.Duration, bool) {
	return s.nextWithRetries(retries)
}

func (s *AdaptiveTimeoutRetryStrategy) nextWithRetries(retries int32) (time.Duration, bool) {
	failCount := s.getFailed()

	// adapt the threshold: each extra retry tightens it, so the strategy gets stricter
	// 动态调整阈值：随着重试次数增加，降低阈值，使策略更严格
	dynamicThreshold := s.threshold
	if retries > 0 {
		// each retry lowers the threshold by 10%, floored at 50% of it
		// 每增加一次重试，阈值降低10%，但不低于阈值的50%
		dynamicThreshold = int(float64(s.threshold) * math.Max(0.5, 1.0-0.1*float64(retries)))
	}

	if failCount >= dynamicThreshold {
		return 0, false
	}
	return s.strategy.NextWithRetries(retries)
}

// Next returns the next interval if the failure count is below the threshold | Next 失败数低于阈值时返回下一次间隔.
func (s *AdaptiveTimeoutRetryStrategy) Next() (time.Duration, bool) {
	failCount := s.getFailed()
	if failCount >= s.threshold {
		return 0, false
	}
	return s.strategy.Next()
}

// Report records the attempt result in the sliding window | Report 把本次尝试结果记入滑动窗口.
func (s *AdaptiveTimeoutRetryStrategy) Report(err error) Strategy {
	if err == nil {
		s.markSuccess()
	} else {
		s.markFail()
	}
	return s
}

func (s *AdaptiveTimeoutRetryStrategy) markSuccess() {
	count := s.reqCount.Add(1) - 1
	count = count % s.bitCnt
	// modulo and division by 2^x can be replaced with bit operations
	// 对 2^x 进行取模或整除运算时可以用位运算代替除法和取模
	// count / 64 becomes count >> 6, which is faster
	// count / 64 可以转换成 count >> 6，位运算会更高效。
	idx := count >> 6
	// count % 64 becomes count & 63
	// count % 64 可以转换成 count & 63
	bitPos := count & 63
	mask := uint64(1) << bitPos
	for {
		old := s.ringBuffer[idx].Load()
		if s.ringBuffer[idx].CompareAndSwap(old, old&^mask) {
			return
		}
	}
}

func (s *AdaptiveTimeoutRetryStrategy) markFail() {
	count := s.reqCount.Add(1) - 1
	count = count % s.bitCnt
	idx := count >> 6
	bitPos := count & 63
	// (uint64(1)<<bitPos) sets the target bit to 1
	// (uint64(1)<<bitPos) 将目标位设置为 1
	mask := uint64(1) << bitPos
	for {
		old := s.ringBuffer[idx].Load()
		if s.ringBuffer[idx].CompareAndSwap(old, old|mask) {
			return
		}
	}
}

func (s *AdaptiveTimeoutRetryStrategy) getFailed() int {
	var failCount int
	for i := range len(s.ringBuffer) {
		v := s.ringBuffer[i].Load()
		failCount += bits.OnesCount64(v)
	}
	return failCount
}

// NewAdaptiveTimeoutRetryStrategy wraps strategy with the adaptive timeout
// check | NewAdaptiveTimeoutRetryStrategy 用自适应超时检查包装传入策略.
func NewAdaptiveTimeoutRetryStrategy(strategy Strategy, bufferLen, threshold int) *AdaptiveTimeoutRetryStrategy {
	return &AdaptiveTimeoutRetryStrategy{
		strategy:   strategy,
		threshold:  threshold,
		bufferLen:  bufferLen,
		ringBuffer: make([]atomic.Uint64, bufferLen),
		bitCnt:     uint64(64) * uint64(bufferLen),
	}
}
