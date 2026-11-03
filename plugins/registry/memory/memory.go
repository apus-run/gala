// Package memory is an in-process registry.Registry and registry.Discovery
// | memory 包是进程内的 registry.Registry 与 registry.Discovery 实现.
//
// # Notes | 说明
//
// State lives in memory only: no persistence, no network, nothing visible
// across processes. It suits tests, local development and single-binary
// deployments; one *Registry plays both roles. Register stores a copy and
// generates the ID when missing; re-registering the same Name and ID replaces
// the instance. Subscribe replays the current snapshot as Add events and then
// streams live events; a subscriber whose buffer overflows is closed — never
// blocked, never silently dropped — and should resubscribe. Close is idempotent
// and drops all instances and subscriptions.
// 状态仅存于内存：不持久化、不联网、跨进程不可见。适用于测试、本地开发与单二进制
// 部署；同一个 *Registry 同时扮演两个角色。Register 存储副本，ID 缺失时自动生成；
// 相同 Name 与 ID 重复注册会替换实例。Subscribe 先把当前快照重放为 Add 事件，再
// 推送实时事件；缓冲区溢出的订阅者会被关闭——既不阻塞也不静默丢弃——应重新订阅。
// Close 幂等，清空全部实例与订阅。
//
// # Example | 示例
//
//	r := memory.New()
//	_ = r.Register(ctx, &registry.ServiceInstance{
//		Name: "svc", Endpoints: []string{"grpc://127.0.0.1:9000"},
//	})
//	events, err := r.Subscribe(ctx, "svc")
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

// Option configures a Registry | Option 配置 Registry.
type Option interface {
	apply(*Registry)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配为 Option.
type OptionFunc func(*Registry)

func (f OptionFunc) apply(o *Registry) {
	f(o)
}

// WithBufferSize sets how many events a subscription can queue beyond its
// initial replay before it is closed as a slow consumer. Default: 64.
// WithBufferSize 设置订阅在初始重放之外还能排队的事件数，超过即作为慢消费者
// 被关闭。默认：64.
func WithBufferSize(n int) Option {
	return OptionFunc(func(r *Registry) {
		if n > 0 {
			r.bufferSize = n
		}
	})
}

// Registry is a registry.Registry and registry.Discovery held in memory.
// Registry 是保存在内存中的 registry.Registry 与 registry.Discovery.
type Registry struct {
	bufferSize int

	mu       sync.Mutex // guards everything below | 保护以下全部字段
	closed   bool
	services map[string]map[string]*registry.ServiceInstance // name -> id -> instance (owned copies) | 名字 -> ID -> 实例（自持副本）
	subs     map[string]map[*subscriber]struct{}             // name -> subscribers | 名字 -> 订阅者
}

type subscriber struct {
	ch   chan registry.Event
	stop func() bool // unhooks the ctx watcher | 解除 ctx 监听
	// ch is closed; guarded by Registry.mu, which also guards every send
	// ch 已关闭；由 Registry.mu 保护，每次发送也在其保护之下
	closed bool
}

// New returns an empty Registry | New 返回一个空的 Registry.
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
// Register 存储实例的副本。没有 ID 时自动生成并写回 instance.ID。
// 相同 Name 与 ID 重复注册会替换实例.
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
// Deregister 移除实例。移除不存在的实例不算错误。instance.ID 必填.
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
// GetService 返回当前实例的副本，按 ID 排序.
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
// Subscribe 实现 registry.Discovery.Subscribe.
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
	// 重放必须永远装得下：缓冲区按重放大小加上允许慢消费者的余量来定。
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
	// 若 ctx 已经结束，回调会立刻运行但会阻塞在 r.mu 上直到本函数返回，
	// 因此订阅者总是先注册、后被移除.
	sub.stop = context.AfterFunc(ctx, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.dropLocked(serviceName, sub)
	})
	return sub.ch, nil
}

// Close closes every subscription and drops all instances. It is idempotent.
// Close 关闭所有订阅并清空全部实例。幂等.
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
// snapshotLocked 返回某服务实例的副本，按 ID 排序.
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
// publish 把 ev 发给 name 的每个订阅者且不阻塞。缓冲区满的订阅者会被关闭，
// 而不是静默丢掉事件。调用方需持有 r.mu.
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
// dropLocked 只关闭一次订阅。调用方需持有 r.mu，这也是并发发送安全的保证.
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
