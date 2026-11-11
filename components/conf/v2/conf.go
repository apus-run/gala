// Package conf loads ordered configuration layers into typed, read-only
// snapshots. Reloads are serialized and publish only after full validation.
package conf

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
)

// Source provides one layer per load. Later sources have higher priority.
// Load must honor ctx and return data that remains immutable while copied.
// Its error text may be logged and must not include configuration values.
// Load runs in a write transaction. It must not synchronously call a write
// method (Reload, Load, Set, Delete or MergeAt) on the same Config.
type Source interface {
	Load(context.Context) (map[string]any, error)
}

// SourceFunc adapts a function to Source.
type SourceFunc func(context.Context) (map[string]any, error)

// Load calls f with ctx.
func (f SourceFunc) Load(ctx context.Context) (map[string]any, error) { return f(ctx) }

// Config holds read-only snapshots. Create it with Open and do not copy it.
type Config[T any] struct {
	defaults    T
	sources     []Source
	sem         chan struct{}
	cur         atomic.Pointer[Snapshot[T]]
	options     Options
	unresolved  map[string]any // writer-owned input to the resolver for transient edits
	observersMu sync.Mutex
	observers   map[*subscription]struct{}
}

// Open validates the struct type and synchronously publishes the first snapshot.
// Missing fields retain defaults. Maps, slices and pointers in defaults may be
// shared by snapshots and must remain read-only after Open. A Validate method
// runs during publication and must not reenter the same Config's write methods.
func Open[T any](ctx context.Context, defaults T, sources ...Source) (*Config[T], error) {
	return OpenWith(ctx, defaults, Options{}, sources...)
}

// OpenTree opens a configuration without a predeclared schema. Its empty
// form can be populated with Set or Load.
func OpenTree(ctx context.Context, sources ...Source) (*Config[Tree], error) {
	return Open(ctx, Tree{}, sources...)
}

// Options selects merge and resolution behavior for an instance. Nil functions
// select recursive object overlay and literal values. Callbacks operate on
// private candidates and must not mutate them concurrently or after return.
// Both hooks run inside a write transaction. Follow-up writes to this Config
// belong in a subscriber after publication, outside the hook.
type Options struct {
	Merge   func(dst, src map[string]any) error
	Resolve func(map[string]any) error
}

