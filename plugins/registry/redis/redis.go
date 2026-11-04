// Package redis implements registry.Registry and registry.Discovery on Redis
// through go-redis | redis 包通过 go-redis 在 Redis 上实现 registry.Registry 与
// registry.Discovery.
//
// # Notes | 说明
//
// It needs a running Redis; the caller passes an rdb.Cmdable (the result of
// an rdb Provider's DB, or any go-redis client) to NewRegistry and
// NewDiscovery and keeps owning its lifecycle. Each service is one zset plus
// one hash under the fixed prefixes "gala:services:{<escaped-name>}:z" and
// "gala:services:{<escaped-name>}:h" (not configurable): the zset member is the
// instance ID scored by its lease deadline (Unix milliseconds), the hash
// field is the instance JSON. A lease is per
// instance — a crashed instance disappears within one lease while its
// siblings stay visible — and one heartbeat goroutine renews every registered
// instance every LeaseTTL/3. Discovery has no watch in Redis: Subscribe
// replays a snapshot and then polls, diffing by ID, every PollInterval. The
// two roles are closed independently.
// 本包需要运行中的 Redis；调用方向 NewRegistry 与 NewDiscovery 传入 rdb.Cmdable
// （rdb Provider 的 DB() 结果，或任何 go-redis 客户端），其生命周期仍归调用方。每
// 个服务是一个 zset 加一个 hash，固定前缀 "gala:services:{<escaped-name>}:z" 与
// "gala:services:{<escaped-name>}:h"（不可配置）：zset 的 member 是实例 ID，score 是租约
// 到期的 Unix 毫秒；hash 的 field 存实例 JSON。租约按实例生效——崩溃的实例在一
// 个租约内消失，兄弟实例不受影响——一个心跳 goroutine 每 LeaseTTL/3 为所有已注
// 册实例续约。Redis 没有 watch：Subscribe 先重放快照，再按 ID 做 diff，每
// PollInterval 轮询一次。两个角色独立 Close。
//
// # Example | 示例
//
//	p, err := rdb.Connect(ctx, &rdb.Options{Addr: "127.0.0.1:6379"})
//	r, err := redis.NewRegistry(p.DB(ctx))
//	defer r.Close() // deregisters every instance still alive
package redis

import (
	"context"
	json "encoding/json/v2"
	"fmt"
	"hash/maphash"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/apus-run/gala/components/rdb"
	"github.com/apus-run/gala/registry"
)

const (
	// defaultPrefix is fixed, like the etcd backend's prefix | 固定前缀，与 etcd 后端一致.
	defaultPrefix = "gala:services:"
	// defaultLeaseTTL bounds the crash-to-invisible window | 租约上限"崩溃到不可见"的窗口.
	defaultLeaseTTL = 30 * time.Second
	// closeTimeout bounds Close's cleanup writes | Close 清理写入的时限.
	closeTimeout = 5 * time.Second
	// minLeaseTTL keeps the heartbeat tick meaningful | 保证心跳间隔有意义的最小租约.
	minLeaseTTL = 300 * time.Millisecond
)

// All lease decisions use the clock of the Redis node that owns the keys.
const purgeExpired = `
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
local expired = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', now)
for _, id in ipairs(expired) do redis.call('HDEL', KEYS[2], id) end
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
`

var leaseScript = rdb.NewScript(purgeExpired + `
redis.call('ZADD', KEYS[1], now + tonumber(ARGV[1]), ARGV[2])
redis.call('HSET', KEYS[2], ARGV[2], ARGV[3])
local latest = redis.call('ZREVRANGE', KEYS[1], 0, 0, 'WITHSCORES')[2]
redis.call('PEXPIREAT', KEYS[1], latest)
redis.call('PEXPIREAT', KEYS[2], latest)
return 1
`)

// liveScript purges expired members and returns the live instance IDs.
// liveScript 清理过期成员并返回存活的实例 ID。
var liveScript = rdb.NewScript(purgeExpired + `
return redis.call('ZRANGEBYSCORE', KEYS[1], now, '+inf')
`)

var _ registry.Registry = (*Registry)(nil)

