// Package consul provides service registration and discovery through the Consul HTTP API.
package consul

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/apus-run/gala/registry"
)

const closeTimeout = 5 * time.Second

var _ registry.Registry = (*Registry)(nil)

// Registry owns registered instances and their TTL keepalives.
// The caller owns any HTTP client supplied through WithHTTPClient.
type Registry struct {
	c         *client
	opts      options
	life      *lifetime
	mu        sync.Mutex // serializes registration changes
	entries   map[string]*entry
	closeOnce sync.Once
	closeErr  error
}

type entry struct {
	svcID   string
	checkID string
	payload *agentService
	cancel  context.CancelFunc
	done    chan struct{}
}

func (e *entry) halt() { e.cancel(); <-e.done }

func NewRegistry(opts ...Option) (*Registry, error) {
	o, err := newOptions(opts)
	if err != nil {
		return nil, err
	}
	return &Registry{c: newClient(o), opts: o,
		life: newLifetime(context.Background()), entries: make(map[string]*entry)}, nil
}

// Register upserts an instance and starts its TTL keepalive. A generated ID
// is written back before the request so retries use the same service key.
func (r *Registry) Register(ctx context.Context, instance *registry.ServiceInstance) error {
	if err := instance.Validate(); err != nil {
		return err
	}
	ctx, done, err := r.life.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if instance.ID == "" {
		instance.ID = registry.NewID()
	}
	payload, err := toAgentService(instance.Clone(), &r.opts)
	if err != nil {
		return err
	}
	// Halt the old writer before replacing its payload.
	if old := r.entries[payload.ID]; old != nil {
		old.halt()
		delete(r.entries, payload.ID)
	}
	if err := r.registerService(ctx, payload); err != nil {
		// The agent may have applied the write before its response was lost.
		r.deregisterQuiet(payload.ID)
		return fmt.Errorf("consul registry: register %s: %w", payload.ID, err)
	}
	eCtx, cancel := context.WithCancel(r.life.ctx)
	e := &entry{svcID: payload.ID, checkID: payload.Check.CheckID,
		payload: payload, cancel: cancel, done: make(chan struct{})}
	r.entries[payload.ID] = e
	go r.keepAlive(eCtx, e)
	return nil
}

// Deregister stops the keepalive and removes the service. Unknown IDs are a no-op.
func (r *Registry) Deregister(ctx context.Context, instance *registry.ServiceInstance) error {
	if err := instance.Validate(); err != nil {
		return err
	}
	if instance.ID == "" {
		return registry.ErrInvalidID
	}
	ctx, done, err := r.life.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	id := serviceID(instance.Name, instance.ID)
	if e := r.entries[id]; e != nil {
		e.halt()
	}
	if err := r.deregisterService(ctx, id); err != nil {
		// Retain the stopped entry so Close can retry cleanup.
		return fmt.Errorf("consul registry: deregister %s: %w", id, err)
	}
	delete(r.entries, id)
	return nil
}

// Close cancels and waits for operations and keepalives, then deregisters
// owned services with a bounded cleanup context. Concurrent calls wait for
// the same result. The supplied HTTP client remains open.
func (r *Registry) Close() error {
	r.closeOnce.Do(func() {
		r.life.stop()
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		var errs []error
		for _, e := range r.entries {
			e.halt()
			if err := r.deregisterService(ctx, e.svcID); err != nil {
				errs = append(errs, fmt.Errorf("deregister %s: %w", e.svcID, err))
			}
		}
		clear(r.entries)
		r.closeErr = errors.Join(errs...)
	})
	return r.closeErr
}

// keepAlive passes the TTL check on every tick. If Consul no longer knows
// the check, the service is registered again.
func (r *Registry) keepAlive(ctx context.Context, e *entry) {
	defer close(e.done)

	t := time.NewTicker(r.opts.interval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}

		err := r.passCheck(ctx, e.checkID)
		switch {
		case err == nil:
		case ctx.Err() != nil:
			return
		case isNotFound(err):
			r.opts.logger.Warn("consul registry: check unknown to Consul, registering again", "service", e.svcID)
			if err := r.registerService(ctx, e.payload); err != nil && ctx.Err() == nil {
				r.opts.logger.Warn("consul registry: register again failed", "service", e.svcID, "error", err)
			}
		default:
			r.opts.logger.Warn("consul registry: passing the check failed", "service", e.svcID, "error", err)
		}
	}
}

func (r *Registry) registerService(ctx context.Context, payload *agentService) error {
	_, err := r.c.do(ctx, "PUT", "/v1/agent/service/register",
		url.Values{"replace-existing-checks": {"true"}}, payload, nil, false)
	return err
}

func (r *Registry) passCheck(ctx context.Context, checkID string) error {
	_, err := r.c.do(ctx, "PUT", "/v1/agent/check/pass/"+url.PathEscape(checkID), nil, nil, nil, false)
	return err
}

// deregisterService treats an unknown service as already deregistered.
func (r *Registry) deregisterService(ctx context.Context, svcID string) error {
	_, err := r.c.do(ctx, "PUT", "/v1/agent/service/deregister/"+url.PathEscape(svcID), nil, nil, nil, false)
	if isNotFound(err) {
		return nil
	}
	return err
}

// deregisterQuiet deregisters with its own timeout and only logs failures.
func (r *Registry) deregisterQuiet(svcID string) {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	if err := r.deregisterService(ctx, svcID); err != nil {
		r.opts.logger.Warn("consul registry: deregister failed", "service", svcID, "error", err)
	}
}

// lifetime coordinates in-flight calls with Close for both independent roles.
// The mutex prevents Add from racing with the final Wait.
type lifetime struct {
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	closed     bool
	operations sync.WaitGroup
}

func newLifetime(parent context.Context) *lifetime {
	ctx, cancel := context.WithCancel(parent)
	return &lifetime{ctx: ctx, cancel: cancel}
}

func (l *lifetime) begin(ctx context.Context) (context.Context, func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.ctx.Err() != nil {
		return nil, nil, registry.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(l.ctx, cancel)
	l.operations.Add(1)
	return ctx, func() {
		stop()
		cancel()
		l.operations.Done()
	}, nil
}

func (l *lifetime) stop() {
	l.mu.Lock()
	l.closed = true
	l.cancel()
	l.mu.Unlock()
	l.operations.Wait()
}
