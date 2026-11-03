// Package layercache implements a JetCache-style two-level cache (Local → Remote → origin) | 实现 JetCache 风格的两级缓存（Local → Remote → 源）.
//
// # Notes | 说明
//
// Adapted from JetCache-Go; see NOTICE.md. A Cache is a non-generic
// coordinator over serialized entries: the namespace is required, the local
// level defaults to a bounded built-in store, and the remote backend is
// application-supplied. Once deduplicates concurrent loads of the same key;
// background refresh and NX/XX writes need optional backend capabilities
// (LeaseRemote, ConditionalRemote). Events and Stats are advisory. It fences
// same-instance stale fills; it does not provide database consistency.
// 改编自 JetCache-Go（见 NOTICE.md）。Cache 是非泛型的序列化条目协调器：
// namespace 必填，本地层默认内置有界存储，远端由应用提供。Once 对同 key
// 的并发加载去重；后台刷新与 NX/XX 写入依赖可选后端能力（LeaseRemote、
// ConditionalRemote）。Event 与 Stats 是通告性质。它只防本实例的过期回填，
// 不提供数据库一致性。
//
// # Features | 功能
//
// - Get/Set/Once read-through with shared slow paths | 读穿透与共享慢路径
// - Typed[V] and T[K, V] typed views, batch MGet | 泛型视图与批量回源
// - Negative caching, TTL jitter, background refresh | 空值缓存、TTL 抖动、后台刷新
// - Pluggable Local/Remote/Codec, events and stats | 可插拔后端、事件与统计
//
// # Example | 示例
//
//	c, err := layercache.New(layercache.WithNamespace("order"), layercache.WithRemote(r))
//	var name string
//	err = c.Once(ctx, "u:42", layercache.Do(loadName), layercache.Value(&name))
package layercache

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"

	"sync"
	"sync/atomic"
	"time"

	"github.com/apus-run/gala/components/ecache"
)

// Cache is a non-generic serialized coordinator. Typed views share this instance.
// Construct with New; the zero value is not usable. Do not copy it.
// Supplied backends and their connections remain application-owned.
// Cache 是非泛型的序列化协调器。Typed 视图共享同一实例。请用 New 构造，
// 零值不可用，不要拷贝。传入的后端及其连接仍归应用所有。
type Cache struct {
	mu      sync.Mutex
	states  map[string]*keyState
	flights map[string]*flight
	refresh map[string]*refreshTask
	workCtx context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	local   Local
	remote  Remote
	opts    Options
	prefix  string
	events  chan Event
	metrics [9]atomic.Uint64
}

