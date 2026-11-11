package conf

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
)

// Tree is a read-only view of the merged, resolved configuration. It contains
// explicit keys, including zero values. Struct defaults belong to the typed
// snapshot. A zero Tree is an empty object. Paths are literal segments;
// no segments selects the root, and decimal indices select list elements.
type Tree struct{ root map[string]any }

// ErrNotFound reports a missing key, invalid list index or non-container parent.
var ErrNotFound = errors.New("configuration path not found")

func (t Tree) find(path []string) (any, bool) {
	var value any = t.root
	for _, key := range path {
		switch node := value.(type) {
		case map[string]any:
			next, ok := node[key]
			if !ok {
				return nil, false
			}
			value = next
		case []any:
			i, ok := listIndex(key, len(node))
			if !ok {
				return nil, false
			}
			value = node[i]
		default:
			return nil, false
		}
	}
	return value, true
}

func listIndex(key string, length int) (int, bool) {
	i, err := strconv.Atoi(key)
	return i, err == nil && i >= 0 && i < length && strconv.Itoa(i) == key
}

// Lookup returns a private copy of the value. The boolean distinguishes a
// missing path from an explicitly configured zero, false or empty string.
func (t Tree) Lookup(path ...string) (any, bool) {
	v, ok := t.find(path)
	if !ok {
		return nil, false
	}
	return copyTreeValue(v), true
}

// Exists reports whether the path is present without copying its value.
func (t Tree) Exists(path ...string) bool { _, ok := t.find(path); return ok }

// Keys returns the sorted immediate keys of an object, or nil for other paths.
func (t Tree) Keys(path ...string) []string {
	v, _ := t.find(path)
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return slices.Sorted(maps.Keys(m))
}

// Cut returns a pinned subtree without copying it.
func (t Tree) Cut(path ...string) (Tree, error) {
	v, ok := t.find(path)
	if !ok {
		return Tree{}, ErrNotFound
	}
	m, ok := v.(map[string]any)
	if !ok {
		return Tree{}, errTypeMismatch
	}
	return Tree{root: m}, nil
}

// Decode binds a subtree into a non-nil pointer with the same conversion
// rules as Open. The destination is replaced only after successful binding.
func (t Tree) Decode(dst any, path ...string) error {
	v, ok := t.find(path)
	if !ok {
		return ErrNotFound
	}
	ptr := reflect.ValueOf(dst)
	if !ptr.IsValid() || ptr.Kind() != reflect.Pointer || ptr.IsNil() {
		return &Error{Stage: StageConfig, Source: -1, Err: errors.New("destination must be a non-nil pointer")}
	}
	typ := ptr.Type().Elem()
	if err := inspectType(typ, fieldPath{}, make(map[reflect.Type]bool), true); err != nil {
		return err
	}
	next := reflect.New(typ).Elem()
	if err := bindValue(v, next, fieldPath{}); err != nil {
		return err
	}
	ptr.Elem().Set(next)
	return nil
}

// Get returns a typed scalar, list, map or struct from a pinned tree.
func Get[T any](tree Tree, path ...string) (T, error) {
	var out T
	err := tree.Decode(&out, path...)
	return out, err
}

// Marshal passes an independent object to a caller-supplied encoder.
func (t Tree) Marshal(encode func(any) ([]byte, error)) ([]byte, error) {
	if encode == nil {
		return nil, errors.New("encoder is nil")
	}
	return encode(copyTreeValue(t.root))
}

// MarshalJSON encodes the root object, including any configured secrets.
func (t Tree) MarshalJSON() ([]byte, error) {
	if t.root == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(t.root)
}

// Snapshot pairs a typed value with the tree from the same publication.
// Value and its reachable references follow Current's read-only contract.
type Snapshot[T any] struct {
	Tree
	Value *T
}

// All collections in a published tree have already been normalized, copied
// from their sources and checked for depth, cycles and node count.
func copyTreeValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, child := range v {
			out[k] = copyTreeValue(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = copyTreeValue(child)
		}
		return out
	default:
		return v
	}
}

// Change is the preceding and current tree for one successful publication.
type Change struct{ Before, After Tree }

type subscription struct {
	path []string
	ch   chan Change
}

// Subscribe registers a path without sending an initial event. For an
// initial state plus subsequent events, use SubscribeSnapshot. Cancel is
// idempotent and closes the channel. A slow reader gets the latest pending
// change. Only successful publications whose selected value changed signal.
func (c *Config[T]) Subscribe(path ...string) (<-chan Change, func()) {
	_, ch, cancel := c.SubscribeSnapshot(path...)
	return ch, cancel
}

// SubscribeSnapshot captures the initial snapshot and registers a subscription
// at the same publication boundary. Every delivered change follows initial.
// As with Subscribe, a slow consumer receives the latest pending change.
func (c *Config[T]) SubscribeSnapshot(path ...string) (initial Snapshot[T], changes <-chan Change, cancel func()) {
	sub := &subscription{path: slices.Clone(path), ch: make(chan Change, 1)}
	c.observersMu.Lock()
	if c.observers == nil {
		c.observers = make(map[*subscription]struct{})
	}
	c.observers[sub] = struct{}{}
	initial = c.Snapshot()
	c.observersMu.Unlock()
	return initial, sub.ch, func() {
		c.observersMu.Lock()
		defer c.observersMu.Unlock()
		if _, ok := c.observers[sub]; ok {
			delete(c.observers, sub)
			close(sub.ch)
		}
	}
}

