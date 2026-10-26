package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/apus-run/gala/registry"
)

const watchBufferSize = 64

var _ registry.Discovery = (*Discovery)(nil)

// Discovery reads and watches instances without acquiring a registration lease.
type Discovery struct {
	client *clientv3.Client
	life   *lifetime
}

// NewDiscovery creates a discovery client without issuing network requests.
// The caller owns client and may use it independently of a Registry.
func NewDiscovery(client *clientv3.Client) (*Discovery, error) {
	if client == nil {
		return nil, fmt.Errorf("etcd registry: nil client")
	}
	return &Discovery{client: client, life: newLifetime(client.Ctx())}, nil
}

// Close cancels reads and subscriptions and waits for them to finish.
// It is idempotent and leaves the caller's client and registry open.
func (d *Discovery) Close() error {
	d.life.stop()
	return nil
}

func (d *Discovery) GetService(ctx context.Context, serviceName string) ([]*registry.ServiceInstance, error) {
	if err := registry.ValidateName(serviceName); err != nil {
		return nil, err
	}
	ctx, done, err := d.life.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	instances, _, err := d.snapshot(ctx, serviceName)
	return instances, err
}

func (d *Discovery) snapshot(ctx context.Context, serviceName string) ([]*registry.ServiceInstance, int64, error) {
	resp, err := d.client.Get(ctx, serviceKey(serviceName), clientv3.WithPrefix())
	if err != nil {
		return nil, 0, err
	}
	instances := make([]*registry.ServiceInstance, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		ins, err := decodeInstance(kv)
		if err != nil {
			return nil, 0, err
		}
		instances = append(instances, ins)
	}
	return instances, resp.Header.Revision, nil
}

// Subscribe replays a consistent snapshot, then watches from its next revision
// so writes between the read and watch creation are not lost.
func (d *Discovery) Subscribe(ctx context.Context, serviceName string) (<-chan registry.Event, error) {
	if err := registry.ValidateName(serviceName); err != nil {
		return nil, err
	}
	ctx, done, err := d.life.begin(ctx)
	if err != nil {
		return nil, err
	}
	instances, revision, err := d.snapshot(ctx, serviceName)
	if err != nil {
		done()
		return nil, err
	}
	watch := d.client.Watch(clientv3.WithRequireLeader(ctx), serviceKey(serviceName),
		clientv3.WithPrefix(), clientv3.WithPrevKV(), clientv3.WithRev(revision+1), clientv3.WithCreatedNotify())
	// Wait for server acknowledgement so creation failures reach the caller.
	select {
	case <-ctx.Done():
		done()
		return nil, ctx.Err()
	case response, ok := <-watch:
		if !ok {
			done()
			return nil, fmt.Errorf("etcd registry: watch closed before creation")
		}
		if err := response.Err(); err != nil {
			done()
			return nil, err
		}
		if !response.Created {
			done()
			return nil, fmt.Errorf("etcd registry: missing watch creation acknowledgement")
		}
	}

	// Reserve enough space for the entire replay regardless of its size.
	out := make(chan registry.Event, len(instances)+watchBufferSize)
	for _, ins := range instances {
		out <- registry.Event{Type: registry.EventTypeAdd, Instance: ins}
	}
	go func() {
		defer done()
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case response, ok := <-watch:
				if !ok {
					return
				}
				if err := response.Err(); err != nil {
					slog.Warn("etcd registry: watch ended", "service", serviceName, "error", err)
					return
				}
				for _, event := range response.Events {
					ev, err := decodeEvent(event)
					if err != nil {
						slog.Warn("etcd registry: invalid watch event", "service", serviceName, "error", err)
						return
					}
					select {
					case <-ctx.Done():
						return
					case out <- ev:
					default:
						// Closing signals the consumer to rebuild from a new snapshot.
						slog.Warn("etcd registry: slow consumer, closing subscription", "service", serviceName)
						return
					}
				}
			}
		}
	}()
	return out, nil
}

func decodeInstance(kv *mvccpb.KeyValue) (*registry.ServiceInstance, error) {
	var ins registry.ServiceInstance
	if err := json.Unmarshal(kv.Value, &ins); err != nil {
		return nil, fmt.Errorf("etcd registry: decode %q: %w", kv.Key, err)
	}
	if err := ins.Validate(); err != nil {
		return nil, err
	}
	if ins.ID == "" || string(kv.Key) != defaultPrefix+"/"+ins.Name+"/"+ins.ID {
		return nil, fmt.Errorf("etcd registry: instance does not match key %q", kv.Key)
	}
	return &ins, nil
}

func decodeEvent(event *clientv3.Event) (registry.Event, error) {
	switch event.Type {
	case mvccpb.PUT:
		ins, err := decodeInstance(event.Kv)
		return registry.Event{Type: registry.EventTypeAdd, Instance: ins}, err
	case mvccpb.DELETE:
		if event.PrevKv != nil {
			ins, err := decodeInstance(event.PrevKv)
			return registry.Event{Type: registry.EventTypeDelete, Instance: ins}, err
		}
		key, ok := strings.CutPrefix(string(event.Kv.Key), defaultPrefix+"/")
		name, id, found := strings.Cut(key, "/")
		ins := &registry.ServiceInstance{Name: name, ID: id}
		if !ok || !found || id == "" || ins.Validate() != nil {
			return registry.Event{}, fmt.Errorf("etcd registry: invalid deletion key %q", event.Kv.Key)
		}
		return registry.Event{Type: registry.EventTypeDelete, Instance: ins}, nil
	default:
		return registry.Event{}, fmt.Errorf("etcd registry: unknown event type %v", event.Type)
	}
}
