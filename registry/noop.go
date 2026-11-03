package registry

import (
	"context"
)

// NoopRegistry is an empty implement of Registry | NoopRegistry 是 Registry 的空实现.
var NoopRegistry Registry = &noopRegistry{}

// noopRegistry is a Registry that does nothing | noopRegistry 是什么都不做的 Registry.
type noopRegistry struct{}

// Subscribe implements Registry | Subscribe 实现 Registry 接口.
func (r *noopRegistry) Subscribe(serviceName string) <-chan Event {
	ch := make(chan Event)
	return ch
}

// Register implements Registry | Register 实现 Registry 接口.
func (r *noopRegistry) Register(ctx context.Context, ins *ServiceInstance) error {
	return nil
}

// Deregister implements Registry | Deregister 实现 Registry 接口.
func (r *noopRegistry) Deregister(ctx context.Context, ins *ServiceInstance) error {
	return nil
}

// ListServices returns nothing | ListServices 恒返回空列表.
func (r *noopRegistry) ListServices(ctx context.Context, serviceName string) ([]ServiceInstance, error) {
	return nil, nil
}

// Close implements io.Closer | Close 实现 io.Closer 接口.
func (r *noopRegistry) Close() error {
	return nil
}
