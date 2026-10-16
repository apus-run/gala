// Package layercache implements the JetCache byte-cache architecture: Local →
// Remote → origin, shared slow paths, typed views, batch misses and maintenance.
// It fences same-instance stale fills; it does not provide database consistency.
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
func (c *Cache) Get(ctx context.Context, key string, dst any) error {
	return c.get(ctx, key, dst, false)
}
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
func (c *Cache) Exists(ctx context.Context, key string) (bool, error) {
	err := c.Get(ctx, key, nil)
	if errors.Is(err, ErrMiss) || c.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// Set uses Value(v) or Do(fn), with optional TTL/NX/XX/SkipLocal/Refresh controls.
// A rejected condition returns ErrNotStored and never publishes the candidate L1.
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

func (c *Cache) Delete(ctx context.Context, key string) error { return c.invalidateKey(ctx, key, true) }
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
func (c *Cache) Namespace() string { return c.opts.namespace }
func (c *Cache) SourceID() string  { return c.opts.sourceID }