// Registry keeps its instances alive through per-instance leases renewed by
// one heartbeat goroutine. Discovery has an independent lifetime.
// Registry 通过按实例生效的租约保活实例，一个心跳 goroutine 统一续约。
// Discovery 的生命周期独立.
type Registry struct {
	client  rdb.Cmdable
	seed    maphash.Seed
	stripes [64]sync.Mutex
	ttl     time.Duration

	mu        sync.Mutex // guards everything below | 保护以下全部字段
	closed    bool
	instances map[string]map[string]*registry.ServiceInstance // name -> id -> stored copy | 名字 -> ID -> 存储副本
	ops       sync.WaitGroup                                  // in-flight Register/Deregister | 在途的 Register/Deregister

	beatCtx    context.Context
	beatCancel context.CancelFunc
	beatDone   chan struct{}
	closeOnce  sync.Once
	closeErr   error
}

// Option configures a Registry | Option 配置 Registry.
type Option interface {
	apply(*Registry)
}

// OptionFunc adapts a plain function to Option | OptionFunc 将普通函数适配为 Option.
type OptionFunc func(*Registry)

func (f OptionFunc) apply(r *Registry) {
	f(r)
}

// WithLeaseTTL sets how long an instance survives its last heartbeat; the
// heartbeat runs at LeaseTTL/3. Default: 30s, minimum: 300ms.
// WithLeaseTTL 设置实例距最后一次心跳的存活时长；心跳间隔为 LeaseTTL/3。
// 默认 30s，最小 300ms。
func WithLeaseTTL(ttl time.Duration) Option {
	return OptionFunc(func(r *Registry) {
		if ttl >= minLeaseTTL {
			r.ttl = ttl
		}
	})
}

// NewRegistry starts the heartbeat. client is an rdb.Cmdable — pass the
// result of an rdb Provider's DB, or any go-redis client; its lifecycle stays
// with the caller.
// NewRegistry 启动心跳。client 是 rdb.Cmdable——传 rdb Provider 的 DB() 结果或任
// 何 go-redis 客户端；其生命周期仍归调用方.
func NewRegistry(client rdb.Cmdable, opts ...Option) (*Registry, error) {
	if client == nil {
		return nil, fmt.Errorf("redis registry: nil client")
	}
	r := &Registry{
		client:    client,
		seed:      maphash.MakeSeed(),
		ttl:       defaultLeaseTTL,
		instances: make(map[string]map[string]*registry.ServiceInstance),
		beatDone:  make(chan struct{}),
	}
	for _, opt := range opts {
		opt.apply(r)
	}
	r.beatCtx, r.beatCancel = context.WithCancel(context.Background())
	go r.heartbeat()
	return r, nil
}

// Register stores the instance and lets the heartbeat keep it alive. If it has
// no ID, one is generated and written back to instance.ID. Registering the
// same Name and ID again replaces the instance. A failed write leaves the previous registration intact.
// Register 成功后才更新本地续租记录。
func (r *Registry) Register(ctx context.Context, instance *registry.ServiceInstance) error {
	if err := instance.Validate(); err != nil {
		return err
	}
	stored := instance.Clone()
	if stored.ID == "" {
		stored.ID = registry.NewID()
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return registry.ErrClosed
	}
	r.ops.Add(1)
	r.mu.Unlock()
	defer r.ops.Done()
	instance.ID = stored.ID
	lock := r.instanceLock(stored.Name, stored.ID)
	lock.Lock()
	defer lock.Unlock()
	if err := r.lease(ctx, stored); err != nil {
		return err
	}
	r.mu.Lock()
	byID := r.instances[stored.Name]
	if byID == nil {
		byID = make(map[string]*registry.ServiceInstance)
		r.instances[stored.Name] = byID
	}
	byID[stored.ID] = stored
	r.mu.Unlock()
	return nil
}

