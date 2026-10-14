package nsq

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/apus-run/gala/components/mq"
	nsqclient "github.com/nsqio/go-nsq"
)

var (
	ErrClosed       = errors.New("nsq: client closed")
	ErrBusy         = errors.New("nsq: lifecycle operation in progress")
	ErrBackpressure = errors.New("nsq: too many pending sends")
)

type Factory struct{}

func NewFactory() mq.Factory { return &Factory{} }

func validateAddresses(addrs []string) error {
	if len(addrs) == 0 {
		return errors.New("nsq: addr is empty")
	}
	for _, addr := range addrs {
		host, port, err := net.SplitHostPort(addr)
		if err != nil || host == "" || port == "" {
			return fmt.Errorf("nsq: invalid nsqd TCP address %q", addr)
		}
	}
	return nil
}

func (f *Factory) NewProducer(cfg mq.ProducerConfig) (mq.Producer, error) {
	if err := validateAddresses(cfg.Addr); err != nil {
		return nil, err
	}
	if cfg.RetryTimes < 0 || cfg.ProduceTimeout < 0 {
		return nil, errors.New("nsq: negative retry count or timeout")
	}
	if cfg.RetryTimes != 0 || cfg.Compression != mq.CompressionNone {
		return nil, fmt.Errorf("nsq: publish retries or compression: %w", mq.ErrUnsupported)
	}
	config := nsqclient.NewConfig()
	if cfg.ProduceTimeout > 0 {
		config.DialTimeout, config.ReadTimeout, config.WriteTimeout = cfg.ProduceTimeout, cfg.ProduceTimeout, cfg.ProduceTimeout
		config.HeartbeatInterval = min(config.HeartbeatInterval, cfg.ProduceTimeout/2)
		// nsqd requires heartbeats >= 1s; -1ms disables them on the wire.
		// Short producer I/O deadlines then close idle sockets; go-nsq reconnects.
		if config.HeartbeatInterval < time.Second {
			config.HeartbeatInterval = -time.Millisecond
		}
	}
	if cfg.ProducerGroup != nil {
		config.ClientID = *cfg.ProducerGroup
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	p := &Producer{slots: make(chan struct{}, maxPendingSends), stop: make(chan struct{})}
	for _, addr := range cfg.Addr {
		client, err := nsqclient.NewProducer(addr, config)
		if err != nil {
			p.Close()
			return nil, err
		}
		p.clients = append(p.clients, client)
	}
	return p, nil
}

func consumerConfig(cfg mq.ConsumerConfig) (*nsqclient.Config, error) {
	if err := validateAddresses(cfg.Addr); err != nil {
		return nil, err
	}
	if !nsqclient.IsValidTopicName(cfg.Topic) || !nsqclient.IsValidChannelName(cfg.ConsumerGroup) {
		return nil, errors.New("nsq: invalid topic or consumer group (channel)")
	}
	if cfg.Orderly || cfg.TagExpression != "" {
		return nil, fmt.Errorf("nsq: orderly consumption or tag filtering: %w", mq.ErrUnsupported)
	}
	if cfg.ConsumeTimeout < 0 || cfg.ConsumeGoroutineNums < 0 {
		return nil, errors.New("nsq: negative timeout or concurrency")
	}
	config := nsqclient.NewConfig()
	// SDK's default MaxAttempts=5 would FIN a repeatedly failing message.
	config.MaxAttempts = 0
	config.MaxInFlight = max(1, cfg.ConsumeGoroutineNums)
	config.MsgTimeout = cfg.ConsumeTimeout
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return config, nil
}

func (f *Factory) NewConsumer(cfg mq.ConsumerConfig) (mq.Consumer, error) {
	config, err := consumerConfig(cfg)
	if err != nil {
		return nil, err
	}
	client, err := nsqclient.NewConsumer(cfg.Topic, cfg.ConsumerGroup, config)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Consumer{client: client, stop: client.StopChan, cancel: cancel,
		addrs: append([]string(nil), cfg.Addr...), topic: cfg.Topic, timeout: cfg.ConsumeTimeout}
	// Register before exposing c: StopChan only closes after an SDK handler exits,
	// even if Close is called before Start or Start rejects a missing handler.
	client.AddConcurrentHandlers(nsqclient.HandlerFunc(func(message *nsqclient.Message) error {
		return c.handle(ctx, message)
	}), config.MaxInFlight)
	return c, nil
}
