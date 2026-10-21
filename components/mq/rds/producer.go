package rds

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/mq"
)

type Producer struct {
	client  redis.Cmdable
	timeout time.Duration
	life    context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	closed  bool
	active  sync.WaitGroup
	slots   chan struct{}
}

var _ mq.Producer = (*Producer)(nil)

// Start is an idempotent lifecycle hook; the client connects on demand.
func (p *Producer) Start() error { return p.checkOpen() }

func (p *Producer) checkOpen() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	return nil
}

// Close cancels and waits for accepted calls/callbacks, without closing Redis.
// Callbacks must return promptly and must not call Close on this producer.
func (p *Producer) Close() error {
	p.mu.Lock()
	p.closed = true
	p.cancel()
	p.mu.Unlock()
	p.active.Wait()
	return nil
}
func (p *Producer) begin(async bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	if async {
		select {
		case p.slots <- struct{}{}:
		default:
			return ErrBackpressure
		}
	}
	p.active.Add(1)
	return nil
}
func (p *Producer) scope(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(parent, p.timeout)
	stop := context.AfterFunc(p.life, cancel)
	return ctx, func() { stop(); cancel() }
}
func (p *Producer) publish(ctx context.Context, m prepared) (mq.SendResponse, error) {
	if err := ctx.Err(); err != nil {
		return mq.SendResponse{}, err
	}
	var err error
	if m.delay > 0 {
		err = p.client.Eval(ctx, schedule, []string{DelayKey(m.topic)}, m.data, m.delay).Err()
	} else {
		err = p.client.XAdd(ctx, &redis.XAddArgs{Stream: StreamKey(m.topic), Values: map[string]any{"data": m.data}}).Err()
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return mq.SendResponse{}, ctxErr
	}
	// Socket deadlines can fire before the context timer is scheduled.
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return mq.SendResponse{}, context.DeadlineExceeded
	}
	if err != nil {
		return mq.SendResponse{}, err
	}
	return mq.SendResponse{MessageID: m.id}, nil
}
func (p *Producer) Send(ctx context.Context, m *mq.Message) (mq.SendResponse, error) {
	return p.SendBatch(ctx, []*mq.Message{m})
}

// SendBatch validates and snapshots the whole input before publishing. The batch
// is not transactional: on error the response identifies the last known success.
func (p *Producer) SendBatch(ctx context.Context, messages []*mq.Message) (mq.SendResponse, error) {
	if err := p.checkOpen(); err != nil {
		return mq.SendResponse{}, err
	}
	ctx, cancel := p.scope(ctx)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return mq.SendResponse{}, err
	}
	if len(messages) == 0 {
		return mq.SendResponse{}, errors.New("redisstream: empty batch")
	}
	batch := make([]prepared, len(messages))
	for i, m := range messages {
		var err error
		batch[i], err = prepare(m)
		if err != nil {
			return mq.SendResponse{}, err
		}
	}
	if err := p.begin(false); err != nil {
		return mq.SendResponse{}, err
	}
	defer p.active.Done()
	var last mq.SendResponse
	for _, m := range batch {
		response, err := p.publish(ctx, m)
		if err != nil {
			return last, err
		}
		last = response
	}
	return last, nil
}
func (p *Producer) SendAsync(ctx context.Context, callback mq.AsyncSendCallback, m *mq.Message) error {
	if err := p.checkOpen(); err != nil {
		return err
	}
	if callback == nil {
		return errors.New("redisstream: nil callback")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	opCtx, cancel := p.scope(ctx)
	message, err := prepare(m)
	if err != nil {
		cancel()
		return err
	}
	if err := p.begin(true); err != nil {
		cancel()
		return err
	}
	go func() {
		defer p.active.Done()
		defer func() { <-p.slots }()
		defer cancel()
		response, err := p.publish(opCtx, message)
		defer func() {
			if value := recover(); value != nil {
				slog.ErrorContext(ctx, "redisstream callback panic", "panic", value)
			}
		}()
		callback(ctx, response, err)
	}()
	return nil
}