// OpenWith is Open with explicit merge and resolver functions. Use Tree{} for
// schema-free configuration. Source and callback errors must be safe to display.
func OpenWith[T any](ctx context.Context, defaults T, options Options, sources ...Source) (*Config[T], error) {
	if reflect.TypeFor[T]() != reflect.TypeFor[Tree]() {
		if err := validateTarget(reflect.TypeFor[T]()); err != nil {
			return nil, err
		}
		if len(sources) == 0 {
			return nil, &Error{Stage: StageConfig, Source: -1, Err: errors.New("at least one source is required")}
		}
	}
	for i, s := range sources {
		if isNilSource(s) {
			return nil, &Error{Stage: StageConfig, Source: i, Err: errors.New("source must not be nil")}
		}
	}
	c := &Config[T]{defaults: defaults, options: options, sources: slices.Clone(sources), sem: make(chan struct{}, 1)}
	if err := c.Reload(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// Current returns a complete read-only snapshot or nil for a zero Config.
func (c *Config[T]) Current() *T {
	if s := c.cur.Load(); s != nil {
		return s.Value
	}
	return nil
}

// Snapshot pins both read views with one atomic load.
func (c *Config[T]) Snapshot() Snapshot[T] {
	if s := c.cur.Load(); s != nil {
		return *s
	}
	return Snapshot[T]{}
}

// Reload rebuilds from defaults and all sources. A failed call never publishes.
// Concurrent callers queue and can cancel their wait without reading sources.
func (c *Config[T]) Reload(ctx context.Context) error {
	return c.reload(ctx, nil)
}

// Load appends higher-priority sources and reloads the entire source list.
// Failure preserves both the old list and the published snapshot.
func (c *Config[T]) Load(ctx context.Context, sources ...Source) error {
	return c.reload(ctx, sources)
}

func (c *Config[T]) acquire(ctx context.Context) error {
	if c.sem == nil {
		return &Error{Stage: StageConfig, Source: -1, Err: errors.New("Config must be created with Open")}
	}
	select {
	case c.sem <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-c.sem
			return &Error{Stage: StageCanceled, Source: -1, Err: err}
		}
		return nil
	case <-ctx.Done():
		return &Error{Stage: StageCanceled, Source: -1, Err: ctx.Err()}
	}
}

func (c *Config[T]) reload(ctx context.Context, extra []Source) error {
	if err := c.acquire(ctx); err != nil {
		return err
	}
	defer func() { <-c.sem }()
	sources := c.sources
	if len(extra) > 0 {
		for i, s := range extra {
			if isNilSource(s) {
				return &Error{Stage: StageConfig, Source: len(sources) + i, Err: errors.New("source must not be nil")}
			}
		}
		sources = slices.Concat(sources, extra)
	}
	layers := layerMerger{merge: c.options.Merge}
	if base, ok := any(c.defaults).(Tree); ok && base.root != nil {
		if err := layers.add(base.root); err != nil {
			return &Error{Stage: StageMerge, Source: -1, Err: err}
		}
	}
	for i, src := range sources {
		if err := ctx.Err(); err != nil {
			return &Error{Stage: StageCanceled, Source: i, Err: err}
		}
		layer, err := src.Load(ctx)
		if cause := ctx.Err(); cause != nil {
			return &Error{Stage: StageCanceled, Source: i, Err: cause}
		}
		if err != nil {
			switch {
			case errors.Is(err, context.Canceled):
				return &Error{Stage: StageCanceled, Source: i, Err: context.Canceled}
			case errors.Is(err, context.DeadlineExceeded):
				return &Error{Stage: StageCanceled, Source: i, Err: context.DeadlineExceeded}
			default:
				return &Error{Stage: StageLoad, Source: i, Err: err}
			}
		}
		if err := layers.add(layer); err != nil {
			return &Error{Stage: StageMerge, Source: i, Err: err}
		}
	}
	raw := layers.values
	if c.options.Merge != nil && raw != nil {
		owned, err := new(layerMerger).copyValue(raw)
		if err != nil {
			return &Error{Stage: StageMerge, Source: -1, Err: err}
		}
		raw = owned.(map[string]any)
	}
	if err := c.publish(ctx, raw); err != nil {
		return err
	}
	c.sources = sources
	return nil
}

func (c *Config[T]) publish(ctx context.Context, raw map[string]any) error {
	if raw == nil {
		raw = map[string]any{}
	}
	tree := Tree{root: raw}
	if resolve := c.options.Resolve; resolve != nil {
		work := copyTreeValue(raw).(map[string]any)
		if err := resolve(work); err != nil {
			return &Error{Stage: StageResolve, Source: -1, Err: err}
		}
		owned, err := new(layerMerger).copyValue(work)
		if err != nil {
			return &Error{Stage: StageResolve, Source: -1, Err: err}
		}
		tree.root = owned.(map[string]any)
	}
	next := c.defaults
	if dynamic, ok := any(&next).(*Tree); ok {
		*dynamic = tree
	} else if err := bindValue(tree.root, reflect.ValueOf(&next).Elem(), fieldPath{}); err != nil {
		return err
	}
	var validation error
	if v, ok := any(&next).(interface{ Validate() error }); ok {
		validation = v.Validate()
	}
	if err := ctx.Err(); err != nil {
		return &Error{Stage: StageCanceled, Source: -1, Err: err}
	}
	if validation != nil {
		return &Error{Stage: StageValidate, Source: -1, Err: validation}
	}
	c.observersMu.Lock()
	defer c.observersMu.Unlock()
	if err := ctx.Err(); err != nil {
		return &Error{Stage: StageCanceled, Source: -1, Err: err}
	}
	prev := c.cur.Swap(&Snapshot[T]{Tree: tree, Value: &next})
	c.unresolved = raw
	if prev != nil {
		for sub := range c.observers {
			sub.notify(prev.Tree, tree)
		}
	}
	return nil
}

func isNilSource(s Source) bool {
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

var (
	errNull       = errors.New("explicit null is not allowed")
	errSourceType = errors.New("unsupported source value; use scalars and collections")
	errCycle      = errors.New("cyclic source values are not allowed")
	errDepth      = errors.New("source nesting exceeds 128 levels")
	errNodeLimit  = errors.New("expanded node limit exceeded")
)

// visit tracks only ancestors, so shared but acyclic collections remain valid.
type visit struct {
	typ     reflect.Type
	pointer uintptr
	length  int
}

const maxExpandedNodes = 65_536

// layerMerger owns the candidate tree and counts expanded nodes across sources.
type layerMerger struct {
	merge  func(map[string]any, map[string]any) error
	values map[string]any
	nodes  int
}

// Copy and validate the whole layer before applying any of its overrides.
func (m *layerMerger) add(layer map[string]any) error {
	if layer == nil {
		return nil
	}
	value, err := m.copyValue(layer)
	if err != nil {
		return err
	}
	owned := value.(map[string]any)
	if m.merge != nil {
		if m.values == nil {
			m.values = map[string]any{}
		}
		return m.merge(m.values, owned)
	}
	if len(m.values) == 0 {
		m.values = owned
	} else {
		mergeCopied(m.values, owned)
	}
	return nil
}

// mergeCopied consumes an already validated, privately owned layer.
func mergeCopied(dst, layer map[string]any) {
	for k, value := range layer {
		next, nextIsMap := value.(map[string]any)
		previous, previousIsMap := dst[k].(map[string]any)
		if nextIsMap && previousIsMap {
			mergeCopied(previous, next)
		} else {
			dst[k] = value
		}
	}
}

func (m *layerMerger) reserve(n int) error {
	if n > maxExpandedNodes-m.nodes {
		return errNodeLimit
	}
	m.nodes += n
	return nil
}

// Copy source collections into the map and slice types consumed by the binder.
func (m *layerMerger) copyValue(value any) (any, error) {
	if err := m.reserve(1); err != nil {
		return nil, err
	}
	return m.copyNested(reflect.ValueOf(value), make(map[visit]bool), 0)
}

func (m *layerMerger) copyNested(v reflect.Value, active map[visit]bool, depth int) (any, error) {
	if depth > 128 {
		return nil, errDepth
	}

	for v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	if !v.IsValid() {
		return nil, errNull
	}
	if v.Kind() == reflect.Map || v.Kind() == reflect.Slice {
		if v.IsNil() {
			return nil, errNull
		}
		key := visit{typ: v.Type(), pointer: v.Pointer(), length: v.Len()}
		if active[key] {
			return nil, errCycle
		}
		active[key] = true
		defer delete(active, key)
	}
	switch v.Kind() {
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return nil, errSourceType
		}
		if err := m.reserve(v.Len()); err != nil {
			return nil, err
		}
		out := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			value, err := m.copyNested(iter.Value(), active, depth+1)
			if err != nil {
				return nil, err
			}
			out[iter.Key().String()] = value
		}
		return out, nil
	case reflect.Slice, reflect.Array:
		if err := m.reserve(v.Len()); err != nil {
			return nil, err
		}
		out := make([]any, v.Len())
		for i := range v.Len() {
			value, err := m.copyNested(v.Index(i), active, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = value
		}
		return out, nil
	case reflect.Struct, reflect.Pointer, reflect.Chan, reflect.Func, reflect.UnsafePointer, reflect.Uintptr, reflect.Complex64, reflect.Complex128:
		return nil, errSourceType
	}
	return v.Interface(), nil
}
