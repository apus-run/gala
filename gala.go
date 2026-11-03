// Package gala is the framework entry point: a Service that starts servers,
// registers the instance, and shuts everything down on signal | 框架入口：
// Service 启动各服务器、注册实例并在收到信号时优雅停止.
//
// # Notes | 说明
//
// New assembles the app from options; Run starts every server in an
// errgroup, registers the ServiceInstance with the configured registry once
// they are running, then blocks on the configured signals (SIGINT, SIGTERM,
// SIGQUIT by default) or until a server returns. Stop deregisters and cancels
// the context; a stopTimeout, when set, bounds how long servers get to drain.
// Endpoints default to what the servers expose as server.Endpointer. The
// package also carries typed context metadata (trace ID, user ID, color tag)
// and build Info helpers overridden via -ldflags.
// New 按选项组装应用；Run 在 errgroup 中启动所有服务器，就绪后向配置的 registry
// 注册 ServiceInstance，随后阻塞等待信号（默认 SIGINT、SIGTERM、SIGQUIT）或任一
// 服务器返回。Stop 注销实例并取消 context；设置 stopTimeout 时限定服务器的排水
// 时间。未显式配置时端点取各 server 的 Endpointer 实现。包内还提供类型化的
// context 元数据（trace ID、user ID、color tag）与可用 -ldflags 覆盖的构建 Info。
//
// # Features | 功能
//
// - Run / Stop lifecycle with before/after hooks | 带 before/after 钩子的 Run / Stop 生命周期
// - Servers managed in an errgroup | errgroup 托管的多个服务器
// - Optional registry with registration timeout | 可选服务注册与注册超时
// - WithTraceID / WithUserID / WithColorTag context helpers | context 元数据助手
// - Version Info with build metadata | 携带构建元数据的版本 Info
//
// # Example | 示例
//
//	app := gala.New(
//		gala.WithName("demo"),
//		gala.WithVersion("v1.0.0"),
//		gala.WithServers(httpServer),
//	)
//	if err := app.Run(); err != nil {
//		log.Fatal(err)
//	}
package gala

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"uuid"

	"golang.org/x/sync/errgroup"

	"github.com/apus-run/gala/registry"
	"github.com/apus-run/gala/server"
)

// Service is the application lifecycle manager | Service 是应用生命周期管理器.
type Service struct {
	opts   *Options
	ctx    context.Context
	cancel context.CancelFunc

	mux      sync.Mutex
	instance *registry.ServiceInstance
}

// New create an application lifecycle manager | New 创建应用生命周期管理器.
func New(opts ...Option) *Service {
	options := NewOptions(opts...)
	if options.id == "" {
		options.id = uuid.NewV4().String()
	}

	ctx, cancel := context.WithCancel(options.context)
	return &Service{
		ctx:    ctx,
		cancel: cancel,
		opts:   options,
	}
}

// ID returns app instance id | ID 返回应用实例 id.
func (s *Service) ID() string { return s.opts.id }

// Name returns service name | Name 返回服务名.
func (s *Service) Name() string { return s.opts.name }

// Version returns app version | Version 返回应用版本.
func (s *Service) Version() string { return s.opts.version }

// Metadata returns service metadata | Metadata 返回服务元数据.
func (s *Service) Metadata() map[string]string { return s.opts.metadata }

// Endpoint returns endpoints | Endpoint 返回端点列表.
func (s *Service) Endpoint() []string {
	if s.instance != nil {
		return s.instance.Endpoints
	}
	return nil
}

// Run executes all OnStart hooks registered with the application's Lifecycle | Run 执行应用 Lifecycle 中注册的所有 OnStart 钩子.
func (s *Service) Run() error {
	instance, err := s.registryService()
	if err != nil {
		return err
	}
	s.mux.Lock()
	s.instance = instance
	s.mux.Unlock()
	c := ServiceContextKey.NewContext(s.ctx, s)
	eg, ctx := errgroup.WithContext(c)
	wg := sync.WaitGroup{}

	for _, fn := range s.opts.beforeStart {
		if err = fn(c); err != nil {
			return err
		}
	}

	octx := ServiceContextKey.NewContext(s.opts.context, s)
	for _, srv := range s.opts.servers {
		eg.Go(func() error {
			<-ctx.Done() // wait for stop signal | 等待停止信号
			stopCtx := octx
			if s.opts.stopTimeout > 0 {
				var cancel context.CancelFunc
				stopCtx, cancel = context.WithTimeout(stopCtx, s.opts.stopTimeout)
				defer cancel()
			}
			return srv.Stop(stopCtx)
		})
		wg.Add(1)
		eg.Go(func() error {
			wg.Done() // here is to ensure core start has begun running before register, so defer is not needed | 这里确保核心启动先于注册开始运行，因此无需 defer
			return srv.Start(octx)
		})
	}
	wg.Wait()
	if s.opts.registry != nil {
		rctx, rcancel := context.WithTimeout(ctx, s.opts.registryTimeout)
		defer rcancel()
		if err = s.opts.registry.Register(rctx, instance); err != nil {
			return err
		}
	}
	for _, fn := range s.opts.afterStart {
		if err = fn(c); err != nil {
			return err
		}
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, s.opts.signals...)
	eg.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
			return s.Stop()
		}
	})
	if err = eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	err = nil
	for _, fn := range s.opts.afterStop {
		err = fn(c)
	}
	return err
}

// Stop gracefully stops the application | Stop 优雅地停止应用.
func (s *Service) Stop() error {
	var err error
	sctx := ServiceContextKey.NewContext(s.ctx, s)
	for _, fn := range s.opts.beforeStop {
		if err = fn(sctx); err != nil {
			return err
		}
	}

	s.mux.Lock()
	instance := s.instance
	s.mux.Unlock()
	if s.opts.registry != nil && instance != nil {
		ctx, cancel := context.WithTimeout(ServiceContextKey.NewContext(s.ctx, s), s.opts.registryTimeout)
		defer cancel()
		if err = s.opts.registry.Deregister(ctx, instance); err != nil {
			return err
		}
	}
	if s.cancel != nil {
		s.cancel()
	}
	return err
}

func (s *Service) registryService() (*registry.ServiceInstance, error) {
	endpoints := make([]string, 0, len(s.opts.endpoints))
	for _, e := range s.opts.endpoints {
		endpoints = append(endpoints, e.String())
	}
	if len(endpoints) == 0 {
		for _, srv := range s.opts.servers {
			if r, ok := srv.(server.Endpointer); ok {
				e, err := r.Endpoint()
				if err != nil {
					return nil, err
				}
				endpoints = append(endpoints, e.String())
			}
		}
	}
	return &registry.ServiceInstance{
		ID:        s.opts.id,
		Name:      s.opts.name,
		Version:   s.opts.version,
		Metadata:  s.opts.metadata,
		Endpoints: endpoints,
	}, nil
}