// Called with publishers and cancellation serialized; receivers run independently.
func (s *subscription) notify(before, after Tree) {
	old, had := before.find(s.path)
	next, has := after.find(s.path)
	if had == has && equalTreeValues(old, next) {
		return
	}
	event := Change{Before: before, After: after}
	select {
	case s.ch <- event:
		return
	default:
	}
	select {
	case <-s.ch:
	default:
	}
	s.ch <- event
}

// Source collections are normalized to these two types. Go scalar equality
// also compares their concrete types; same-type NaNs represent one tree value.
func equalTreeValues(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		return ok && maps.EqualFunc(x, y, equalTreeValues)
	case []any:
		y, ok := b.([]any)
		return ok && slices.EqualFunc(x, y, equalTreeValues)
	}
	if a == b {
		return true
	}
	x, y := reflect.ValueOf(a), reflect.ValueOf(b)
	if !x.IsValid() || !y.IsValid() || x.Type() != y.Type() {
		return false
	}
	return (x.Kind() == reflect.Float32 || x.Kind() == reflect.Float64) && math.IsNaN(x.Float()) && math.IsNaN(y.Float())
}

// Set replaces a value in the current version. Missing intermediate objects
// are created. List indices must already exist. Edits expire on the next
// successful Reload or Load. No source is read.
func (c *Config[T]) Set(ctx context.Context, value any, path ...string) error {
	return c.edit(ctx, path, false, func(any) (any, error) { return value, nil })
}

// Delete removes a key or a list element from this version. An empty path
// clears the root. A missing path returns ErrNotFound without publishing.
func (c *Config[T]) Delete(ctx context.Context, path ...string) error {
	return c.edit(ctx, path, true, func(any) (any, error) { return nil, nil })
}

// MergeAt overlays a tree at a path in the current version. A missing path
// is created. The existing target must be an object. The configured merge
// policy applies, and the edit expires on the next successful reload.
func (c *Config[T]) MergeAt(ctx context.Context, tree Tree, path ...string) error {
	return c.edit(ctx, path, false, func(old any) (any, error) {
		var dst map[string]any
		if old == nil {
			dst = map[string]any{}
		} else {
			m, ok := old.(map[string]any)
			if !ok {
				return nil, errTypeMismatch
			}
			dst = copyTreeValue(m).(map[string]any)
		}
		overlay := layerMerger{values: dst, merge: c.options.Merge}
		if err := overlay.add(tree.root); err != nil {
			return nil, err
		}
		return overlay.values, nil
	})
}

func (c *Config[T]) edit(ctx context.Context, path []string, remove bool, change func(any) (any, error)) error {
	if err := c.acquire(ctx); err != nil {
		return err
	}
	defer func() { <-c.sem }()
	if len(path) > 128 {
		return &Error{Stage: StageMerge, Source: -1, Err: errDepth}
	}
	root, err := editValue(c.unresolved, path, remove, change)
	if err != nil {
		return &Error{Stage: StageMerge, Source: -1, Err: err}
	}
	if remove && len(path) == 0 {
		root = map[string]any{}
	}
	// Normalize new values and validate total depth and size before publication.
	owned, err := new(layerMerger).copyValue(root)
	if err != nil {
		return &Error{Stage: StageMerge, Source: -1, Err: err}
	}
	m, ok := owned.(map[string]any)
	if !ok {
		return &Error{Stage: StageMerge, Source: -1, Err: errors.New("root must be an object")}
	}
	return c.publish(ctx, m)
}

func editValue(node any, path []string, remove bool, change func(any) (any, error)) (any, error) {
	if len(path) == 0 {
		return change(node)
	}
	key := path[0]
	switch parent := node.(type) {
	case map[string]any:
		old, ok := parent[key]
		if !ok && remove {
			return nil, ErrNotFound
		}
		out := maps.Clone(parent)
		if len(path) == 1 && remove {
			delete(out, key)
			return out, nil
		}
		if !ok && len(path) > 1 {
			old = map[string]any{}
		}
		child, err := editValue(old, path[1:], remove, change)
		if err != nil {
			return nil, err
		}
		out[key] = child
		return out, nil
	case []any:
		i, ok := listIndex(key, len(parent))
		if !ok {
			return nil, ErrNotFound
		}
		out := slices.Clone(parent)
		if len(path) == 1 && remove {
			return slices.Delete(out, i, i+1), nil
		}
		child, err := editValue(parent[i], path[1:], remove, change)
		if err != nil {
			return nil, err
		}
		out[i] = child
		return out, nil
	default:
		if remove {
			return nil, ErrNotFound
		}
		return nil, errTypeMismatch
	}
}
