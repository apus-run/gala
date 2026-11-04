package redis

import (
	"context"
	json "encoding/json/v2"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/apus-run/gala/components/rdb"
	"github.com/apus-run/gala/registry"
)

const (
	// defaultPollInterval is how often Subscribe re-reads the service | Subscribe 重读服务的间隔.
	defaultPollInterval = 500 * time.Millisecond
	// minPollInterval keeps the poller from hammering Redis | 防止轮询打爆 Redis 的下限.
	minPollInterval = 50 * time.Millisecond
	// subBufferSlack is the event budget beyond the replay a subscriber gets | 订阅者在重放之外的事件余量.
	subBufferSlack = 64
)

var _ registry.Discovery = (*Discovery)(nil)

// Discovery reads the Registry's zsets and hashes. Subscribe polls instead of
// watching: an event lands within one PollInterval of the change. Each
// subscription runs its own poller, so a slow consumer never blocks the others.
// Discovery 读取 Registry 的 zset 与 hash。Subscribe 用轮询而非 watch：事件最迟
// 在变更后一个 PollInterval 内到达。每个订阅有自己的轮询，慢消费者不阻塞别人.
type Discovery struct {
	client rdb.Cmdable

	poll time.Duration

	mu        sync.Mutex // guards everything below | 保护以下全部字段
	closed    bool
	ops       sync.WaitGroup // in-flight GetService calls | 在途的 GetService 调用
	subCtx    context.Context
	subCancel context.CancelFunc
	pollers   sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}

// DiscoveryOption configures a Discovery | DiscoveryOption 配置 Discovery.
type DiscoveryOption interface {
	apply(*Discovery)
}

// DiscoveryOptionFunc adapts a plain function to DiscoveryOption |
// DiscoveryOptionFunc 将普通函数适配为 DiscoveryOption.
type DiscoveryOptionFunc func(*Discovery)

func (f DiscoveryOptionFunc) apply(d *Discovery) {
	f(d)
}

// WithPollInterval sets how often Subscribe re-reads a service. Default:
// 500ms, minimum: 50ms.
// WithPollInterval 设置 Subscribe 重读服务的间隔。默认 500ms，最小 50ms。
func WithPollInterval(d time.Duration) DiscoveryOption {
	return DiscoveryOptionFunc(func(dis *Discovery) {
		if d >= minPollInterval {
			dis.poll = d
		}
	})
}

// NewDiscovery returns a Discovery over the same key layout the Registry
// writes. client is an rdb.Cmdable, like NewRegistry's; its lifecycle stays
// with the caller.
// NewDiscovery 返回按 Registry 的键布局读取的 Discovery。client 与 NewRegistry 的
// 一样是 rdb.Cmdable；其生命周期仍归调用方.
func NewDiscovery(client rdb.Cmdable, opts ...DiscoveryOption) (*Discovery, error) {
	if client == nil {
		return nil, fmt.Errorf("redis discovery: nil client")
	}
	d := &Discovery{
		client: client,

		poll: defaultPollInterval,
	}
	for _, opt := range opts {
		opt.apply(d)
	}
	d.subCtx, d.subCancel = context.WithCancel(context.Background())
	return d, nil
}

// GetService returns copies of the live instances, sorted by ID. Expired
// members are purged along the way.
// GetService 返回存活实例的副本，按 ID 排序。顺带清理过期成员。
func (d *Discovery) GetService(ctx context.Context, serviceName string) ([]*registry.ServiceInstance, error) {
	if err := registry.ValidateName(serviceName); err != nil {
		return nil, err
	}

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, registry.ErrClosed
	}
	d.ops.Add(1)
	d.mu.Unlock()
	defer d.ops.Done()

	return d.read(ctx, serviceName)
}