// New assembles and validates options before allocating resources. Namespace is
// required; a bounded local cache is provided by default. Options may be reused.
// New 在分配资源前组装并校验选项。namespace 必填；默认提供有界本地缓存。
// Options 可复用。
func New(opts ...Option) (*Cache, error) {
	for i, opt := range opts {
		if nilLike(opt) {
			return nil, fmt.Errorf("%w: option %d is nil", ErrInvalidOptions, i)
		}
	}
	configured := NewOptions(opts...)
	if err := configured.Validate(); err != nil {
		return nil, err
	}
	o := *configured
	if nilLike(o.local) {
		o.local = nil
	}
	if nilLike(o.remote) {
		o.remote = nil
	}
	if o.builtinLocal {
		o.local = ecache.New[string, Entry](ecache.WithMaxKeys(o.localCapacity))
	}
	if o.sourceID == "" {
		o.sourceID = rand.Text()
	}
	if o.refreshIdle == 0 {
		o.refreshIdle = o.refreshInterval + time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Cache{states: make(map[string]*keyState), flights: make(map[string]*flight), refresh: make(map[string]*refreshTask), workCtx: ctx, cancel: cancel, local: o.local, remote: o.remote, opts: o, prefix: strconv.Itoa(len(o.namespace)) + ":" + o.namespace + ":"}
	c.startMaintenance()
	return c, nil
}

// Get only reads caches and decodes into dst. Nil dst probes an envelope without
// decoding its business payload. A non-nil destination must be a non-nil pointer.
// Get 只读缓存并解码到 dst。dst 为 nil 时仅探测信封、不解码业务负载；
// 非 nil 目标必须是非 nil 指针。
func (c *Cache) Get(ctx context.Context, key string, dst any) error {
	return c.get(ctx, key, dst, false)
}

// GetSkippingLocal bypasses L1 and reads L2 only | GetSkippingLocal 跳过本地层，仅读远端。
func (c *Cache) GetSkippingLocal(ctx context.Context, key string, dst any) error {
	return c.get(ctx, key, dst, true)
}
func (c *Cache) get(ctx context.Context, key string, dst any, skip bool) error {
	done, err := c.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	if err := destination(dst); err != nil {
		return err
	}
	k := c.prefix + key
	if !skip {
		if e, ok := c.getLocal(k); ok {
			return c.decodeResult(k, e.Data, dst)
		}
	}
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	t := c.retain(k)
	defer c.release(t)
	e, err := c.readCache(ctx, t, skip)
	if err != nil {
		return err
	}
	return c.decodeResult(k, e.Data, dst)
}

// Exists reports key presence via a probe Get | Exists 通过探测 Get 判断 key 是否存在。
func (c *Cache) Exists(ctx context.Context, key string) (bool, error) {
	err := c.Get(ctx, key, nil)
	if errors.Is(err, ErrMiss) || c.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// Set uses Value(v) or Do(fn), with optional TTL/NX/XX/SkipLocal/Refresh controls.
// A rejected condition returns ErrNotStored and never publishes the candidate L1.
// Set 使用 Value(v) 或 Do(fn)，可选 TTL/NX/XX/SkipLocal/Refresh 控制。
// 条件被拒绝时返回 ErrNotStored，且绝不发布候选的 L1 值。
func (c *Cache) Set(ctx context.Context, key string, opts ...ItemOption) error {
	done, err := c.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	i, err := c.item(opts)
	if err != nil {
		return err
	}
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	value := i.value
	if i.do != nil {
		task, stop := context.WithTimeout(ctx, c.opts.loadTimeout)
		defer stop()
		c.incr(statLoad)
		value, err = i.do(task)
		if e := c.contextError(task); e != nil {
			err = e
		}
	}
	negative := c.IsNotFound(err)
	if err != nil && !negative {
		c.incr(statLoadError)
		return err
	}
	if negative && c.opts.notFoundTTL == 0 {
		return err
	}
	data, err := c.encode(value, negative)
	if err != nil {
		return &OpError{"encode", c.prefix + key, err}
	}
	t := c.retain(c.prefix + key)
	defer c.release(t)
	if err := c.lockKey(ctx, t); err != nil {
		return err
	}
	defer unlockKey(t)
	committed, err := c.commitEntry(ctx, t, data, i, true)
	if committed {
		c.send(EventSet, key)
	}
	if err != nil {
		return err
	}
	if i.refresh {
		if err := c.registerLocked(t.key, i); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes the key from local and remote | Delete 从本地与远端删除 key。
func (c *Cache) Delete(ctx context.Context, key string) error { return c.invalidateKey(ctx, key, true) }

// InvalidateLocal removes the key from the local level only
// | InvalidateLocal 仅从本地层删除 key。
func (c *Cache) InvalidateLocal(ctx context.Context, key string) error {
	return c.invalidateKey(ctx, key, false)
}
func (c *Cache) invalidateKey(ctx context.Context, key string, remote bool) error {
	done, err := c.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	t := c.retain(c.prefix + key)
	defer c.release(t)
	if err := c.lockKey(ctx, t); err != nil {
		return err
	}
	defer unlockKey(t)
	c.invalidate(t, remote)
	c.removeLocal(t.key)
	if remote {
		committed, err := c.deleteRemote(ctx, t.key)
		if committed {
			c.send(EventDelete, key)
		}
		if err != nil {
			return err
		}
	}
	if err := c.contextError(ctx); err != nil {
		return err
	}
	return nil
}

// Once follows JetCache: L1 fast path, shared double-check of L1/L2, then Do and
// write-back. Every waiter decodes independently. Equal keys must use compatible
// loader semantics; skip-local, TTL and conditional modes use separate flights.
// Once 遵循 JetCache：L1 快路径，L1/L2 共享二次检查，然后 Do 并回写。
// 每个等待者独立解码。相同 key 必须使用兼容的加载语义；skip-local、TTL 与
// 条件模式使用不同的 flight。
func (c *Cache) Once(ctx context.Context, key string, opts ...ItemOption) error {
	done, err := c.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	i, err := c.item(opts)
	if err != nil {
		return err
	}
	if err := destination(i.value); err != nil {
		return err
	}
	k := c.prefix + key
	if i.refresh {
		t := c.retain(k)
		err = c.lockKey(ctx, t)
		if err == nil {
			err = c.registerLocked(k, i)
			unlockKey(t)
		}
		c.release(t)
		if err != nil {
			return err
		}
	}
	if !i.skipLocal {
		if e, ok := c.getLocal(k); ok {
			return c.decodeResult(k, e.Data, i.value)
		}
	}
	t := c.retain(k)
	defer c.release(t)
	kind := fmt.Sprintf("once/%t/%d/%d/%t", i.skipLocal, i.ttl, i.mode(), i.do != nil)
	f, err := c.startTask(ctx, kind, []*ticket{t}, func(task context.Context) (any, error) { return c.load(task, key, t, i, false) }, nil)
	if err != nil {
		return err
	}
	v, err := c.wait(ctx, f)
	if err != nil {
		return err
	}
	return c.decodeResult(k, v.([]byte), i.value)
}
func (c *Cache) load(ctx context.Context, key string, t *ticket, i ItemOptions, force bool) ([]byte, error) {
	if !force {
		e, err := c.readCache(ctx, t, i.skipLocal)
		if err == nil {
			return e.Data, nil
		}
		if errors.Is(err, ErrCorrupt) || !errors.Is(err, ErrMiss) {
			return nil, err
		}
	}
	if i.do == nil {
		return nil, ErrMiss
	}
	if err := c.contextError(ctx); err != nil {
		return nil, err
	}
	c.incr(statLoad)
	value, sourceErr := i.do(ctx)
	if err := c.contextError(ctx); err != nil {
		sourceErr = err
	}
	negative := c.IsNotFound(sourceErr)
	if sourceErr != nil && !negative {
		c.incr(statLoadError)
		return nil, sourceErr
	}
	if negative && c.opts.notFoundTTL == 0 {
		if force {
			if err := c.refreshAbsent(ctx, t); err != nil {
				return nil, err
			}
		}
		return nil, sourceErr
	}
	data, err := c.encode(value, negative)
	if err != nil {
		return nil, &OpError{"encode", t.key, err}
	}
	committed, err := c.backfill(ctx, t, data, i)
	if committed {
		kind := EventLoad
		if force {
			kind = EventRefresh
		}
		c.send(kind, key)
	}
	if err != nil {
		if force || errors.Is(err, ErrInvalidated) || errors.Is(err, ErrClosed) || ctx.Err() != nil {
			return nil, err
		}
		c.report(err)
	}
	if negative {
		return nil, sourceErr
	}
	return data, nil
}
func (c *Cache) backfill(ctx context.Context, t *ticket, data []byte, i ItemOptions) (bool, error) {
	if err := c.lockKey(ctx, t); err != nil {
		return false, err
	}
	defer unlockKey(t)
	if err := c.checkGeneration(t); err != nil {
		return false, err
	}
	return c.commitEntry(ctx, t, data, i, false)
}

// IsNotFound reports whether err is a negative-cache hit, honoring the
// configured ErrNotFound | IsNotFound 判断 err 是否为空值缓存命中，含配置的 ErrNotFound。
func (c *Cache) IsNotFound(err error) bool {
	if errors.Is(err, ErrCorrupt) {
		return false
	}
	return errors.Is(err, ErrNotFound) || (c.opts.errNotFound != nil && errors.Is(err, c.opts.errNotFound))
}
func (c *Cache) notFound() error {
	if c.opts.errNotFound != nil {
		return c.opts.errNotFound
	}
	return ErrNotFound
}
func (c *Cache) report(err error) {
	if err != nil && c.opts.errorHandler != nil {
		c.opts.errorHandler(err)
	}
}

// CacheType reports which levels are configured | CacheType 报告配置了哪些层级。
func (c *Cache) CacheType() string {
	if c.local != nil && c.remote != nil {
		return TypeBoth
	}
	if c.remote != nil {
		return TypeRemote
	}
	return TypeLocal
}

func (c *Cache) decodeResult(key string, data []byte, dst any) error {
	err := c.decode(data, dst)
	if err != nil && !c.IsNotFound(err) {
		return &OpError{"decode", key, err}
	}
	return err
}

// Namespace and SourceID identify this coordinator when filtering sync events.
// Namespace 与 SourceID 在过滤同步事件时标识本协调器。
func (c *Cache) Namespace() string { return c.opts.namespace }

// SourceID identifies this coordinator instance | SourceID 标识本协调器实例。
func (c *Cache) SourceID() string { return c.opts.sourceID }
