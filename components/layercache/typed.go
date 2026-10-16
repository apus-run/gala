package layercache

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Typed is a thin value adapter; all types share the same serialized Cache.
// Use distinct namespaces/key domains for incompatible schemas.
type Typed[V any] struct{ *Cache }

func NewTyped[V any](c *Cache) *Typed[V] {
	if c == nil {
		panic("layercache: nil cache")
	}
	return &Typed[V]{c}
}
func (c *Typed[V]) Get(ctx context.Context, key string) (V, error) {
	var v V
	err := c.Cache.Get(ctx, key, &v)
	return v, err
}
func (c *Typed[V]) GetSkippingLocal(ctx context.Context, key string) (V, error) {
	var v V
	err := c.Cache.GetSkippingLocal(ctx, key, &v)
	return v, err
}

// Explicit typed arguments take precedence over Value/Do item options. Clip
// prevents append from modifying a caller-owned backing array with spare capacity.
func (c *Typed[V]) Set(ctx context.Context, key string, value V, opts ...ItemOption) error {
	return c.Cache.Set(ctx, key, append(slices.Clip(opts), Value(value), Do(nil))...)
}
func (c *Typed[V]) GetOrLoad(ctx context.Context, key string, load Loader[V], opts ...ItemOption) (V, error) {
	var v V
	if load == nil {
		return v, ErrInvalidOptions
	}
	options := append(slices.Clip(opts), Value(&v), Do(func(task context.Context) (any, error) { return load(task, key) }))
	err := c.Once(ctx, key, options...)
	return v, err
}
func (c *Typed[V]) MGet(ctx context.Context, keys []string, load BatchLoader[V]) (map[string]V, error) {
	done, err := c.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	var do func(context.Context, []string) (map[string]any, error)
	if load != nil {
		do = func(task context.Context, missing []string) (map[string]any, error) {
			v, err := load(task, missing)
			out := make(map[string]any, len(missing))
			for _, key := range missing {
				if stop := c.contextError(task); stop != nil {
					return out, errors.Join(err, stop)
				}
				if value, ok := v[key]; ok {
					out[key] = value
				}
			}
			return out, err
		}
	}
	raw, errs := c.mget(ctx, keys, do)
	out := make(map[string]V, len(raw))
	for key, data := range raw {
		if err := c.contextError(ctx); err != nil {
			return out, errors.Join(errs, err)
		}
		var v V
		if err := c.decode(data, &v); err != nil {
			errs = errors.Join(errs, &OpError{"decode", c.prefix + key, err})
		} else {
			out[key] = v
		}
	}
	return out, errs
}

// T mirrors JetCache's ordered-ID facade. Prefix/id concatenation is explicit;
// applications must choose non-colliding key domains and separators. Raw-key
// operations and coordinator lifecycle are available explicitly through Typed.
type T[K cmp.Ordered, V any] struct{ Typed *Typed[V] }

func NewT[K cmp.Ordered, V any](c *Cache) *T[K, V] { return &T[K, V]{NewTyped[V](c)} }
func (c *T[K, V]) key(prefix string, id K) string {
	return prefix + c.Typed.opts.separator + fmt.Sprint(id)
}
func (c *T[K, V]) Set(ctx context.Context, prefix string, id K, v V, opts ...ItemOption) error {
	return c.Typed.Set(ctx, c.key(prefix, id), v, opts...)
}
func (c *T[K, V]) Get(ctx context.Context, prefix string, id K, load func(context.Context, K) (V, error)) (V, error) {
	if load == nil {
		return c.Typed.Get(ctx, c.key(prefix, id))
	}
	return c.GetOrLoad(ctx, prefix, id, load)
}
func (c *T[K, V]) GetOrLoad(ctx context.Context, prefix string, id K, load func(context.Context, K) (V, error), opts ...ItemOption) (V, error) {
	var fn Loader[V]
	if load != nil {
		fn = func(task context.Context, _ string) (V, error) { return load(task, id) }
	}
	return c.Typed.GetOrLoad(ctx, c.key(prefix, id), fn, opts...)
}
func (c *T[K, V]) GetSkippingLocal(ctx context.Context, prefix string, id K) (V, error) {
	return c.Typed.GetSkippingLocal(ctx, c.key(prefix, id))
}
func (c *T[K, V]) InvalidateLocal(ctx context.Context, prefix string, id K) error {
	return c.Typed.InvalidateLocal(ctx, c.key(prefix, id))
}
func (c *T[K, V]) Delete(ctx context.Context, prefix string, id K) error {
	return c.Typed.Delete(ctx, c.key(prefix, id))
}
func (c *T[K, V]) Exists(ctx context.Context, prefix string, id K) (bool, error) {
	return c.Typed.Exists(ctx, c.key(prefix, id))
}

