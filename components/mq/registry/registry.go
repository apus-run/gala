// Package registry batch-starts a group of mq consumers with rollback
// | registry 批量启动一组 mq 消费者并支持失败回滚.
//
// # Notes | 说明
//
// It wraps one mq.Factory: every registered ConsumerWorker contributes its
// ConsumerConfig and handler. StartAll starts a snapshot of the registered
// workers and rolls back everything it already started on failure or
// cancellation; Close closes consumers in reverse order exactly once. The
// registry cannot restart, concurrent lifecycle calls fail with ErrBusy, and
// handler panics are recovered so they surface as handler errors instead of
// crashing the consumer.
// 它包装一个 mq.Factory：每个登记的 ConsumerWorker 提供自己的
// ConsumerConfig 与处理器。StartAll 启动已登记 worker 的一份快照，失败或
// 取消时回滚已启动的部分；Close 逆序关闭消费者且只关一次。注册器不能重启，
// 并发生命周期调用返回 ErrBusy，处理器里的 panic 会被恢复为错误而不是拖垮消费者。
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
	// ErrBusy means another StartAll or Close call is still in progress
	// | ErrBusy 表示另一个 StartAll 或 Close 正在进行。
	ErrBusy = errors.New("mq: registry lifecycle operation in progress")
	// ErrClosed means this registry has been permanently closed
	// | ErrClosed 表示该注册器已被永久关闭。
	ErrClosed = errors.New("mq: registry closed")
)

type defaultConsumerRegistry struct {
	factory mq.Factory
	mu      sync.Mutex
	workers []mq.ConsumerWorker

	// TryLock prevents concurrent lifecycle calls and reentrant callbacks from
	// blocking | TryLock 用非阻塞锁防止并发生命周期调用与重入回调造成阻塞。
	lifecycle sync.Mutex
	consumers []mq.Consumer
	started   bool
	closed    bool
	closeErr  error
}

// NewConsumerRegistry creates a registry over factory | NewConsumerRegistry 基于 factory 创建注册器。
func NewConsumerRegistry(factory mq.Factory) mq.ConsumerRegistry {
	return &defaultConsumerRegistry{factory: factory}
}

// Register implements mq.ConsumerRegistry, appending workers | 实现 mq.ConsumerRegistry，追加 worker.
func (d *defaultConsumerRegistry) Register(workers []mq.ConsumerWorker) mq.ConsumerRegistry {
	d.mu.Lock()
	d.workers = append(d.workers, workers...)
	d.mu.Unlock()
	return d
}

// StartAll implements mq.ConsumerRegistry, starting a snapshot of the workers with rollback on failure | 实现 mq.ConsumerRegistry，启动 worker 快照，失败时回滚.
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
		// Start 失败的消费者可能仍持有资源；把它纳入回滚。
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

// Close implements mq.ConsumerRegistry, closing consumers in reverse order exactly once | 实现 mq.ConsumerRegistry，逆序关闭消费者且只关一次.
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

// HandleMessage implements mq.ConsumerHandler, recovering handler panics into errors | 实现 mq.ConsumerHandler，把处理器 panic 恢复为错误.
func (s *safeConsumerHandlerDecorator) HandleMessage(ctx context.Context, msg *mq.MessageExt) (err error) {
	defer safego.Recover(ctx, &err)
	return s.handler.HandleMessage(ctx, msg)
}

func newSafeConsumerWrapper(h mq.ConsumerHandler) mq.ConsumerHandler {
	return &safeConsumerHandlerDecorator{handler: h}
}
