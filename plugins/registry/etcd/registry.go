// Package etcd provides service registration and discovery using the etcd SDK.
package etcd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"

	"github.com/apus-run/gala/registry"
)

const defaultPrefix = "/services/gala/executor"

func serviceKey(name string) string                    { return defaultPrefix + "/" + name + "/" }
func instanceKey(ins *registry.ServiceInstance) string { return serviceKey(ins.Name) + ins.ID }

const closeTimeout = 5 * time.Second

var _ registry.Registry = (*Registry)(nil)

// Registry keeps all its instances alive under one shared session lease.
// Lease loss ends this registry; the caller must create a new one and register
// again. Discovery has an independent lifetime.
type Registry struct {
	client    *clientv3.Client
	session   *concurrency.Session
	life      *lifetime
	closeOnce sync.Once
	closeErr  error
}

// NewRegistry starts a shared lease keepalive. The caller owns client.
func NewRegistry(client *clientv3.Client) (*Registry, error) {
	if client == nil {
		return nil, fmt.Errorf("etcd registry: nil client")
	}
	session, err := concurrency.NewSession(client)
	if err != nil {
		return nil, err
	}
	return &Registry{client: client, session: session, life: newLifetime(session.Ctx())}, nil
}

func (r *Registry) Register(ctx context.Context, ins *registry.ServiceInstance) error {
	if err := ins.Validate(); err != nil {
		return err
	}
	ctx, done, err := r.life.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	if ins.ID == "" {
		// Keep the same key on retry if Put succeeds but its response is lost.
		ins.ID = registry.NewID()
	}
	stored := ins.Clone()
	value, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	_, err = r.client.Put(ctx, instanceKey(stored), string(value), clientv3.WithLease(r.session.Lease()))
	return err
}

func (r *Registry) Deregister(ctx context.Context, ins *registry.ServiceInstance) error {
	if err := ins.Validate(); err != nil {
		return err
	}
	if ins.ID == "" {
		return registry.ErrInvalidID
	}
	ctx, done, err := r.life.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	_, err = r.client.Delete(ctx, instanceKey(ins))
	return err
}

// Close stops registration operations and revokes their shared lease. Revoke
// gets its own bounded context so an expired caller context cannot skip cleanup.
// Concurrent calls wait for the same result. The caller's client stays open.
func (r *Registry) Close() error {
	r.closeOnce.Do(func() {
		r.life.stop()
		r.session.Orphan()
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		_, r.closeErr = r.client.Revoke(ctx, r.session.Lease())
		if errors.Is(r.closeErr, rpctypes.ErrLeaseNotFound) {
			r.closeErr = nil
		}
	})
	return r.closeErr
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
