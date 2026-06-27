package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"

	"github.com/apus-run/gala/registry"
)

const (
	defaultPrefix = "/services/gala/executor"
)

type Registry struct {
	client  *clientv3.Client
	session *concurrency.Session

	mutex       sync.Mutex
	watchCancel []func()
}

// NewRegistry creates a new etcd registry instance with the given client.
func NewRegistry(client *clientv3.Client) (*Registry, error) {
	session, err := concurrency.NewSession(client)
	if err != nil {
		return nil, err
	}
	return &Registry{
		client:  client,
		session: session,
	}, nil
}

func (r *Registry) Register(ctx context.Context, ins registry.ServiceInstance) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	value, err := json.Marshal(ins)
	if err != nil {
		return err
	}

	_, err = r.client.Put(ctx, r.instanceKey(ins), string(value), clientv3.WithLease(r.session.Lease()))
	return err
}

func (r *Registry) instanceKey(ins registry.ServiceInstance) string {
	return fmt.Sprintf("%s/%s/%s", defaultPrefix, ins.Name, ins.ID)
}

func (r *Registry) Deregister(ctx context.Context, ins registry.ServiceInstance) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	_, err := r.client.Delete(ctx, r.instanceKey(ins))
	return err
}

func (r *Registry) ListServices(ctx context.Context, serviceName string) ([]registry.ServiceInstance, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	resp, err := r.client.Get(ctx, r.serviceKey(serviceName), clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}

	services := make([]registry.ServiceInstance, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		var ins registry.ServiceInstance
		if err := json.Unmarshal(kv.Value, &ins); err != nil {
			return nil, err
		}
		services = append(services, ins)
	}
	return services, nil
}

func (r *Registry) serviceKey(serviceName string) string {
	return fmt.Sprintf("%s/%s", defaultPrefix, serviceName)
}

func (r *Registry) Subscribe(serviceName string) <-chan registry.Event {
	ctx, cancel := context.WithCancel(context.Background())
	ctx = clientv3.WithRequireLeader(ctx)
	r.mutex.Lock()
	r.watchCancel = append(r.watchCancel, cancel)
	r.mutex.Unlock()

	ch := r.client.Watch(ctx, r.serviceKey(serviceName), clientv3.WithPrefix(), clientv3.WithPrevKV())
	res := make(chan registry.Event)
	typesMap := map[mvccpb.Event_EventType]registry.EventType{
		mvccpb.PUT:    registry.EventTypeAdd,
		mvccpb.DELETE: registry.EventTypeDelete,
	}

	go func() {
		for {
			select {
			case watchResp := <-ch:
				if watchResp.Canceled {
					return
				}
				if watchResp.Err() != nil {
					continue
				}
				for _, ev := range watchResp.Events {
					res <- registry.Event{
						Type: typesMap[ev.Type],
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return res
}

func (r *Registry) Close() error {
	r.mutex.Lock()
	for _, cancel := range r.watchCancel {
		cancel()
	}
	r.mutex.Unlock()

	// Close etcd session
	return r.session.Close()
}
