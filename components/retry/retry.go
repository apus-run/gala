package retry

import (
	"context"
	"fmt"
	"time"

	"github.com/apus-run/gala/components/retry/strategy"
)

type Config struct {
	Type               string                    `json:"type"`
	FixedInterval      *FixedIntervalConfig      `json:"fixedInterval"`
	ExponentialBackoff *ExponentialBackoffConfig `json:"exponentialBackoff"`
}

type ExponentialBackoffConfig struct {
	// 初始重试间隔 单位ms
	InitialInterval time.Duration `json:"initialInterval"`
	MaxInterval     time.Duration `json:"maxInterval"`
	// 最大重试次数
	MaxRetries int32 `json:"maxRetries"`
}

type FixedIntervalConfig struct {
	MaxRetries int32         `json:"maxRetries"`
	Interval   time.Duration `json:"interval"`
}

// NewRetry 当配置不对的时候报错
func NewRetry(cfg Config) (strategy.Strategy, error) {
	// 根据 config 中的字段来检测
	switch cfg.Type {
	case "fixed":
		if cfg.FixedInterval == nil {
			return nil, fmt.Errorf("fixed 重试配置不能为空")
		}
		return strategy.NewFixedIntervalRetryStrategy(cfg.FixedInterval.Interval, cfg.FixedInterval.MaxRetries), nil
	case "exponential":
		if cfg.ExponentialBackoff == nil {
			return nil, fmt.Errorf("exponential 重试配置不能为空")
		}
		return strategy.NewExponentialBackoffRetryStrategy(cfg.ExponentialBackoff.InitialInterval, cfg.ExponentialBackoff.MaxInterval, cfg.ExponentialBackoff.MaxRetries), nil
	default:
		return nil, fmt.Errorf("未知重试类型: %s", cfg.Type)
	}
}

// Retry 会在以下条件满足的情况下返回：
// 1. 重试达到了最大次数，而后返回重试耗尽的错误
// 2. ctx 被取消或者超时
// 3. bizFunc 没有返回 error
// 而只要 bizFunc 返回 error，就会尝试重试
func Retry(ctx context.Context,
	s strategy.Strategy,
	bizFunc func() error,
) error {
	var (
		timer   *time.Timer
		retries int32
	)
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := bizFunc()
		if nextStrategy := s.Report(err); nextStrategy != nil {
			s = nextStrategy
		}
		// 直接退出
		if err == nil {
			return nil
		}

		retries++
		duration, ok := s.NextWithRetries(retries)
		if !ok {
			return fmt.Errorf("ekit: 重试耗尽, 原因: %w", err)
		}

		if timer == nil {
			timer = time.NewTimer(duration)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(duration)
		}

		select {
		case <-ctx.Done():
			// 超时或者被取消了，直接返回
			return ctx.Err()
		case <-timer.C:
		}
	}
}
