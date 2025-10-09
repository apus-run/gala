package gala

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/apus-run/gala/registry"
	"github.com/apus-run/gala/server"
)

type Service struct {
	opts   *Options
	ctx    context.Context
	cancel context.CancelFunc

	mux      sync.Mutex
	instance *registry.ServiceInstance
}

// New create an application lifecycle manager.
func New(opts ...Option) *Service {
	options := Apply(opts...)
	if id, err := uuid.NewUUID(); err == nil {
		options.id = id.String()
	}

	ctx, cancel := context.WithCancel(options.context)
	return &Service{
		ctx:    ctx,
		cancel: cancel,
		opts:   options,
	}
}

// ID returns app instance id.
func (s *Service) ID() string { return s.opts.id }

// Name returns service name.
func (s *Service) Name() string { return s.opts.name }

// Version returns app version.
func (s *Service) Version() string { return s.opts.version }

// Metadata returns service metadata.
func (s *Service) Metadata() map[string]string { return s.opts.metadata }

// Endpoint returns endpoints.
func (s *Service) Endpoint() []string {
	if s.instance != nil {
		return s.instance.Endpoints
	}
	return nil
}

// Run executes all OnStart hooks registered with the application's Lifecycle.
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
		server := srv
		eg.Go(func() error {
			<-ctx.Done() // wait for stop signal
			stopCtx := octx
			if s.opts.stopTimeout > 0 {
				var cancel context.CancelFunc
				stopCtx, cancel = context.WithTimeout(stopCtx, s.opts.stopTimeout)
				defer cancel()
			}
			return server.Stop(stopCtx)
		})
		wg.Add(1)
		eg.Go(func() error {
			wg.Done() // here is to ensure core start has begun running before register, so defer is not needed
			return server.Start(octx)
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

// Stop gracefully stops the application.
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