// MGet returns partial successes with errors instead of swallowing batch failures.
func (c *T[K, V]) MGet(ctx context.Context, prefix string, ids []K, load func(context.Context, []K) (map[K]V, error)) (map[K]V, error) {
	reverse := make(map[string]K, len(ids))
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		k := c.key(prefix, id)
		if _, ok := reverse[k]; !ok {
			keys = append(keys, k)
		}
		reverse[k] = id
	}
	var fn BatchLoader[V]
	if load != nil {
		fn = func(task context.Context, missing []string) (map[string]V, error) {
			m := make([]K, 0, len(missing))
			for _, k := range missing {
				m = append(m, reverse[k])
			}
			slices.Sort(m)
			values, err := load(task, m)
			out := make(map[string]V, len(values))
			for _, k := range missing {
				if v, ok := values[reverse[k]]; ok {
					out[k] = v
				}
			}
			return out, err
		}
	}
	values, err := c.Typed.MGet(ctx, keys, fn)
	out := make(map[K]V, len(values))
	for key, v := range values {
		out[reverse[key]] = v
	}
	return out, err
}

// mget is called inside Typed.MGet's lifetime registration, including decoding.
func (c *Cache) mget(ctx context.Context, keys []string, load func(context.Context, []string) (map[string]any, error)) (map[string][]byte, error) {
	result := make(map[string][]byte)
	keys = slices.Clone(keys)
	slices.Sort(keys)
	keys = slices.Compact(keys)
	var ts []*ticket
	defer func() {
		for _, t := range ts {
			c.release(t)
		}
	}()
	var errs error
	for _, key := range keys {
		if err := c.contextError(ctx); err != nil {
			return result, err
		}
		if e, ok := c.getLocal(c.prefix + key); ok {
			kind, _, err := envelope(e.Data)
			if err != nil {
				errs = errors.Join(errs, &OpError{"get", c.prefix + key, err})
			} else if kind != 1 {
				result[key] = e.Data
			}
			continue
		}
		ts = append(ts, c.retain(c.prefix+key))
	}

	if len(ts) == 0 {
		return result, errs
	}
	// Sorting the missing set produces a canonical, collision-free flight key.
	// Cache-only requests must not join a source-loading request (or vice versa).
	kind := "batch/cache"
	if load != nil {
		kind = "batch/load"
	}
	f, err := c.startTask(ctx, kind, ts, func(task context.Context) (any, error) { return c.loadBatch(task, ts, load) }, nil)
	if err != nil {
		return result, errors.Join(errs, err)
	}
	v, err := c.wait(ctx, f)
	if v != nil {
		for k, b := range v.(map[string][]byte) {
			result[k] = b
		}
	}
	return result, errors.Join(errs, err)
}
func (c *Cache) loadBatch(ctx context.Context, ts []*ticket, load func(context.Context, []string) (map[string]any, error)) (map[string][]byte, error) {
	result := make(map[string][]byte)
	var pending []*ticket
	var errs error
	accept := func(t *ticket, e Entry, err error) {
		if err != nil {
			errs = errors.Join(errs, err)
			return
		}
		kind, _, err := envelope(e.Data)
		if err != nil {
			errs = errors.Join(errs, &OpError{"get", t.key, err})
		} else if kind != 1 {
			result[strings.TrimPrefix(t.key, c.prefix)] = e.Data
		}
	}
	for _, t := range ts {
		e, hit, err := c.admitRead(ctx, t, false)
		if err != nil {
			return result, err
		}
		if hit {
			accept(t, e, nil)
		} else {
			pending = append(pending, t)
		}
	}
	var missing []*ticket
	remote, batched := c.remote.(BatchRemote)
	var values map[string]Entry
	var batchErr error
	if batched && len(pending) > 0 {
		keys := make([]string, len(pending))
		for n, t := range pending {
			keys[n] = t.key
		}
		op, cancel := context.WithTimeout(ctx, c.opts.remoteTimeout)
		values, batchErr = remote.MGet(op, keys)
		if e := c.contextError(op); e != nil {
			batchErr = e
			values = nil
		}
		cancel()
		errs = errors.Join(errs, batchErr)
	}
	for _, t := range pending {
		e, found := values[t.key]
		var err error
		if !batched || !found && batchErr != nil {
			// No batch capability, or an aggregate error left this key ambiguous.
			// Reuse the single-key path to distinguish a miss from a failure.
			e, err = c.readCache(ctx, t, false)
		} else {
			if !found {
				err = ErrMiss
			}
			e, err = c.acceptRemote(ctx, t, e, err, false)
		}
		if errors.Is(err, ErrMiss) && !errors.Is(err, ErrCorrupt) {
			missing = append(missing, t)
		} else {
			accept(t, e, err)
		}
	}
	if len(missing) == 0 || load == nil {
		return result, errs
	}
	if err := c.contextError(ctx); err != nil {
		return result, errors.Join(errs, err)
	}
	keys := make([]string, len(missing))
	for n, t := range missing {
		keys[n] = strings.TrimPrefix(t.key, c.prefix)
	}
	c.incr(statLoad)
	sourceValues, sourceErr := load(ctx, keys)
	if sourceErr != nil {
		c.incr(statLoadError)
		errs = errors.Join(errs, sourceErr)
	}
	writes := make(map[string][]byte, len(missing))
	for _, t := range missing {
		if err := c.contextError(ctx); err != nil {
			return result, errors.Join(errs, err)
		}
		key := strings.TrimPrefix(t.key, c.prefix)
		value, found := sourceValues[key]
		if !found && (sourceErr != nil || c.opts.notFoundTTL == 0) {
			continue
		}
		data, err := c.encode(value, !found)
		if err != nil {
			errs = errors.Join(errs, &OpError{"encode", c.prefix + key, err})
			continue
		}
		writes[t.key] = data
		if found {
			result[key] = data
		}
	}
	if err := c.commitBatch(ctx, missing, writes); err != nil {
		if !errors.Is(err, ErrInvalidated) && !errors.Is(err, ErrClosed) && ctx.Err() == nil {
			c.report(err)
		}
		errs = errors.Join(errs, err)
	}
	return result, errs
}
func (c *Cache) commitBatch(ctx context.Context, ts []*ticket, writes map[string][]byte) error {
	var locked []*ticket
	defer func() {
		for n := len(locked) - 1; n >= 0; n-- {
			unlockKey(locked[n])
		}
	}()
	for _, t := range ts {
		if _, ok := writes[t.key]; !ok {
			continue
		}
		if err := c.lockKey(ctx, t); err != nil {
			return err
		}
		locked = append(locked, t)
		if err := c.checkGeneration(t); err != nil {
			return err
		}
	}
	for _, t := range locked {
		c.invalidate(t, false)
		c.removeLocal(t.key)
	}
	var errs error
	var successful []string
	// A later group can fail after earlier writes committed. Notify those
	// successes on every exit, even when the request's context has expired.
	defer func() {
		if len(successful) > 0 {
			slices.Sort(successful)
			c.send(EventBatch, successful...)
		}
	}()
	for _, negative := range []bool{false, true} {
		if err := c.contextError(ctx); err != nil {
			return errors.Join(errs, err)
		}
		group := make(map[string][]byte)
		for k, b := range writes {
			if isNegative(b) == negative {
				group[k] = b
			}
		}
		if len(group) == 0 {
			continue
		}
		i := ItemOptions{ttl: c.defaultTTL()}
		if remote, ok := c.remote.(BatchRemote); ok {
			// One TTL per class, as in JetCache's positive/negative MSet partition.
			ttl := c.ttl(i.ttl, negative)
			limit := time.Now().Add(ttl)
			op, cancel := context.WithTimeout(ctx, c.opts.remoteTimeout)
			receipts, err := remote.MSet(op, group, ttl)
			// Receipts acknowledge writes even when their reply arrives after the
			// operation budget. Notify peers, while withholding late L1 fills.
			for key, deadline := range receipts {
				if _, requested := group[key]; requested && !deadline.IsZero() {
					successful = append(successful, strings.TrimPrefix(key, c.prefix))
				}
			}
			if x := c.contextError(op); x != nil {
				err = x
				receipts = nil
			}
			cancel()
			errs = errors.Join(errs, err)
			for key, data := range group {
				if err := c.contextError(ctx); err != nil {
					return errors.Join(errs, err)
				}
				deadline, ok := receipts[key]
				if !ok || deadline.IsZero() {
					if err == nil {
						errs = errors.Join(errs, &OpError{"set", key, ErrInvalidTTL})
					}
					continue
				}
				if deadline.After(limit) {
					deadline = limit
				}
				c.setLocal(key, Entry{Data: data}, deadline)
			}
		} else {
			for key, data := range group {
				if err := c.contextError(ctx); err != nil {
					return errors.Join(errs, err)
				}
				committed, err := c.writeEntry(ctx, key, data, i)
				if committed {
					successful = append(successful, strings.TrimPrefix(key, c.prefix))
				}
				errs = errors.Join(errs, err)
			}
		}
	}
	return errs
}
