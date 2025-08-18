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
		return strategy.NewFixedIntervalRetryStrategy(cfg.FixedInterval.Interval, cfg.FixedInterval.MaxRetries), nil
	case "exponential":
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
	var ticker *time.Ticker
	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()
	for {
		err := bizFunc()
		// 直接退出
		if err == nil {
			return nil
		}
		duration, ok := s.Next()
		if !ok {
			return fmt.Errorf("ekit: 重试耗尽, 原因: %w", err)
		}
		if ticker == nil {
			ticker = time.NewTicker(duration)
		} else {
			ticker.Reset(duration)
		}
		select {
		case <-ctx.Done():
			// 超时或者被取消了，直接返回
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
