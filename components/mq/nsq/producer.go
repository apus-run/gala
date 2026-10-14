package nsq

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/apus-run/gala/components/mq"
	nsqclient "github.com/nsqio/go-nsq"
)

const maxPendingSends = 64

type producerClient interface {
	PublishAsync(string, []byte, chan *nsqclient.ProducerTransaction, ...interface{}) error
	MultiPublishAsync(string, [][]byte, chan *nsqclient.ProducerTransaction, ...interface{}) error
	DeferredPublishAsync(string, time.Duration, []byte, chan *nsqclient.ProducerTransaction, ...interface{}) error
	Stop()
}

type Producer struct {
	clients   []producerClient
	mu        sync.Mutex
	closed    bool
	next      uint64
	slots     chan struct{}
	stop      chan struct{}
	pending   sync.WaitGroup
	closeOnce sync.Once
}

// Start is a lifecycle hook. go-nsq connects lazily on the first publish.
func (p *Producer) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	return nil
}

// Close stops SDK clients and waits for all accepted sends/callbacks to finish.
// Callbacks must not call Close on this producer or block indefinitely.
func (p *Producer) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		close(p.stop)
		p.mu.Unlock()
		for _, client := range p.clients {
			client.Stop()
		}
		p.pending.Wait()
	})
	return nil
}

func validateMessage(message *mq.Message) error {
	if message == nil || !nsqclient.IsValidTopicName(message.Topic) {
		return errors.New("nsq: invalid message or topic")
	}
	if message.Tag != "" || message.PartitionKey != "" || len(message.Properties) > 0 {
		return fmt.Errorf("nsq: tags, properties or partition key: %w", mq.ErrUnsupported)
	}
	if message.DeferDuration < 0 {
		return errors.New("nsq: negative delay")
	}
	return nil
}

func (p *Producer) Send(ctx context.Context, message *mq.Message) (mq.SendResponse, error) {
	return p.SendBatch(ctx, []*mq.Message{message})
}

func (p *Producer) SendBatch(ctx context.Context, messages []*mq.Message) (mq.SendResponse, error) {
	done, err := p.send(ctx, messages, nil)
	if err != nil {
		return mq.SendResponse{}, err
	}
	select {
	case err := <-done:
		return mq.SendResponse{}, err
	case <-ctx.Done():
		return mq.SendResponse{}, ctx.Err()
	}
}

func (p *Producer) SendAsync(ctx context.Context, callback mq.AsyncSendCallback, message *mq.Message) error {
	if callback == nil {
		return errors.New("nsq: callback is nil")
	}
	_, err := p.send(ctx, []*mq.Message{message}, callback)
	return err
}

func (p *Producer) send(ctx context.Context, messages []*mq.Message, callback mq.AsyncSendCallback) (<-chan error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, errors.New("nsq: empty batch")
	}
	bodies := make([][]byte, len(messages))
	for i, message := range messages {
		if err := validateMessage(message); err != nil {
			return nil, err
		}
		if message.Topic != messages[0].Topic {
			return nil, errors.New("nsq: batch topics differ")
		}
		if len(messages) > 1 && message.DeferDuration != 0 {
			return nil, fmt.Errorf("nsq: delayed batch: %w", mq.ErrUnsupported)
		}
		bodies[i] = bytes.Clone(message.Body)
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrClosed
	}
	select {
	case p.slots <- struct{}{}:
	default:
		p.mu.Unlock()
		return nil, ErrBackpressure
	}
	client := p.clients[p.next%uint64(len(p.clients))]
	p.next++
	p.pending.Add(1)
	p.mu.Unlock()

	ack := make(chan *nsqclient.ProducerTransaction, 1)
	var err error
	if err = ctx.Err(); err == nil {
		switch {
		case len(messages) > 1:
			err = client.MultiPublishAsync(messages[0].Topic, bodies, ack)
		case messages[0].DeferDuration > 0:
			// DPUB uses integer milliseconds; round up to avoid early delivery.
			delay := messages[0].DeferDuration
			if remainder := delay % time.Millisecond; remainder != 0 {
				// Saturate the conversion rather than overflowing time.Duration.
				if delay > time.Duration(1<<63-1)-(time.Millisecond-remainder) {
					err = errors.New("nsq: delay overflows millisecond precision")
					break
				}
				delay += time.Millisecond - remainder
			}
			err = client.DeferredPublishAsync(messages[0].Topic, delay, bodies[0], ack)
		default:
			err = client.PublishAsync(messages[0].Topic, bodies[0], ack)
		}
	}
	if err != nil {
		<-p.slots
		p.pending.Done()
		return nil, err
	}
	result := make(chan error, 1)
	go func() {
		defer p.pending.Done()
		defer func() { <-p.slots }()
		var err error
		acknowledged := false
		select {
		case transaction := <-ack:
			acknowledged = true
			err = transaction.Error
		case <-ctx.Done():
			err = ctx.Err()
		case <-p.stop:
			err = ErrClosed
		}
		result <- err
		if callback != nil {
			callback(ctx, mq.SendResponse{}, err)
		}
		if !acknowledged {
			// Cancellation doesn't cancel NSQ's command. Keep its slot until the
			// SDK completes or shutdown, bounding abandoned in-flight commands.
			select {
			case <-ack:
			case <-p.stop:
			}
		}
	}()
	return result, nil
}
