// Package memory is an in-process implementation of registry.Registry and
// registry.Discovery. It is meant for tests, local development and single
// binary deployments; one *Registry plays both roles.
package memory

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/apus-run/gala/registry"
)

const defaultBufferSize = 64

var (
	_ registry.Registry  = (*Registry)(nil)
	_ registry.Discovery = (*Registry)(nil)
)

// Option configures a Registry.
type Option interface {
	apply(*Registry)
}

// OptionFunc adapts a plain function to Option.
type OptionFunc func(*Registry)

func (f OptionFunc) apply(o *Registry) {
	f(o)
}

// WithBufferSize sets how many events a subscription can queue beyond its
// initial replay before it is closed as a slow consumer. Default: 64.
func WithBufferSize(n int) Option {
	return OptionFunc(func(r *Registry) {
		if n > 0 {
			r.bufferSize = n
		}
	})
}

// Registry is a registry.Registry and registry.Discovery held in memory.
type Registry struct {
	bufferSize int

	mu       sync.Mutex // guards everything below
	closed   bool
	services map[string]map[string]*registry.ServiceInstance // name -> id -> instance (owned copies)
	subs     map[string]map[*subscriber]struct{}             // name -> subscribers
}

type subscriber struct {
	ch     chan registry.Event
	stop   func() bool // unhooks the ctx watcher
	closed bool        // ch is closed; guarded by Registry.mu, which also guards every send
}

// New returns an empty Registry.
func New(opts ...Option) *Registry {
	r := &Registry{
		bufferSize: defaultBufferSize,
		services:   make(map[string]map[string]*registry.ServiceInstance),
		subs:       make(map[string]map[*subscriber]struct{}),
	}
	for _, opt := range opts {
		opt.apply(r)
	}
	return r
}

// Register stores a copy of the instance. If it has no ID, one is generated
// and written back to instance.ID. Registering the same Name and ID again
// replaces the instance.
func (r *Registry) Register(ctx context.Context, instance *registry.ServiceInstance) error {
	if err := instance.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	stored := instance.Clone()
	if stored.ID == "" {
		stored.ID = registry.NewID()
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return registry.ErrClosed
	}
	byID := r.services[stored.Name]
	if byID == nil {
		byID = make(map[string]*registry.ServiceInstance)
		r.services[stored.Name] = byID
	}
	byID[stored.ID] = stored
	r.publish(stored.Name, registry.Event{Type: registry.EventTypeAdd, Instance: stored.Clone()})

	instance.ID = stored.ID
	return nil
}

// Deregister removes the instance. Removing an unknown instance is not an
// error. instance.ID is required.
func (r *Registry) Deregister(ctx context.Context, instance *registry.ServiceInstance) error {
	if err := instance.Validate(); err != nil {
		return err
	}
	if instance.ID == "" {
		return fmt.Errorf("%w: empty", registry.ErrInvalidID)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	cur, ok := r.services[instance.Name][instance.ID]
	if !ok {
		return nil
	}
	delete(r.services[instance.Name], instance.ID)
	if len(r.services[instance.Name]) == 0 {
		delete(r.services, instance.Name)
	}
	r.publish(instance.Name, registry.Event{Type: registry.EventTypeDelete, Instance: cur})
	return nil
}

// GetService returns copies of the current instances, sorted by ID.
func (r *Registry) GetService(ctx context.Context, serviceName string) ([]*registry.ServiceInstance, error) {
	if err := registry.ValidateName(serviceName); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, registry.ErrClosed
	}
	return r.snapshotLocked(serviceName), nil
}

// Subscribe implements registry.Discovery.Subscribe.
func (r *Registry) Subscribe(ctx context.Context, serviceName string) (<-chan registry.Event, error) {
	if err := registry.ValidateName(serviceName); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, registry.ErrClosed
	}

	snapshot := r.snapshotLocked(serviceName)
	// The replay must always fit: size the buffer for it plus the slack
	// that slow consumers are allowed.
	sub := &subscriber{ch: make(chan registry.Event, len(snapshot)+r.bufferSize)}
	for _, ins := range snapshot {
		sub.ch <- registry.Event{Type: registry.EventTypeAdd, Instance: ins}
	}

	if r.subs[serviceName] == nil {
		r.subs[serviceName] = make(map[*subscriber]struct{})
	}
	r.subs[serviceName][sub] = struct{}{}
	// If ctx is already done the callback runs at once but blocks on r.mu
	// until we return, so the subscriber is registered before it is dropped.
	sub.stop = context.AfterFunc(ctx, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.dropLocked(serviceName, sub)
	})
	return sub.ch, nil
}

// Close closes every subscription and drops all instances. It is idempotent.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	for name, set := range r.subs {
		for sub := range set {
			r.dropLocked(name, sub)
		}
	}
	r.services = make(map[string]map[string]*registry.ServiceInstance)
	return nil
}

// snapshotLocked returns copies of a service's instances sorted by ID.
func (r *Registry) snapshotLocked(name string) []*registry.ServiceInstance {
	byID := r.services[name]
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := make([]*registry.ServiceInstance, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id].Clone())
	}
	return out
}

// publish sends ev to every subscriber of name without blocking. A
// subscriber whose buffer is full is closed instead of losing the event
// silently. Callers hold r.mu.
func (r *Registry) publish(name string, ev registry.Event) {
	for sub := range r.subs[name] {
		select {
		case sub.ch <- ev:
		default:
			r.dropLocked(name, sub)
		}
	}
}

// dropLocked closes a subscription once. Callers hold r.mu, which is also
// what makes it safe against concurrent sends.
func (r *Registry) dropLocked(name string, sub *subscriber) {
	if sub.closed {
		return
	}
	sub.closed = true
	close(sub.ch)
	delete(r.subs[name], sub)
	if len(r.subs[name]) == 0 {
		delete(r.subs, name)
	}
	if sub.stop != nil {
		sub.stop()
	}
}
