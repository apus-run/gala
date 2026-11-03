// Package rds implements the mq interfaces on Redis Streams with consumer
// groups | 基于 Redis Streams 与消费者组实现 mq 接口.
//
// # Notes | 说明
//
// The package borrows an application-owned *redis.Client: connection,
// pooling and credentials belong to the client, and the producer additionally
// requires ContextTimeoutEnabled=true with socket deadlines configured.
// Redis >= 6.2 is required (redelivery is driven by XAUTOCLAIM). Delayed
// messages live in a per-topic sorted set promoted by a Lua scheduler; failed
// deliveries are retried via XAUTOCLAIM by idle time and, once retries are
// exhausted, moved to the group's dead-letter stream.
// 本包借用应用自持的 *redis.Client：连接、连接池与凭据都归客户端管理，
// 生产者还要求 ContextTimeoutEnabled=true 且配置了 socket 超时。
// 依赖 Redis >= 6.2（重投由 XAUTOCLAIM 驱动）。延迟消息存于按主题的
// sorted set，由 Lua 调度器搬运；失败投递按空闲时间经 XAUTOCLAIM 重试，
// 重试耗尽后转入该消费者组的死信 stream。
//
// Unsupported by this backend (each fails with mq.ErrUnsupported): Addr and
// compression in producer config, publish retries, producer groups, partition
// ordering (PartitionKey), orderly consumption and tag filtering. ACK does
// not delete stream entries — retention is the application's job, see Trim.
// 本后端不支持（均返回 mq.ErrUnsupported）：生产者配置中的 Addr 与压缩、
// 发布重试、生产者组、分区顺序（PartitionKey）、顺序消费与标签过滤。
// ACK 不会删除 stream 条目——保留策略由应用负责，见 Trim。
//
// # Features | 功能
//
// - Sync, batch and callback-based async sending | 同步、批量与回调式异步发送
// - Delayed delivery via sorted-set scheduling | 基于 sorted set 调度的延迟投递
// - Per-group dead-letter stream and Trim of commonly acked history | 按组的死信 stream 与按全体组确认裁剪历史
package rds

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/mq"
)

var (
	// ErrClosed is returned by operations on a closed producer
	// | ErrClosed 由已关闭生产者上的操作返回。
	ErrClosed = errors.New("redisstream: closed")
	// ErrBackpressure is returned when too many async sends are still pending
	// | ErrBackpressure 表示未完成的异步发送过多。
	ErrBackpressure = errors.New("redisstream: too many asynchronous sends")
)

// Options tunes consumer retry and polling behavior | Options 调整消费者的重试与轮询行为。
type Options struct {
	maxRetries   int
	pollInterval time.Duration
	claimIdle    time.Duration
}

// Option configures Options | Option 配置 Options。
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配为 Option。
type OptionFunc func(*Options)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// WithMaxRetries sets retries after the initial delivery; zero sends the first
// failed delivery to the group's dead-letter stream. Crashed deliveries count.
// WithMaxRetries 设置首次投递之后的重试次数；为 0 时首次失败即转入该组的
// 死信 stream。崩溃掉的投递也计入次数。
func WithMaxRetries(n int) Option { return OptionFunc(func(o *Options) { o.maxRetries = n }) }

// WithPollInterval sets the delay scheduler interval and blocking read duration.
// WithPollInterval 设置延迟调度器的间隔与阻塞读取的时长。
func WithPollInterval(d time.Duration) Option {
	return OptionFunc(func(o *Options) { o.pollInterval = d })
}

// WithClaimIdle sets how long an unacknowledged delivery may be idle before
// another worker takes it. It must exceed the consumer's handler timeout.
// WithClaimIdle 设置一条未确认投递空闲多久后可被其他 worker 接管。
// 该值必须大于消费者的处理器超时。
func WithClaimIdle(d time.Duration) Option { return OptionFunc(func(o *Options) { o.claimIdle = d }) }

// Factory borrows client. Close adapters before closing the application-owned
// client. Addr, compression, producer group and publish retry settings in the
// common configs are unsupported; connection settings belong to client.
// Factory 借用传入的 client。请在关闭应用自持的 client 之前先关闭适配器。
// 公共配置中的 Addr、压缩、生产者组与发布重试均不支持；连接设置归 client。
type Factory struct {
	client *redis.Client
	opts   Options
}

