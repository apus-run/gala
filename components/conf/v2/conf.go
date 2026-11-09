// Package conf loads an ordered set of configuration sources into a typed
// struct, and reloads atomically: a batch is either fully published or
// discarded, and the previously published value is kept on failure.
//
// Package conf 按声明顺序把多个来源合并为强类型结构体，并原子重载：
// 一批要么整体发布，要么整体丢弃并保留上一份有效配置。
package conf

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
)

// Source produces one layer of configuration as a nested map. Later sources
// in Open override earlier ones. Returned maps and slices are copied without
// mutation; sources must not modify them concurrently while they are copied.
// Values must be scalars or acyclic string-keyed maps, slices and arrays, with
// at most 128 collection edges from the root. Struct values and pointers are
// rejected; represent objects as nested maps for binding to business structs.
// Owned copies normalize maps to map[string]any and sequences to []any.
// Source methods must not reenter this Config's loading or lifecycle methods.
//
// Source 产出一层嵌套 map 形式的配置。Open 中靠后的来源覆盖靠前的。
// 本包复制且不修改来源数据；复制期间来源不得并发改写返回的 map/slice。
type Source interface {
	Load(context.Context) (map[string]any, error)
}

// Config holds the currently published value of T. It is safe for concurrent
// use; Current never blocks on I/O. Create it with Open and do not copy it.
//
// Config 持有 T 当前已发布的值。并发安全；Current 不做任何 I/O。
type Config[T any] struct {
	sources        []Source
	layers         []map[string]any
	opts           options
	session        *watchSession
	closed         atomic.Bool
	closeDone      chan struct{}
	lifetime       context.Context
	cancelLifetime context.CancelFunc
	closeErr       error
	subMu          sync.Mutex
	subscribers    map[chan Event[T]]struct{}
	cur            atomic.Pointer[T]
	// sem is a capacity-1 semaphore so that a waiting Reload can be
	// cancelled, which a sync.Mutex cannot do.
	// sem 是容量为 1 的信号量，使等待中的 Reload 可被取消（Mutex 做不到）。
	sem chan struct{}
}

// Open loads all sources once and returns a usable Config, or an error and a
// nil Config. Background monitoring starts only with WithWatch or Start.
//
// Open 同步加载一次所有来源。成功返回可用的 Config，失败返回 nil 与错误。
// 仅在 WithWatch 或 Start 时启动后台监听。
func Open[T any](ctx context.Context, opts ...Option) (*Config[T], error) {
	if err := checkType[T](); err != nil {
		return nil, err
	}
	var o options
	for _, opt := range opts {
		if opt == nil {
			return nil, newConfigError(-1, "option must not be nil")
		}
		opt(&o)
	}
	if o.poll < 0 {
		return nil, newConfigError(-1, "poll interval must not be negative")
	}
	if err := checkSources(o.sources); err != nil {
		return nil, err
	}
	lifetime, cancelLifetime := context.WithCancel(context.Background())
	c := &Config[T]{
		lifetime:       lifetime,
		cancelLifetime: cancelLifetime,
		sources:        slices.Clone(o.sources),
		opts:           o,
		sem:            make(chan struct{}, 1),
		closeDone:      make(chan struct{}),
		subscribers:    make(map[chan Event[T]]struct{}),
	}
	if err := c.Reload(ctx); err != nil {
		c.Close()
		return nil, err
	}
	if o.watch {
		if err := c.Start(ctx); err != nil {
			c.Close()
			return nil, err
		}
	}
	return c, nil
}

func checkSources(sources []Source) error {
	if len(sources) == 0 {
		return newConfigError(-1, "at least one source is required")
	}
	for i, s := range sources {
		if isNilSource(s) {
			return newConfigError(i, "source must not be nil")
		}
	}
	return nil
}

// Current returns the published value. The result is read-only: callers
// must not modify it or any map, slice or pointer reachable from it.
//
// Current 返回已发布的值。结果只读：调用方不得修改它或其可达的
// map、slice、指针。
func (c *Config[T]) Current() *T {
	return c.cur.Load()
}

// Reload rebuilds the value from all sources and publishes it only if every
// step succeeds. Concurrent calls queue; a call cancelled while waiting
// returns without reading any source.
//
// Reload 从所有来源重建值，且仅在每一步都成功时发布。并发调用排队；
// 等待中被取消的调用直接返回，不读取来源。
func (c *Config[T]) Reload(ctx context.Context) error {
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	if err := c.acquire(ctx); err != nil {
		return err
	}
	defer c.release()
	layers, err := readSources(ctx, c.sources, 0)
	if err != nil {
		return err
	}
	return c.publish(ctx, c.sources, layers)
}

