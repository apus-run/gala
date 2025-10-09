package registry

import (
	"context"
)

// NoopRegistry is an empty implement of Registry
var NoopRegistry Registry = &noopRegistry{}

// NoopRegistry
type noopRegistry struct{}

// Subscribe implements Registry.
func (r *noopRegistry) Subscribe(serviceName string) <-chan Event {
	ch := make(chan Event)
	return ch
}

func (r *noopRegistry) Register(ctx context.Context, ins *ServiceInstance) error {
	return nil
}

func (r *noopRegistry) Deregister(ctx context.Context, ins *ServiceInstance) error {
	return nil
}

func (r *noopRegistry) ListServices(ctx context.Context, serviceName string) ([]ServiceInstance, error) {
	return nil, nil
}

func (r *noopRegistry) Close() error {
	return nil
}
