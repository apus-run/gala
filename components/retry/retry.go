// Package retry retries a function until it succeeds, the strategy is
// exhausted, or ctx is done | 重试：函数成功、策略耗尽或 ctx 结束前反复执行.
//
// # Notes | 说明
//
// Retry drives a strategy.Strategy from the strategy subpackage: it reports
// each error back to the strategy (adaptive strategies adjust from that),
// waits for the interval the strategy returns, and stops when the strategy
// reports no more retries — the last error is wrapped and returned. NewRetry
// builds only the "fixed" and "exponential" configs; other strategies must be
// constructed from the strategy package directly.
// Retry 驱动 strategy 子包的 Strategy：把每次错误回报给策略（自适应策略据此调整），
// 按策略返回的间隔等待，策略不再允许重试时停止，并把最后一个错误包装返回。
// NewRetry 只构造 "fixed" 与 "exponential" 两种配置，其它策略需直接用 strategy 包
// 构造。
//
// # Features | 功能
//
// - Retry loop driven by any strategy.Strategy | 由任意 strategy.Strategy 驱动的重试循环
// - NewRetry builds fixed-interval or exponential-backoff strategies | NewRetry 构造固定间隔或指数退避策略
// - Honors ctx cancellation between attempts | 两次尝试之间响应 ctx 取消
//
// # Example | 示例
//
//	s, err := retry.NewRetry(retry.Config{
//		Type:          "fixed",
//		FixedInterval: &retry.FixedIntervalConfig{MaxRetries: 3, Interval: time.Second},
//	})
//	if err != nil {
//		return err
//	}
//	return retry.Retry(ctx, s, callRPC)
package retry

import (
	"context"
	"fmt"
	"time"

	"github.com/apus-run/gala/components/retry/strategy"
)

// Config selects the retry strategy to build | Config 选择要构造的重试策略.
type Config struct {
	// Strategy type: "fixed" or "exponential" | 策略类型："fixed" 或 "exponential".
	Type string `json:"type"`
	// Fixed-interval config, required for type "fixed" | 固定间隔配置，type 为 fixed 时必填.
	FixedInterval *FixedIntervalConfig `json:"fixedInterval"`
	// Exponential-backoff config, required for type "exponential" | 指数退避配置，type 为 exponential 时必填.
	ExponentialBackoff *ExponentialBackoffConfig `json:"exponentialBackoff"`
}

// ExponentialBackoffConfig configures exponential backoff | ExponentialBackoffConfig 配置指数退避.
type ExponentialBackoffConfig struct {
	// Initial retry interval in milliseconds | 初始重试间隔，单位毫秒.
	InitialInterval time.Duration `json:"initialInterval"`
	// Max retry interval | 最大重试间隔.
	MaxInterval time.Duration `json:"maxInterval"`
	// Max retry count | 最大重试次数.
	MaxRetries int32 `json:"maxRetries"`
}

// FixedIntervalConfig configures fixed-interval retries | FixedIntervalConfig 配置固定间隔重试.
type FixedIntervalConfig struct {
	// Max retry count; 0 or negative means unlimited | 最大重试次数，0 或负数表示无限重试.
	MaxRetries int32 `json:"maxRetries"`
	// Interval between retries | 重试间隔.
	Interval time.Duration `json:"interval"`
}

// NewRetry builds a strategy from cfg, erroring on invalid config | NewRetry 按 cfg 构造策略，配置无效时报错.
func NewRetry(cfg Config) (strategy.Strategy, error) {
	// select the strategy by the cfg fields
	// 按 cfg 中的字段选择策略
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

// Retry repeats bizFunc until it succeeds, the strategy is exhausted, or ctx
// is done. It returns when retries hit the maximum (the exhaustion error is
// returned), when ctx is canceled or times out, or when bizFunc returns no
// error. Any error from bizFunc triggers another attempt.
// Retry 反复执行 bizFunc，直到成功、策略耗尽或 ctx 结束。返回条件：重试达到最大次数
// （返回重试耗尽的错误）；ctx 被取消或者超时；bizFunc 没有返回 error。只要 bizFunc
// 返回 error，就会尝试重试。
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
		// exit immediately on success
		// 成功则直接退出
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
			// timed out or canceled, return immediately
			// 超时或者被取消了，直接返回
			return ctx.Err()
		case <-timer.C:
		}
	}
}