// Load appends higher-priority sources to the last successful source snapshots.
// Existing sources are not read again; Reload refreshes all registered sources.
// Failure changes neither the source list nor the published snapshot.
func (c *Config[T]) Load(ctx context.Context, sources ...Source) error {
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	sources = slices.Clone(sources)
	if err := checkSources(sources); err != nil {
		return err
	}
	if err := c.acquire(ctx); err != nil {
		return err
	}
	defer c.release()
	var pending []sourceWatcher
	var cancelPending context.CancelFunc
	if c.session != nil && c.session.ctx.Err() == nil {
		var err error
		watchCtx, cancel := context.WithCancel(c.session.ctx)
		cancelPending = cancel
		// Only watcher setup belongs to this call. After setup, rollback owns
		// cancellation until the session takes ownership on successful commit.
		stop := context.AfterFunc(ctx, cancel)
		pending, err = openWatchers(watchCtx, sources, len(c.sources))
		stop()
		if err != nil {
			cancelPending()
			return err
		}
	}
	committed := false
	defer func() {
		if !committed {
			if cancelPending != nil {
				cancelPending()
			}
			closeWatchers(pending)
		}
	}()
	added, err := readSources(ctx, sources, len(c.sources))
	if err != nil {
		return err
	}
	allSources := slices.Concat(c.sources, sources)
	allLayers := slices.Concat(c.layers, added)
	if err := c.publish(ctx, allSources, allLayers); err != nil {
		return err
	}
	if len(pending) > 0 {
		c.session.startWorkers(pending)
	} else if cancelPending != nil {
		cancelPending()
	}
	committed = true
	return nil
}

// Tie in-flight work to Close as well as the caller's cancellation.
func (c *Config[T]) operationContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	if c.lifetime == nil {
		return ctx, cancel
	}
	stop := context.AfterFunc(c.lifetime, cancel)
	if c.lifetime.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}

func (c *Config[T]) acquire(ctx context.Context) error {
	if c.sem == nil {
		return newConfigError(-1, "Config must be created with Open")
	}
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return newError(StageCanceled, -1, "", "", ctx.Err())
	}
	if c.closed.Load() {
		c.release()
		return newConfigError(-1, "Config is closed")
	}
	if err := ctx.Err(); err != nil {
		c.release()
		return newError(StageCanceled, -1, "", "", err)
	}
	return nil
}
func (c *Config[T]) release() { <-c.sem }

func readSources(ctx context.Context, sources []Source, offset int) ([]map[string]any, error) {
	layers := make([]map[string]any, 0, len(sources))
	for i, s := range sources {
		if err := ctx.Err(); err != nil {
			return nil, newError(StageCanceled, offset+i, "", "", err)
		}
		layer, err := s.Load(ctx)
		if cause := ctx.Err(); cause != nil {
			return nil, newError(StageCanceled, offset+i, "", "", cause)
		}
		if err != nil {
			return nil, wrapSourceErr(offset+i, err)
		}
		owned := map[string]any{}
		if err := mergeInto(owned, layer); err != nil {
			return nil, newMergeError(offset+i, err)
		}
		layers = append(layers, owned)
	}
	return layers, nil
}

func (c *Config[T]) publish(ctx context.Context, sources []Source, layers []map[string]any) error {
	merged := map[string]any{}
	for i, layer := range layers {
		if c.opts.merge == nil {
			if err := mergeInto(merged, layer); err != nil {
				return newMergeError(i, err)
			}
		} else {
			owned := map[string]any{}
			if err := mergeInto(owned, layer); err != nil {
				return newMergeError(i, err)
			}
			err := c.opts.merge(merged, owned)
			if cause := ctx.Err(); cause != nil {
				return newError(StageCanceled, -1, "", "", cause)
			}
			if err != nil {
				return &Error{Stage: StageMerge, Source: i, reason: "custom merge failed"}
			}
		}
	}
	if c.opts.resolve != nil {
		err := c.opts.resolve(ctx, merged)
		if cause := ctx.Err(); cause != nil {
			return newError(StageCanceled, -1, "", "", cause)
		}
		if err != nil {
			return &Error{Stage: StageResolve, Source: -1, reason: "resolver failed"}
		}
	}
	if c.opts.merge != nil || c.opts.resolve != nil {
		owned := map[string]any{}
		if err := mergeInto(owned, merged); err != nil {
			return newMergeError(-1, err)
		}
		merged = owned
	}
	var next T
	if err := bind(merged, &next); err != nil {
		return err
	}
	if v, ok := any(&next).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return newValidateError(err)
		}
	}
	if err := ctx.Err(); err != nil {
		return newError(StageCanceled, -1, "", "", err)
	}
	previous := c.cur.Swap(&next)
	c.sources = sources
	c.layers = layers
	if previous != nil && !reflect.DeepEqual(previous, &next) {
		c.broadcast(Event[T]{Previous: previous, Current: &next})
	}
	return nil
}

// checkType rejects T that is not a struct value type.
// checkType 拒绝非结构体值类型的 T。
func checkType[T any]() error {
	t := reflect.TypeFor[T]()
	if t.Kind() != reflect.Struct {
		return newConfigError(-1, "T must be a struct value type")
	}
	return nil
}

func isNilSource(s any) bool {
	if s == nil {
		return true
	}
	v := reflect.ValueOf(s)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}
