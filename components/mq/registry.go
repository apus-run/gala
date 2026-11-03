package mq

import (
	"context"
)

// ConsumerRegistry batch-starts a set of consumers and owns their lifecycle
// | ConsumerRegistry 批量启动一组消费者并持有其生命周期.
//
//go:generate mockgen -destination=mocks/registry.go -package=mocks . ConsumerRegistry,ConsumerWorker
type ConsumerRegistry interface {
	// Register adds workers for the next startup attempt.
	// Register before StartAll.
	// Register 登记下一轮启动的 worker；须在 StartAll 之前调用。
	Register(worker []ConsumerWorker) ConsumerRegistry
	// StartAll starts a snapshot of workers, rolling back on failure or
	// cancellation. Once successful, further calls are no-ops. ctx controls
	// startup only.
	// StartAll 启动已登记 worker 的一次快照，失败或取消时回滚；成功后再次调用是
	// no-op。ctx 只控制启动阶段。
	StartAll(ctx context.Context) error
	// Close closes consumers in reverse order, once. The registry cannot
	// restart. Concurrent StartAll/Close calls may return registry.ErrBusy.
	// Close 逆序关闭消费者且只关一次；注册器不能重启。并发的 StartAll/Close
	// 调用可能返回 registry.ErrBusy。
	Close() error
}

// ConsumerWorker supplies the config and handler for one consumer | ConsumerWorker 提供单个消费者的配置与处理器.
type ConsumerWorker interface {
	// ConsumerCfg returns the config used to create the consumer | ConsumerCfg 返回创建消费者所用的配置.
	ConsumerCfg(ctx context.Context) (*ConsumerConfig, error)
	ConsumerHandler
}