// Deregister removes the instance and stops keeping it alive. Removing an
// unknown instance is not an error; instance.ID is required. After Close it
// is a silent no-op, like the memory backend.
// Deregister 移除实例并停止保活。移除不存在的实例不算错误；instance.ID 必填。
// Close 之后是静默 no-op，与 memory 后端一致.
func (r *Registry) Deregister(ctx context.Context, instance *registry.ServiceInstance) error {
	if err := instance.Validate(); err != nil {
		return err
	}
	if instance.ID == "" {
		return fmt.Errorf("%w: empty", registry.ErrInvalidID)
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.ops.Add(1)
	r.mu.Unlock()
	defer r.ops.Done()
	lock := r.instanceLock(instance.Name, instance.ID)
	lock.Lock()
	defer lock.Unlock()
	if err := r.remove(ctx, instance); err != nil {
		return fmt.Errorf("redis registry: deregister: %w", err)
	}
	r.mu.Lock()
	if byID := r.instances[instance.Name]; byID != nil {
		delete(byID, instance.ID)
		if len(byID) == 0 {
			delete(r.instances, instance.Name)
		}
	}
	r.mu.Unlock()
	return nil
}

// Close stops the heartbeat, waits for in-flight Register/Deregister calls and
// removes every still-registered instance with its own bounded context.
// Concurrent calls wait for the same result. The caller's client stays open.
// Close 停止心跳，等待在途的 Register/Deregister，并用独立的限时上下文移除所有
// 仍注册的实例。并发调用会等待同一个结果。调用方的 client 保持打开.
func (r *Registry) Close() error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.beatCancel()
		r.mu.Unlock()

		<-r.beatDone // heartbeat stopped; it no longer touches redis | 心跳已停，不再触碰 redis
		r.ops.Wait()

		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		for _, byID := range r.instances {
			for _, ins := range byID {
				if err := r.remove(ctx, ins); err != nil && r.closeErr == nil {
					r.closeErr = err // first error wins; keep removing the rest | 首个错误保留，其余继续删
				}
			}
		}
	})
	return r.closeErr
}

// remove is Deregister's redis half without the bookkeeping, used by
// Close | remove 是 Deregister 的 redis 半边，去掉簿记，供 Close 使用.
func (r *Registry) remove(ctx context.Context, ins *registry.ServiceInstance) error {
	zset, data := serviceKeys(ins.Name)
	pipe := r.client.TxPipeline()
	pipe.ZRem(ctx, zset, ins.ID)
	pipe.HDel(ctx, data, ins.ID)
	_, err := pipe.Exec(ctx)
	return err
}

func serviceKeys(name string) (string, string) {
	base := defaultPrefix + "{" + url.PathEscape(name) + "}"
	return base + ":z", base + ":h"
}

func (r *Registry) instanceLock(name, id string) *sync.Mutex {
	index := maphash.String(r.seed, name+"\x00"+id) % uint64(len(r.stripes))
	return &r.stripes[index]
}

// lease writes one instance under its key pair | lease 把一个实例写进它的键对.
func (r *Registry) lease(ctx context.Context, ins *registry.ServiceInstance) error {
	value, err := json.Marshal(ins)
	if err != nil {
		return fmt.Errorf("redis registry: encode instance: %w", err)
	}
	zset, data := serviceKeys(ins.Name)
	if err := leaseScript.Run(ctx, r.client, []string{zset, data}, r.ttl.Milliseconds(), ins.ID, value).Err(); err != nil {
		return fmt.Errorf("redis registry: register: %w", err)
	}
	return nil
}

// heartbeat renews every registered instance's lease until Close. Errors are
// logged and retried on the next tick: an outage shorter than the lease is
// survived, a longer one lets the instances expire — which is the point of a
// lease. It runs on its own context, so a request-scoped Register ctx never
// decides whether instances stay registered.
// heartbeat 为所有已注册实例续约，直到 Close。错误记日志后等下一 tick 重试：
// 短于租约的故障可以扛过，更长的故障让实例过期——这正是租约的意义。心跳用自己
// 的 context，请求级的 Register ctx 不决定实例是否保持注册.
func (r *Registry) heartbeat() {
	defer close(r.beatDone)
	ticker := time.NewTicker(r.ttl / 3)
	defer ticker.Stop()
	for {
		select {
		case <-r.beatCtx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(r.beatCtx, r.ttl/3)
			r.mu.Lock()
			snapshot := make([]*registry.ServiceInstance, 0, 8)
			for _, byID := range r.instances {
				for _, ins := range byID {
					snapshot = append(snapshot, ins)
				}
			}
			r.mu.Unlock()
			for _, ins := range snapshot {
				lock := r.instanceLock(ins.Name, ins.ID)
				// Retry busy stripes next tick so unrelated leases can renew.
				if !lock.TryLock() {
					continue
				}
				r.mu.Lock()
				current := r.instances[ins.Name][ins.ID]
				r.mu.Unlock()
				var err error
				if current == ins {
					err = r.lease(ctx, ins)
				}
				lock.Unlock()
				if err != nil && ctx.Err() == nil {
					slog.Warn("redis registry: heartbeat failed", "instance", ins.String(), "err", err)
				}
			}
			cancel()
		}
	}
}
