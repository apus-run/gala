package consul

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/apus-run/gala/registry"
)

var _ registry.Discovery = (*Discovery)(nil)

// Discovery reads and watches the healthy instances of a service.
type Discovery struct {
	c    *client
	opts options

	life *lifetime
}

// NewDiscovery returns a registry.Discovery that queries a Consul agent.
func NewDiscovery(opts ...Option) (*Discovery, error) {
	o, err := newOptions(opts)
	if err != nil {
		return nil, err
	}
	return &Discovery{c: newClient(o), opts: o, life: newLifetime(context.Background())}, nil
}

// GetService returns the instances of a service whose health checks pass,
// sorted by ID.
func (d *Discovery) GetService(ctx context.Context, serviceName string) ([]*registry.ServiceInstance, error) {
	if err := registry.ValidateName(serviceName); err != nil {
		return nil, err
	}
	ctx, done, err := d.life.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	instances, _, err := d.fetch(ctx, serviceName, 0)
	return instances, err
}

// Subscribe watches the healthy instances of a service. See
// registry.Discovery for the contract. An instance whose check turns
// critical is reported as Delete, and as Add again once it passes.
//
// Transient failures (network errors, 5xx) are retried inside the
// subscription: the next successful poll is diffed against the last known
// state, so consumers converge to the latest state. Intermediate transitions
// between polls are not replayed. A failure that cannot be fixed by retrying,
// such as a 403, closes the channel.
func (d *Discovery) Subscribe(ctx context.Context, serviceName string) (<-chan registry.Event, error) {
	if err := registry.ValidateName(serviceName); err != nil {
		return nil, err
	}
	ctx, done, err := d.life.begin(ctx)
	if err != nil {
		return nil, err
	}

	snapshot, index, err := d.fetch(ctx, serviceName, 0)
	if err != nil {
		done()
		return nil, err
	}
	out := make(chan registry.Event, len(snapshot)+d.opts.bufferSize)
	go func() {
		defer done()
		defer close(out)
		d.run(ctx, serviceName, snapshot, index, out)
	}()
	return out, nil
}

// run replays the snapshot, then polls with blocking queries and turns the
// difference between snapshots into events. The caller owns channel cleanup.
func (d *Discovery) run(
	ctx context.Context,
	serviceName string,
	snapshot []*registry.ServiceInstance,
	index uint64,
	out chan registry.Event,
) {
	last := make(map[string]*registry.ServiceInstance, len(snapshot))
	for _, ins := range snapshot {
		out <- registry.Event{Type: registry.EventTypeAdd, Instance: ins} // cannot block: buffer fits the replay
		last[ins.ID] = ins
	}

	delay := d.opts.retryMin
	for ctx.Err() == nil {
		start := time.Now()
		current, newIndex, err := d.fetch(ctx, serviceName, index)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if isFatal(err) {
				d.opts.logger.Warn("consul registry: watch failed for good, closing subscription",
					"service", serviceName, "error", err)
				return
			}
			d.opts.logger.Warn("consul registry: watch failed, retrying",
				"service", serviceName, "error", err, "retry_in", delay)
			if !sleep(ctx, delay) {
				return
			}
			delay = min(delay*2, d.opts.retryMax)
			continue
		}
		delay = d.opts.retryMin

		prev := index
		index = nextIndex(prev, newIndex)

		for _, ev := range diff(last, current) {
			select {
			case out <- ev:
			default:
				// Never block, never drop silently: closing tells the
				// consumer to resubscribe and rebuild its state.
				d.opts.logger.Warn("consul registry: slow consumer, closing subscription",
					"service", serviceName)
				return
			}
			if ev.Type == registry.EventTypeDelete {
				delete(last, ev.Instance.ID)
			} else {
				last[ev.Instance.ID] = ev.Instance
			}
		}

		// A query that returned at once with nothing new (the server
		// ignored the wait, or the index was reset) must not turn into a
		// busy loop.
		if newIndex <= prev {
			if rest := d.opts.minPoll - time.Since(start); rest > 0 && !sleep(ctx, rest) {
				return
			}
		}
	}
}

// Close closes every live subscription and waits for them to finish. It is
// idempotent.
func (d *Discovery) Close() error {
	d.life.stop()
	return nil
}

// fetch reads the healthy instances. With index > 0 it is a blocking query
// that returns when the data changes past index or the wait time elapses.
func (d *Discovery) fetch(ctx context.Context, serviceName string, index uint64) ([]*registry.ServiceInstance, uint64, error) {
	q := url.Values{"passing": {"true"}}
	blocking := index > 0
	if blocking {
		q.Set("index", strconv.FormatUint(index, 10))
		q.Set("wait", d.opts.wait.String())
	}

	var entries []healthEntry
	newIndex, err := d.c.do(ctx, "GET", "/v1/health/service/"+url.PathEscape(serviceName), q, nil, &entries, blocking)
	if err != nil {
		return nil, 0, fmt.Errorf("consul registry: query %s: %w", serviceName, err)
	}

	seen := make(map[string]struct{}, len(entries))
	out := make([]*registry.ServiceInstance, 0, len(entries))
	for _, e := range entries {
		ins := fromHealthEntry(e)
		if _, dup := seen[ins.ID]; dup { // same ID on several nodes: keep the first
			continue
		}
		seen[ins.ID] = struct{}{}
		out = append(out, ins)
	}
	slices.SortFunc(out, func(a, b *registry.ServiceInstance) int {
		return cmp.Compare(a.ID, b.ID)
	})
	return out, newIndex, nil
}

// nextIndex is the index to block on after a response with index got. Consul
// requires a fresh read when the index goes backwards. Otherwise use at
// least 1, so an empty result with index zero does not cause a busy loop.
func nextIndex(prev, got uint64) uint64 {
	if got < prev {
		return 0
	}
	return max(got, 1)
}

// diff returns the events that turn last into current: Delete for every
// instance that vanished, Add for every new or changed one, each group
// sorted by ID.
func diff(last map[string]*registry.ServiceInstance, current []*registry.ServiceInstance) []registry.Event {
	seen := make(map[string]struct{}, len(current))
	var adds []registry.Event
	for _, ins := range current { // current is sorted by ID
		seen[ins.ID] = struct{}{}
		if old, ok := last[ins.ID]; !ok || !old.Equal(ins) {
			adds = append(adds, registry.Event{Type: registry.EventTypeAdd, Instance: ins})
		}
	}
	var dels []registry.Event
	for id, old := range last {
		if _, ok := seen[id]; !ok {
			dels = append(dels, registry.Event{Type: registry.EventTypeDelete, Instance: old})
		}
	}
	slices.SortFunc(dels, func(a, b registry.Event) int {
		return cmp.Compare(a.Instance.ID, b.Instance.ID)
	})
	return append(dels, adds...)
}

// sleep waits for d or until ctx is done, and reports whether d elapsed.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
