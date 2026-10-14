package kafka

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apus-run/gala/components/mq"
	"github.com/twmb/franz-go/pkg/kgo"
)

var ErrClosed = errors.New("kafka: client closed")

type producerClient interface {
	ProduceSync(context.Context, ...*kgo.Record) kgo.ProduceResults
	TryProduce(context.Context, *kgo.Record, func(*kgo.Record, error))
	Flush(context.Context) error
	Close()
}

type Producer struct {
	client    producerClient
	closed    atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

// Start is a lifecycle compatibility hook; franz-go connects lazily on use.
func (p *Producer) Start() error {
	if p.closed.Load() {
		return ErrClosed
	}
	return nil
}

// Close flushes pending sends for up to ten seconds, then closes the client.
// Callbacks must not call Close or block on this producer.
func (p *Producer) Close() error {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		p.closeErr = p.client.Flush(ctx)
		p.client.Close()
	})
	return p.closeErr
}

func (p *Producer) Send(ctx context.Context, message *mq.Message) (mq.SendResponse, error) {
	return p.SendBatch(ctx, []*mq.Message{message})
}

// SendBatch validates all messages before sending. Kafka batches are not atomic;
// on success the response identifies the last input message.
func (p *Producer) SendBatch(ctx context.Context, messages []*mq.Message) (mq.SendResponse, error) {
	if err := ctx.Err(); err != nil {
		return mq.SendResponse{}, err
	}
	if p.closed.Load() {
		return mq.SendResponse{}, ErrClosed
	}
	if len(messages) == 0 {
		return mq.SendResponse{}, errors.New("kafka: empty batch")
	}
	records := make([]*kgo.Record, len(messages))
	for i, message := range messages {
		record, err := toRecord(message)
		if err != nil {
			return mq.SendResponse{}, err
		}
		records[i] = record
	}
	results := p.client.ProduceSync(ctx, records...)
	if err := results.FirstErr(); err != nil {
		return mq.SendResponse{}, err
	}
	return response(records[len(records)-1], nil), nil
}

// SendAsync reports validation errors directly and delivery errors via callback.
// A nil return means the request was handed to franz-go, not that Kafka stored it.
func (p *Producer) SendAsync(ctx context.Context, callback mq.AsyncSendCallback, message *mq.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.closed.Load() {
		return ErrClosed
	}
	if callback == nil {
		return errors.New("kafka: callback is nil")
	}
	record, err := toRecord(message)
	if err != nil {
		return err
	}
	p.client.TryProduce(ctx, record, func(record *kgo.Record, err error) { callback(ctx, response(record, err), err) })
	return nil
}
