// Package strategy provides retry policies that decide the interval between
// attempts and when to stop | strategy 提供决定重试间隔与停止时机的策略.
package strategy

import (
	"time"
)

// Strategy computes the wait interval between retries | Strategy 计算重试之间的等待间隔.
type Strategy interface {
	// NextWithRetries computes the next interval for the given retry count; the
	// second return is false when no more retries are needed.
	// NextWithRetries 根据当前重试次数计算下一次重试间隔，如果不需要继续重试，那么第二参数返回 false.
	NextWithRetries(retries int32) (time.Duration, bool)
	// Next returns the interval until the next retry; the second return is
	// false when no more retries are needed.
	// Next 返回下一次重试的间隔，如果不需要继续重试，那么第二参数返回 false.
	Next() (time.Duration, bool)
	// Report feeds the attempt result back, optionally returning a replacement
	// strategy | Report 上报本次尝试结果，可返回替代策略.
	Report(err error) Strategy
}
