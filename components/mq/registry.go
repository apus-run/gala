package mq

import (
	"context"
)

//go:generate mockgen -destination=mocks/registry.go -package=mocks . ConsumerRegistry,ConsumerWorker

type ConsumerRegistry interface {
	// Register adds workers for the next startup attempt. Register before StartAll.
	Register(worker []ConsumerWorker) ConsumerRegistry
	// StartAll starts a snapshot of workers, rolling back on failure or cancellation.
	// Once successful, further calls are no-ops. ctx controls startup only.
	StartAll(ctx context.Context) error
	// Close closes consumers in reverse order, once. The registry cannot restart.
	// Concurrent StartAll/Close calls may return registry.ErrBusy.
	Close() error
}

type ConsumerWorker interface {
	ConsumerCfg(ctx context.Context) (*ConsumerConfig, error)
	ConsumerHandler
}
