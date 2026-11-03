// Package consul implements registry.Registry and registry.Discovery on the
// Consul agent HTTP API, using only the standard library | consul 包基于 Consul
// agent 的 HTTP API 实现 registry.Registry 与 registry.Discovery，仅用标准库.
//
// # Notes | 说明
//
// It needs a reachable Consul agent: the address defaults to $CONSUL_HTTP_ADDR
// (else http://127.0.0.1:8500) and the ACL token to $CONSUL_HTTP_TOKEN.
// Register gives each instance a TTL health check passed every TTL/3 and
// registers it again if the agent has forgotten it; a dead process turns
// critical and Consul drops it after WithDeregisterCriticalAfter. Discovery
// returns only passing instances and diffs blocking-query snapshots into
// Add/Delete events; transient failures are retried, but a fatal 4xx or a slow
// consumer closes the channel, and transitions between snapshots are not
// replayed. Registry and Discovery are separate types, closed independently.
// The first endpoint maps to Consul Address/Port; the full endpoint list,
// version and ID travel in Meta under the reserved "gala." prefix (user
// metadata must not use it), and services registered by other tools are still
// discovered, as one "tcp://host:port" endpoint.
// 本包需要可访问的 Consul agent：地址默认取 $CONSUL_HTTP_ADDR（否则
// http://127.0.0.1:8500），ACL token 取 $CONSUL_HTTP_TOKEN。Register 为每个实例
// 创建 TTL 健康检查，每 TTL/3 续约一次，agent 遗忘时自动重注册；进程死亡后检查转
// critical，超过 WithDeregisterCriticalAfter 被 Consul 移除。Discovery 只返回健康
// 实例，将阻塞查询的快照差分为 Add/Delete 事件；瞬时失败自动重试，致命 4xx 或慢
// 消费者会关闭通道，快照之间的中间变化不重放。Registry 与 Discovery 是独立类型，
// 各自 Close。首个 endpoint 映射为 Consul 的 Address/Port；完整 endpoint 列表、
// 版本与 ID 放在保留的 "gala." 前缀 Meta 中（用户元数据不得使用），其它工具注册
// 的服务也能被发现，映射为单个 "tcp://host:port" endpoint。
//
// # Example | 示例
//
//	r, err := consul.NewRegistry(consul.WithAddress("http://127.0.0.1:8500"))
//	d, err := consul.NewDiscovery(consul.WithToken(token))
//	// ... r.Close() and d.Close() are separate
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
// Registry 持有已注册实例及其 TTL 续约。
// 通过 WithHTTPClient 传入的 HTTP client 归调用方所有。
type Registry struct {
	c         *client
	opts      Options
	life      *lifetime
	mu        sync.Mutex // serializes registration changes | 串行化注册变更
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

// NewRegistry creates a Registry backed by a Consul agent | 创建基于 Consul agent 的 Registry.
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
// Register 插入或更新一个实例并启动其 TTL 续约。生成的 ID 会在请求前写回，
// 使重试使用同一个 service key.
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
	// 替换 payload 之前先停掉旧的续约协程。
	if old := r.entries[payload.ID]; old != nil {
		old.halt()
		delete(r.entries, payload.ID)
	}
	if err := r.registerService(ctx, payload); err != nil {
		// The agent may have applied the write before its response was lost.
		// agent 可能在响应丢失之前已经应用了这次写入。
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
// Deregister 停止续约并移除服务。未知 ID 是空操作.
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
		// 保留已停止的 entry，让 Close 还有机会重试清理。
		return fmt.Errorf("consul registry: deregister %s: %w", id, err)
	}
	delete(r.entries, id)
	return nil
}

// Close cancels and waits for operations and keepalives, then deregisters
// owned services with a bounded cleanup context. Concurrent calls wait for
// the same result. The supplied HTTP client remains open.
// Close 取消并等待所有操作与续约结束，随后在限时的清理上下文里注销自有服务。
// 并发调用会等待同一个结果。传入的 HTTP client 保持打开.
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
// keepAlive 每个周期通过一次 TTL 检查。Consul 不再认识该检查时，
// 重新注册服务.
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
// deregisterService 把未知的 service 视为已经注销.
func (r *Registry) deregisterService(ctx context.Context, svcID string) error {
	_, err := r.c.do(ctx, "PUT", "/v1/agent/service/deregister/"+url.PathEscape(svcID), nil, nil, nil, false)
	if isNotFound(err) {
		return nil
	}
	return err
}

// deregisterQuiet deregisters with its own timeout and only logs failures.
// deregisterQuiet 用自己的超时注销服务，失败只记日志.
func (r *Registry) deregisterQuiet(svcID string) {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	if err := r.deregisterService(ctx, svcID); err != nil {
		r.opts.logger.Warn("consul registry: deregister failed", "service", svcID, "error", err)
	}
}

// lifetime coordinates in-flight calls with Close for both independent roles.
// The mutex prevents Add from racing with the final Wait.
// lifetime 为两个独立角色协调在途调用与 Close。
// 互斥锁防止 Add 与最终的 Wait 竞争.
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