// read loads the live instances of one service | read 载入一个服务的存活实例.
func (d *Discovery) read(ctx context.Context, serviceName string) ([]*registry.ServiceInstance, error) {
	zset, data := serviceKeys(serviceName) // same layout Registry writes | 与 Registry 写入的布局一致
	res, err := liveScript.Run(ctx, d.client, []string{zset, data}).Result()
	if err != nil {
		return nil, fmt.Errorf("redis discovery: list %q: %w", zset, err)
	}
	ids, _ := res.([]any)
	if len(ids) == 0 {
		return nil, nil
	}
	fields := make([]string, len(ids))
	for i, id := range ids {
		fields[i] = id.(string)
	}
	values, err := d.client.HMGet(ctx, data, fields...).Result()
	if err != nil {
		return nil, fmt.Errorf("redis discovery: read %q: %w", data, err)
	}
	out := make([]*registry.ServiceInstance, 0, len(values))
	for _, v := range values {
		s, _ := v.(string)
		if s == "" { // hash field vanished between the two calls | 两次调用之间 hash 字段没了
			continue
		}
		ins := new(registry.ServiceInstance)
		if err := json.Unmarshal([]byte(s), ins); err != nil {
			// A foreign field under our key: skip it, do not fail the read.
			// key 下混入的外部字段：跳过，不让读取失败。
			continue
		}
		out = append(out, ins)
	}
	slices.SortFunc(out, func(a, b *registry.ServiceInstance) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// subscriber is one poller-backed subscription | subscriber 是一个轮询支撑的订阅.
type subscriber struct {
	name string
	ch   chan registry.Event
}

// Subscribe replays the current snapshot as Add events, then streams diffs
// every PollInterval. Poll errors are retried on the next tick: a Redis outage
// delays events but does not end the subscription; only ctx cancellation,
// Close or a full buffer closes the channel.
// Subscribe 先把当前快照重放为 Add 事件，再每 PollInterval 推送 diff。轮询错误
// 等下一 tick 重试：Redis 故障只会延迟事件，不会终止订阅；只有 ctx 取消、Close
// 或缓冲区满才会关闭频道.
func (d *Discovery) Subscribe(ctx context.Context, serviceName string) (<-chan registry.Event, error) {
	if err := registry.ValidateName(serviceName); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, registry.ErrClosed
	}
	// Counted under the same mutex as the closed check: a poller added after
	// Close's pollers.Wait() would escape Close and keep its channel open,
	// breaking "Close closes every live subscription".
	// 与 closed 检查同锁计数：晚于 Close 的 pollers.Wait() 才 Add 的 poller 会
	// 逃过 Close，频道不关，违反"Close 关闭所有活跃订阅"。
	d.pollers.Add(1)
	d.mu.Unlock()

	// First read is synchronous: the replay must fit the buffer, like memory.
	// 第一次读取是同步的：重放必须装进缓冲，与 memory 一致。
	snapshot, err := d.read(ctx, serviceName)
	if err != nil {
		d.pollers.Done()
		return nil, err
	}
	sub := &subscriber{
		name: serviceName,
		ch:   make(chan registry.Event, len(snapshot)+subBufferSlack),
	}
	for _, ins := range snapshot {
		sub.ch <- registry.Event{Type: registry.EventTypeAdd, Instance: ins.Clone()}
	}

	pollCtx, cancel := context.WithCancel(ctx)
	go func() {
		// Detach from the caller's ctx when the subscription ends, whatever
		// ended it; otherwise every Subscribe leaks one child registration.
		// 无论订阅因何结束，都从调用方 ctx 上摘除；否则每次 Subscribe 都泄漏
		// 一个子注册。
		defer cancel()
		defer d.pollers.Done()
		d.pollLoop(pollCtx, sub, snapshot)
	}()
	return sub.ch, nil
}

// pollLoop diffs the service until the subscription ends, then closes the
// channel. The caller's goroutine owns pollers accounting; pollLoop only
// closes ch.
// pollLoop 不断 diff 该服务直到订阅结束，然后关闭频道。pollers 计数归调用方
// goroutine 管；pollLoop 只负责关闭 ch.
func (d *Discovery) pollLoop(ctx context.Context, sub *subscriber, last []*registry.ServiceInstance) {
	defer close(sub.ch)
	ticker := time.NewTicker(d.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.subCtx.Done():
			return
		case <-ticker.C:
		}
		cur, err := d.read(ctx, sub.name)
		if err != nil {
			if ctx.Err() != nil || d.subCtx.Err() != nil {
				return
			}
			slog.Warn("redis discovery: poll failed, retrying next tick", "service", sub.name, "err", err)
			continue // transient outage: retry on the next tick | 短暂故障：下一 tick 重试
		}
		if !d.diff(sub, last, cur) {
			return // buffer overflow: subscription closed | 缓冲满：订阅被关闭
		}
		last = cur
	}
}

// diff emits Add/Delete events between old and cur. It reports false when the
// subscriber's buffer is full, in which case the caller closes the channel.
// diff 发出 old 与 cur 之间的 Add/Delete 事件。返回 false 表示订阅者缓冲已满，
// 调用方应关闭其频道.
func (d *Discovery) diff(sub *subscriber, old, cur []*registry.ServiceInstance) bool {
	prev := make(map[string]*registry.ServiceInstance, len(old))
	for _, ins := range old {
		prev[ins.ID] = ins
	}
	seen := make(map[string]bool, len(cur))
	for _, ins := range cur {
		seen[ins.ID] = true
		if p, ok := prev[ins.ID]; ok && p.Equal(ins) {
			continue // unchanged | 无变化
		}
		if !d.send(sub, registry.Event{Type: registry.EventTypeAdd, Instance: ins.Clone()}) {
			return false
		}
	}
	for _, ins := range old {
		if !seen[ins.ID] {
			if !d.send(sub, registry.Event{Type: registry.EventTypeDelete, Instance: ins.Clone()}) {
				return false
			}
		}
	}
	return true
}

// send delivers ev without blocking | send 不阻塞地投递 ev.
func (d *Discovery) send(sub *subscriber, ev registry.Event) bool {
	select {
	case sub.ch <- ev:
		return true
	default:
		return false // slow consumer: the poller exits and closes ch | 慢消费者：轮询退出并关闭 ch
	}
}

// Close stops every poller (their channels close) and waits for in-flight
// GetService calls. Idempotent. The caller's client stays open.
// Close 停止所有轮询（其频道关闭），等待在途的 GetService。幂等。调用方的
// client 保持打开.
func (d *Discovery) Close() error {
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		d.subCancel()
		d.mu.Unlock()
		d.pollers.Wait()
		d.ops.Wait()
	})
	return d.closeErr
}
