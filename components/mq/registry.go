package mq

import (
	"context"
)

//go:generate mockgen -destination=mocks/registry.go -package=mocks . ConsumerRegistry,ConsumerWorker

type ConsumerRegistry interface {
	Register(worker []ConsumerWorker) ConsumerRegistry
	StartAll(ctx context.Context) error
}

type ConsumerWorker interface {
	ConsumerCfg(ctx context.Context) (*ConsumerConfig, error)
	ConsumerHandler
}
