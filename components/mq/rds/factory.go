// Package rds implements mq using Redis Streams and consumer groups.
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
	ErrClosed       = errors.New("redisstream: closed")
	ErrBackpressure = errors.New("redisstream: too many asynchronous sends")
)

type options struct {
	maxRetries   int
	pollInterval time.Duration
	claimIdle    time.Duration
}
type Option func(*options)

// WithMaxRetries sets retries after the initial delivery; zero sends the first
// failed delivery to the group's dead-letter stream. Crashed deliveries count.
func WithMaxRetries(n int) Option { return func(o *options) { o.maxRetries = n } }

// WithPollInterval sets the delay scheduler interval and blocking read duration.
func WithPollInterval(d time.Duration) Option { return func(o *options) { o.pollInterval = d } }

// WithClaimIdle sets how long an unacknowledged delivery may be idle before
// another worker takes it. It must exceed the consumer's handler timeout.
func WithClaimIdle(d time.Duration) Option { return func(o *options) { o.claimIdle = d } }

// Factory borrows client. Close adapters before closing the application-owned
// client. Addr, compression, producer group and publish retry settings in the
// common configs are unsupported; connection settings belong to client.
type Factory struct {
	client *redis.Client
	opts   options
}

var _ mq.Factory = (*Factory)(nil)

func NewFactory(client *redis.Client, opts ...Option) (*Factory, error) {
	if client == nil {
		return nil, errors.New("redisstream: Redis client is required")
	}
	o := options{maxRetries: 3, pollInterval: time.Second, claimIdle: time.Minute}
	for _, option := range opts {
		if option == nil {
			return nil, errors.New("redisstream: nil option")
		}
		option(&o)
	}
	if o.maxRetries < 0 || o.pollInterval < time.Millisecond || o.claimIdle < time.Millisecond {
		return nil, errors.New("redisstream: invalid retry count or polling/claim duration")
	}
	// Redis represents blocking and idle thresholds in whole milliseconds.
	o.pollInterval = o.pollInterval.Truncate(time.Millisecond)
	o.claimIdle = o.claimIdle.Truncate(time.Millisecond)
	return &Factory{client: client, opts: o}, nil
}
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
