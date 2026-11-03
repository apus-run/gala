// Package etcd implements registry.Registry and registry.Discovery on etcd v3
// through the official clientv3 SDK | etcd 包通过官方 clientv3 SDK 在 etcd v3 上
// 实现 registry.Registry 与 registry.Discovery.
//
// # Notes | 说明
//
// It needs a running etcd cluster; the caller creates, owns and closes the
// *clientv3.Client and passes it to NewRegistry and NewDiscovery. Instances
// are stored as JSON under the fixed prefix /services/gala/executor/<name>/<id>
// (not configurable). Registry keeps every instance under one shared session
// lease: losing the lease ends the registry, so the caller must build a new
// one and register again; Close revokes the lease with a bounded context,
// removing all its instances at once. Discovery holds no lease: Subscribe
// replays a consistent snapshot and then watches from the next revision, so
// writes between the read and the watch are not lost; a slow consumer or a
// watch error closes the channel and the consumer resubscribes. The two roles
// are closed independently.
// 本包需要运行中的 etcd 集群；*clientv3.Client 由调用方创建、持有并关闭，再传入
// NewRegistry 与 NewDiscovery。实例以 JSON 存储在固定前缀
// /services/gala/executor/<name>/<id> 下（不可配置）。Registry 的所有实例共用一个
// session 租约：租约丢失即 Registry 失效，调用方需重建并重新注册；Close 在限时
// 上下文里撤销租约，一次性移除其全部实例。Discovery 不持有租约：Subscribe 先重放
// 一致快照，再从下一 revision 开始 watch，读与 watch 之间的写入不会丢失；慢消费者
// 或 watch 错误会关闭通道，由消费者重新订阅。两个角色独立 Close。
//
// # Example | 示例
//
//	client, err := clientv3.New(clientv3.Config{Endpoints: []string{"127.0.0.1:2379"}})
//	r, err := etcd.NewRegistry(client)
//	defer r.Close() // revokes the shared lease
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
// Registry 把所有实例都保活在一个共享 session 租约下。租约丢失即本 registry
// 失效，调用方需新建一个并重新注册。Discovery 的生命周期独立.
type Registry struct {
	client    *clientv3.Client
	session   *concurrency.Session
	life      *lifetime
	closeOnce sync.Once
	closeErr  error
}

// NewRegistry starts a shared lease keepalive. The caller owns client.
// NewRegistry 启动共享租约的保活。client 归调用方所有.
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

// Register puts the instance under the shared lease | Register 把实例写入
// 共享租约之下.
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
		// 若 Put 成功但响应丢失，重试时保持同一个 key。
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

// Deregister deletes the instance key | Deregister 删除实例的 key.
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
// Close 停止注册操作并撤销共享租约。Revoke 使用自己的限时上下文，调用方上下文
// 已过期也不会跳过清理。并发调用会等待同一个结果。调用方的 client 保持打开.
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