var _ mq.Factory = (*Factory)(nil)

// DefaultOptions returns the package defaults | DefaultOptions 返回包默认配置。
func DefaultOptions() Options {
	return Options{maxRetries: 3, pollInterval: time.Second, claimIdle: time.Minute}
}

// Validate checks the retry count and durations. It does not change the
// Options.
// Validate 校验重试次数与时长，不修改 Options 本身。
func (o *Options) Validate() error {
	if o.maxRetries < 0 || o.pollInterval < time.Millisecond || o.claimIdle < time.Millisecond {
		return errors.New("redisstream: invalid retry count or polling/claim duration")
	}
	return nil
}

// NewFactory builds a Factory over the given client | NewFactory 基于给定 client 创建 Factory。
func NewFactory(client *redis.Client, opts ...Option) (*Factory, error) {
	if client == nil {
		return nil, errors.New("redisstream: Redis client is required")
	}
	o := DefaultOptions()
	for _, option := range opts {
		if option == nil {
			return nil, errors.New("redisstream: nil option")
		}
		option.apply(&o)
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	// Redis represents blocking and idle thresholds in whole milliseconds.
	// Redis 以整毫秒表示阻塞与空闲阈值。
	o.pollInterval = o.pollInterval.Truncate(time.Millisecond)
	o.claimIdle = o.claimIdle.Truncate(time.Millisecond)
	return &Factory{client: client, opts: o}, nil
}

// NewProducer creates a Producer for cfg; a zero ProduceTimeout defaults to
// five seconds.
// NewProducer 依据 cfg 创建 Producer；ProduceTimeout 为 0 时默认 5 秒。
func (f *Factory) NewProducer(cfg mq.ProducerConfig) (mq.Producer, error) {
	if cfg.ProduceTimeout < 0 || cfg.RetryTimes < 0 {
		return nil, errors.New("redisstream: negative timeout or retries")
	}
	if len(cfg.Addr) > 0 || cfg.Compression != mq.CompressionNone || cfg.RetryTimes != 0 || cfg.ProducerGroup != nil {
		return nil, fmt.Errorf("redisstream: producer config: %w", mq.ErrUnsupported)
	}
	clientOpts := f.client.Options()
	if !clientOpts.ContextTimeoutEnabled {
		return nil, errors.New("rds: producer requires Redis ContextTimeoutEnabled=true")
	}
	if clientOpts.ReadTimeout < 0 || clientOpts.WriteTimeout < 0 {
		return nil, errors.New("rds: producer requires Redis socket deadline setting enabled")
	}
	timeout := cfg.ProduceTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Producer{client: f.client, timeout: timeout, life: ctx, cancel: cancel, slots: make(chan struct{}, 64)}, nil
}

// NewConsumer creates a Consumer for cfg; a zero ConsumeTimeout defaults to
// thirty seconds and the configured claim idle must exceed it.
// NewConsumer 依据 cfg 创建 Consumer；ConsumeTimeout 为 0 时默认 30 秒，
// 且配置的 claim idle 必须大于该超时。
func (f *Factory) NewConsumer(cfg mq.ConsumerConfig) (mq.Consumer, error) {
	if strings.TrimSpace(cfg.Topic) == "" || strings.TrimSpace(cfg.ConsumerGroup) == "" || cfg.ConsumeTimeout < 0 || cfg.ConsumeGoroutineNums < 0 {
		return nil, errors.New("redisstream: invalid consumer configuration")
	}
	if len(cfg.Addr) > 0 || cfg.Orderly || cfg.TagExpression != "" {
		return nil, fmt.Errorf("redisstream: consumer config: %w", mq.ErrUnsupported)
	}
	timeout := cfg.ConsumeTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if f.opts.claimIdle <= timeout {
		return nil, errors.New("redisstream: claim idle must exceed consume timeout")
	}
	return &Consumer{client: f.client, opts: f.opts, topic: cfg.Topic, group: cfg.ConsumerGroup, timeout: timeout, workers: max(1, cfg.ConsumeGoroutineNums)}, nil
}
