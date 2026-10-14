package registry

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/apus-run/gala/components/mq"
	"github.com/apus-run/gala/pkg/lang/safego"
)

var (
	// ErrBusy means another StartAll or Close call is still in progress.
	ErrBusy = errors.New("mq: registry lifecycle operation in progress")
	// ErrClosed means this registry has been permanently closed.
	ErrClosed = errors.New("mq: registry closed")
)

type defaultConsumerRegistry struct {
	factory mq.Factory
	mu      sync.Mutex
	workers []mq.ConsumerWorker

	// TryLock prevents concurrent lifecycle calls and reentrant callbacks from blocking.
	lifecycle sync.Mutex
	consumers []mq.Consumer
	started   bool
	closed    bool
	closeErr  error
}

func NewConsumerRegistry(factory mq.Factory) mq.ConsumerRegistry {
	return &defaultConsumerRegistry{factory: factory}
}

func (d *defaultConsumerRegistry) Register(workers []mq.ConsumerWorker) mq.ConsumerRegistry {
	d.mu.Lock()
	d.workers = append(d.workers, workers...)
	d.mu.Unlock()
	return d
}

func (d *defaultConsumerRegistry) StartAll(ctx context.Context) (err error) {
	if !d.lifecycle.TryLock() {
		return ErrBusy
	}
	defer d.lifecycle.Unlock()
	if d.closed {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.started {
		return nil
	}
	if d.factory == nil {
		return errors.New("mq: factory is nil")
	}
	d.mu.Lock()
	workers := append([]mq.ConsumerWorker(nil), d.workers...)
	d.mu.Unlock()

	var consumers []mq.Consumer
	defer func() {
		if err != nil {
			err = errors.Join(err, closeConsumers(consumers))
		}
	}()
	for i, worker := range workers {
		if err := ctx.Err(); err != nil {
			return err
		}
		if worker == nil {
			return fmt.Errorf("mq: worker %d is nil", i)
		}
		cfg, err := worker.ConsumerCfg(ctx)
		if err != nil {
			return fmt.Errorf("mq: worker %d config: %w", i, err)
		}
		if cfg == nil {
			return fmt.Errorf("mq: worker %d config is nil", i)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		c, err := d.factory.NewConsumer(*cfg)
		if err != nil {
			return fmt.Errorf("mq: worker %d create: %w", i, err)
		}
		if c == nil {
			return fmt.Errorf("mq: worker %d consumer is nil", i)
		}
		// A failed Start may still own resources; include it in rollback.
		consumers = append(consumers, c)
		c.RegisterHandler(newSafeConsumerWrapper(worker))
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.Start(); err != nil {
			return fmt.Errorf("mq: worker %d start: %w", i, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	d.consumers = consumers
	d.started = true
	return nil
}

func (d *defaultConsumerRegistry) Close() error {
	if !d.lifecycle.TryLock() {
		return ErrBusy
	}
	defer d.lifecycle.Unlock()
	if !d.closed {
		d.closed = true
		d.closeErr = closeConsumers(d.consumers)
		d.consumers = nil
	}
	return d.closeErr
}

func closeConsumers(consumers []mq.Consumer) error {
	var errs []error
	for i := len(consumers) - 1; i >= 0; i-- {
		if err := consumers[i].Close(); err != nil {
			errs = append(errs, fmt.Errorf("mq: consumer %d close: %w", i, err))
		}
	}
	return errors.Join(errs...)
}

type safeConsumerHandlerDecorator struct {
	handler mq.ConsumerHandler
}

func (s *safeConsumerHandlerDecorator) HandleMessage(ctx context.Context, msg *mq.MessageExt) (err error) {
	defer safego.Recover(ctx, &err)
	return s.handler.HandleMessage(ctx, msg)
}

func newSafeConsumerWrapper(h mq.ConsumerHandler) mq.ConsumerHandler {
	return &safeConsumerHandlerDecorator{handler: h}
}
