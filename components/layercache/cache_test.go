package layercache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apus-run/gala/components/ecache"
	"github.com/stretchr/testify/require"
)

var ctx = context.Background()
var errBackend = errors.New("backend unavailable")

type memRemote struct {
	mu                     sync.Mutex
	data                   map[string]RemoteEntry
	reads, writes, deletes atomic.Int64
	get                    func(context.Context, string) (RemoteEntry, error)
	set                    func(context.Context, string, []byte, time.Duration) error
	del                    func(context.Context, string) error
}

func newRemote() *memRemote { return &memRemote{data: make(map[string]RemoteEntry)} }

func (r *memRemote) Get(ctx context.Context, key string) (RemoteEntry, error) {
	r.reads.Add(1)
	if r.get != nil {
		return r.get(ctx, key)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.data[key]
	if !ok || !time.Now().Before(e.ExpiresAt) {
		return RemoteEntry{}, ErrMiss
	}
	return e, nil
}

func (r *memRemote) Set(ctx context.Context, key string, data []byte, ttl time.Duration) (time.Time, error) {
	r.writes.Add(1)
	deadline := time.Now().Add(ttl)
	if r.set != nil {
		return deadline, r.set(ctx, key, data, ttl)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[key] = RemoteEntry{Data: append([]byte(nil), data...), ExpiresAt: deadline}
	return deadline, nil
}

func (r *memRemote) Delete(ctx context.Context, key string) error {
	r.deletes.Add(1)
	if r.del != nil {
		return r.del(ctx, key)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.data, key)
	return nil
}

func newLocal() Local { return ecache.New[string, Entry](ecache.WithMaxKeys(100)) }

func newCache[V any](t *testing.T, local Local, remote Remote, opts ...Option) *Typed[V] {
	t.Helper()
	opts = append([]Option{WithNamespace("test")}, opts...)
	base, err := New(append([]Option{WithLocal(local), WithRemote(remote)}, opts...)...)
	require.NoError(t, err)
	c := NewTyped[V](base)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	return c
}

func assertIdle[V any](t *testing.T, c *Typed[V]) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	require.Empty(t, c.states, "[ablation:state_reclamation]")
	require.Empty(t, c.flights)
}

func TestConstructionAndOptions(t *testing.T) {
	var nilRemote *memRemote
	for _, tc := range []struct {
		name   string
		local  Local
		remote Remote
		opts   []Option
	}{
		{"no backends", nil, nil, []Option{WithNamespace("x")}},
		{"typed nil", nil, nilRemote, []Option{WithNamespace("x")}},
		{"no namespace", newLocal(), nil, nil},
		{"blank namespace", newLocal(), nil, []Option{WithNamespace(" \t")}},
		{"nil codec", newLocal(), nil, []Option{WithNamespace("x"), WithCodec(nil)}},
		{"local TTL", newLocal(), nil, []Option{WithNamespace("x"), WithLocalTTL(0)}},
		{"remote TTL", newLocal(), nil, []Option{WithNamespace("x"), WithRemoteTTL(-1)}},
		{"negative TTL", newLocal(), nil, []Option{WithNamespace("x"), WithNotFoundTTL(-1)}},
		{"load timeout", newLocal(), nil, []Option{WithNamespace("x"), WithLoadTimeout(0)}},
		{"remote timeout", newLocal(), nil, []Option{WithNamespace("x"), WithRemoteTimeout(-1)}},
		{"flight limit", newLocal(), nil, []Option{WithNamespace("x"), WithMaxConcurrentLoads(0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New(append([]Option{WithLocal(tc.local), WithRemote(tc.remote)}, tc.opts...)...)
			require.Error(t, err)
			require.Nil(t, c)
		})
	}
	a, b := DefaultOptions(), DefaultOptions()
	a.Apply(WithLocalTTL(0)) // No validation or rollback in Apply.
	require.Equal(t, DefaultLocalTTL, b.localTTL)
	a.Apply(WithNamespace("x"), WithLocalTTL(time.Second), WithLocalTTL(2*time.Second))
	require.NoError(t, a.Validate())
	require.Equal(t, 2*time.Second, a.localTTL)
	var captured *Options
	c := newCache[string](t, newLocal(), nil, OptionFunc(func(o *Options) { captured = o }))
	captured.Apply(WithNamespace("changed"), WithLocalTTL(time.Hour))
	require.Equal(t, "test", c.opts.namespace)
	require.Equal(t, DefaultLocalTTL, c.opts.localTTL)
}

func TestReadWritePaths(t *testing.T) {
	for _, mode := range []string{"local", "remote", "both"} {
		t.Run(mode, func(t *testing.T) {
			var local Local
			var remote *memRemote
			if mode != "remote" {
				local = newLocal()
			}
			if mode != "local" {
				remote = newRemote()
			}
			c := newCache[string](t, local, remote)
			_, err := c.Get(ctx, "key")
			require.ErrorIs(t, err, ErrMiss)
			var calls int
			load := func(_ context.Context, key string) (string, error) {
				calls++
				require.Equal(t, "key", key)
				return "value", nil
			}
			for range 2 {
				got, err := c.GetOrLoad(ctx, "key", load)
				require.NoError(t, err)
				require.Equal(t, "value", got)
			}
			require.Equal(t, 1, calls)
			if mode == "both" {
				reads := remote.reads.Load()
				require.NoError(t, c.InvalidateLocal(ctx, "key"))
				got, err := c.Get(ctx, "key")
				require.NoError(t, err)
				require.Equal(t, "value", got)
				require.Equal(t, reads+1, remote.reads.Load())
				_, ok := local.Get(c.prefix + "key")
				require.True(t, ok)
			}
			require.NoError(t, c.Set(ctx, "key", "new"))
			got, err := c.Get(ctx, "key")
			require.NoError(t, err)
			require.Equal(t, "new", got)
			require.NoError(t, c.Delete(ctx, "key"))
			_, err = c.Get(ctx, "key")
			require.ErrorIs(t, err, ErrMiss)
			_, err = c.GetOrLoad(ctx, "key", nil)
			require.Error(t, err)
			assertIdle(t, c)
		})
	}
}

func TestNegativeCaching(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRemote()
		c := newCache[string](t, newLocal(), r, WithNotFoundTTL(2*time.Second))
		calls := 0
		load := func(context.Context, string) (string, error) {
			calls++
			return "", fmt.Errorf("missing row: %w", ErrNotFound)
		}
		for range 2 {
			_, err := c.GetOrLoad(ctx, "missing", load)
			require.ErrorIs(t, err, ErrNotFound)
		}
		require.Equal(t, 1, calls)
		local, ok := c.local.Get(c.prefix + "missing")
		require.True(t, ok)
		require.Equal(t, 2*time.Second, time.Until(local.ExpiresAt))
		require.NoError(t, c.InvalidateLocal(ctx, "missing"))
		_, err := c.Get(ctx, "missing")
		require.ErrorIs(t, err, ErrNotFound)
		time.Sleep(2 * time.Second)
		_, err = c.GetOrLoad(ctx, "missing", load)
		require.ErrorIs(t, err, ErrNotFound)
		require.Equal(t, 2, calls)
	})
	for _, originErr := range []error{ErrNotFound, errBackend} {
		c := newCache[string](t, newLocal(), nil)
		calls := 0
		for range 2 {
			_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { calls++; return "", originErr })
			require.Same(t, originErr, err)
		}
		require.Equal(t, 2, calls)
		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, ErrMiss)
	}
}

func TestReadErrorsNeverLoad(t *testing.T) {
	for _, tc := range []struct {
		name      string
		data      []byte
		deadline  time.Time
		err, want error
	}{
		{"backend", nil, time.Time{}, errBackend, errBackend},
		{"persistent", []byte{'L', 'C', 1, 0, '1'}, time.Time{}, nil, ErrInvalidTTL},
		{"raw JSON", []byte(`"old"`), time.Now().Add(time.Minute), nil, ErrCorrupt},
		{"version", []byte{'L', 'C', 99, 0, '1'}, time.Now().Add(time.Minute), nil, ErrCorrupt},
		{"kind", []byte{'L', 'C', 1, 2}, time.Now().Add(time.Minute), nil, ErrCorrupt},
		{"negative payload", []byte{'L', 'C', 1, 1, 'x'}, time.Now().Add(time.Minute), nil, ErrCorrupt},
		{"bad JSON", []byte{'L', 'C', 1, 0, '{'}, time.Now().Add(time.Minute), nil, ErrCorrupt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRemote()
			r.get = func(context.Context, string) (RemoteEntry, error) {
				return RemoteEntry{Data: tc.data, ExpiresAt: tc.deadline}, tc.err
			}
			c := newCache[int](t, newLocal(), r)
			called := false
			_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (int, error) { called = true; return 0, nil })
			require.ErrorIs(t, err, tc.want)
			require.False(t, called)
			var op *OpError
			require.ErrorAs(t, err, &op)
			_, ok := c.local.Get(c.prefix + "key")
			require.Equal(t, tc.name == "bad JSON", ok)
		})
	}
}

func TestWriteFailureSemantics(t *testing.T) {
	r := newRemote()
	r.set = func(context.Context, string, []byte, time.Duration) error { return errBackend }
	var reported error
	c := newCache[string](t, newLocal(), r, WithErrorHandler(func(err error) { reported = err }))
	got, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "source", nil })
	require.NoError(t, err)
	require.Equal(t, "source", got)
	require.ErrorIs(t, reported, errBackend)
	_, ok := c.local.Get(c.prefix + "key")
	require.False(t, ok)
	c.local.Set(c.prefix+"key", Entry{Data: wireString("old"), ExpiresAt: time.Now().Add(time.Minute)}, time.Minute)
	require.ErrorIs(t, c.Set(ctx, "key", "new"), errBackend)
	_, ok = c.local.Get(c.prefix + "key")
	require.False(t, ok)
	r.del = func(context.Context, string) error { return errBackend }
	c.local.Set(c.prefix+"key", Entry{Data: wireString("old"), ExpiresAt: time.Now().Add(time.Minute)}, time.Minute)
	require.ErrorIs(t, c.Delete(ctx, "key"), errBackend)
	_, ok = c.local.Get(c.prefix + "key")
	require.False(t, ok)
}

type failingCodec struct{}

func (failingCodec) Marshal(any) ([]byte, error) { return nil, errBackend }
func (failingCodec) Unmarshal([]byte, any) error { return errBackend }

func TestEncodingFailure(t *testing.T) {
	c := newCache[any](t, newLocal(), nil, WithCodec(failingCodec{}))
	c.local.Set(c.prefix+"key", Entry{Data: wireString("old"), ExpiresAt: time.Now().Add(time.Minute)}, time.Minute)
	require.ErrorIs(t, c.Set(ctx, "key", struct{}{}), errBackend)
	got, err := c.Get(ctx, "key")
	require.NoError(t, err)
	require.Equal(t, "old", got)
	require.NoError(t, c.Delete(ctx, "key"))
	got, err = c.GetOrLoad(ctx, "key", func(context.Context, string) (any, error) { return struct{}{}, nil })
	require.ErrorIs(t, err, errBackend)
	require.Nil(t, got)
	_, ok := c.local.Get(c.prefix + "key")
	require.False(t, ok)
}

func TestNamespaceFraming(t *testing.T) {
	r := newRemote()
	a := newCache[string](t, nil, r, WithNamespace("a"))
	b := newCache[string](t, nil, r, WithNamespace("a:b"))
	require.NoError(t, a.Set(ctx, "b:c", "first"))
	require.NoError(t, b.Set(ctx, "c", "second"))
	got, err := a.Get(ctx, "b:c")
	require.NoError(t, err)
	require.Equal(t, "first", got)
	got, err = b.Get(ctx, "c")
	require.NoError(t, err)
	require.Equal(t, "second", got)
}

func wireString(s string) []byte { return append([]byte{'L', 'C', 2, 2}, []byte(s)...) }

func TestBuiltinLocalOptionsAreBoundedAndReusable(t *testing.T) {
	opts := []Option{WithNamespace("default-local"), WithLocalCapacity(1)}
	configured := NewOptions(opts...)
	require.NoError(t, configured.Validate())
	require.Nil(t, configured.local, "assembling and validating options must not create a store")
	first, err := New(opts...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, first.Close()) })
	second, err := New(opts...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, second.Close()) })
	require.NotEqual(t, first.SourceID(), second.SourceID())
	a, b := NewTyped[string](first), NewTyped[string](second)
	require.NoError(t, a.Set(ctx, "one", "1"))
	_, err = b.Get(ctx, "one")
	require.ErrorIs(t, err, ErrMiss, "reusing options must not share built-in local state")
	require.NoError(t, a.Set(ctx, "two", "2"))
	_, err = a.Get(ctx, "one")
	require.ErrorIs(t, err, ErrMiss, "configured capacity must actually evict entries")
	got, err := a.Get(ctx, "two")
	require.NoError(t, err)
	require.Equal(t, "2", got)
	require.Nil(t, configured.local)
}

func TestLocalBackendSelectionUsesLastOption(t *testing.T) {
	custom := newLocal()
	for _, tc := range []struct {
		name    string
		opts    []Option
		builtin bool
	}{
		{"default", nil, true},
		{"custom", []Option{WithLocalCapacity(2), WithLocal(custom)}, false},
		{"builtin", []Option{WithLocal(custom), WithLocalCapacity(2)}, true},
		{"replace invalid choice", []Option{WithLocalCapacity(0), WithLocal(custom)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New(append([]Option{WithNamespace(tc.name)}, tc.opts...)...)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, c.Close()) })
			if tc.builtin {
				require.NotSame(t, custom, c.local)
			} else {
				require.Same(t, custom, c.local)
			}
			require.Equal(t, TypeLocal, c.CacheType())
		})
	}
	remote := newRemote()
	both, err := New(WithNamespace("both"), WithRemote(remote))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, both.Close()) })
	require.Equal(t, TypeBoth, both.CacheType())
	only, err := New(WithNamespace("remote-only"), WithRemote(remote), WithLocal(nil))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, only.Close()) })
	require.Equal(t, TypeRemote, only.CacheType())
}

func TestOptionsErrorsIdentifyTheSetting(t *testing.T) {
	for _, tc := range []struct {
		name     string
		option   Option
		sentinel error
		message  string
	}{
		{"capacity", WithLocalCapacity(0), ErrInvalidOptions, "local capacity"},
		{"negative capacity", WithLocalCapacity(-1), ErrInvalidOptions, "local capacity"},
		{"local TTL", WithLocalTTL(0), ErrInvalidTTL, "local TTL"},
		{"remote TTL", WithRemoteTTL(-1), ErrInvalidTTL, "remote TTL"},
		{"negative caching TTL", WithNotFoundTTL(-1), ErrInvalidTTL, "not-found TTL"},
		{"jitter", WithTTLJitter(-1), ErrInvalidTTL, "TTL jitter"},
		{"load timeout", WithLoadTimeout(0), ErrInvalidOptions, "load timeout"},
		{"remote timeout", WithRemoteTimeout(0), ErrInvalidOptions, "remote timeout"},
		{"load capacity", WithMaxConcurrentLoads(0), ErrInvalidOptions, "max concurrent loads"},
		{"refresh interval", WithRefreshInterval(time.Millisecond), ErrInvalidOptions, "refresh interval"},
		{"refresh idle", WithRefreshIdleTimeout(-1), ErrInvalidOptions, "refresh idle timeout"},
		{"refresh capacity", WithMaxRefreshTasks(0), ErrInvalidOptions, "max refresh tasks"},
		{"refresh concurrency", WithRefreshConcurrency(0), ErrInvalidOptions, "refresh concurrency"},
		{"events buffer", WithEventBuffer(0), ErrInvalidOptions, "event buffer"},
		{"stats interval", WithStatsInterval(0), ErrInvalidOptions, "stats interval"},
		{"codec", WithCodec(nil), ErrInvalidOptions, "codec"},
		{"nil option", nil, ErrInvalidOptions, "option 1 is nil"},
		{"nil adapter", OptionFunc(nil), ErrInvalidOptions, "option 1 is nil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New(WithNamespace("invalid"), tc.option)
			require.Nil(t, c)
			require.ErrorIs(t, err, tc.sentinel)
			require.ErrorContains(t, err, tc.message)
		})
	}
}

func TestDefaultNotFoundTTLMatchesDisabledDefault(t *testing.T) {
	c, err := New(WithNamespace("explicit-default"), WithNotFoundTTL(DefaultNotFoundTTL))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	view := NewTyped[string](c)
	calls := 0
	for range 2 {
		_, err := view.GetOrLoad(ctx, "missing", func(context.Context, string) (string, error) { calls++; return "", ErrNotFound })
		require.ErrorIs(t, err, ErrNotFound)
	}
	require.Equal(t, 2, calls, "the exported default must keep negative caching disabled")
	require.Equal(t, DefaultOptions().notFoundTTL, DefaultNotFoundTTL)
}

func TestNotFoundRecognitionDoesNotEnableNegativeCaching(t *testing.T) {
	missing := errors.New("application missing")
	for _, cacheNegative := range []bool{false, true} {
		opts := []Option{WithNamespace("not-found"), WithErrNotFound(missing)}
		if cacheNegative {
			opts = append(opts, WithNotFoundTTL(time.Minute))
		}
		c, err := New(opts...)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, c.Close()) })
		view := NewTyped[string](c)
		calls := 0
		for range 2 {
			_, err := view.GetOrLoad(ctx, "missing", func(context.Context, string) (string, error) { calls++; return "", missing })
			require.True(t, c.IsNotFound(err))
		}
		if cacheNegative {
			require.Equal(t, 1, calls)
		} else {
			require.Equal(t, 2, calls)
		}
	}
	// Recognition and storage are independent, in either option order.
	for _, opts := range [][]Option{
		{WithNotFoundTTL(0), WithErrNotFound(missing)},
		{WithErrNotFound(missing), WithNotFoundTTL(0)},
	} {
		o := NewOptions(opts...)
		require.Zero(t, o.notFoundTTL)
		require.Same(t, missing, o.errNotFound)
	}
}

func TestEventHandlerAloneEnablesEvents(t *testing.T) {
	seen := make(chan Event, 1)
	c, err := New(WithNamespace("events"), WithRemote(newRemote()), WithEventHandler(func(_ context.Context, e Event) error { seen <- e; return nil }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	require.NoError(t, c.Set(ctx, "key", Value("value")))
	select {
	case e := <-seen:
		require.Equal(t, EventSet, e.Type)
		require.Equal(t, []string{"key"}, e.Keys)
	case <-time.After(time.Second):
		t.Fatal("handler must enable delivery without a second toggle")
	}
	disabled, err := New(WithNamespace("disabled-events"), WithEventHandler(func(context.Context, Event) error { t.Error("disabled handler invoked"); return nil }), WithEventHandler(nil))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, disabled.Close()) })
	require.Nil(t, disabled.events)
	require.NoError(t, disabled.Set(ctx, "key", Value("value")))
}

func TestNewValidatesOnlyFinalOptionValues(t *testing.T) {
	for _, tc := range []struct {
		name    string
		opts    []Option
		invalid bool
	}{
		{"replace invalid value", []Option{WithLocalTTL(0), WithLocalTTL(time.Second)}, false},
		{"explicit zero wins", []Option{WithLocalTTL(time.Second), WithLocalTTL(0)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New(append([]Option{WithNamespace("final-values")}, tc.opts...)...)
			if tc.invalid {
				require.ErrorIs(t, err, ErrInvalidTTL)
				require.Nil(t, c)
				return
			}
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, c.Close()) })
			require.Equal(t, time.Second, c.opts.localTTL)
		})
	}
}

func TestConstructionKeepsDerivedDefaultsOutOfInputOptions(t *testing.T) {
	var input *Options
	c, err := New(WithNamespace("snapshot"), WithRefreshInterval(time.Second), OptionFunc(func(o *Options) { input = o }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	// Runtime ownership and per-instance defaults belong to the cache's snapshot.
	require.Nil(t, input.local)
	require.Empty(t, input.sourceID)
	require.Zero(t, input.refreshIdle)
	require.NoError(t, input.Validate())
	require.NotNil(t, c.local)
	require.NotEmpty(t, c.SourceID())
	require.Equal(t, 2*time.Second, c.opts.refreshIdle)
	// Deliberately retain the input to verify the defensive value-copy boundary.
	input.Apply(WithNamespace("changed"), WithLocalCapacity(1), WithRefreshInterval(0))
	require.Equal(t, "snapshot", c.Namespace())
	require.Equal(t, DefaultLocalCapacity, c.opts.localCapacity)
	require.Equal(t, time.Second, c.opts.refreshInterval)
}

func TestNilItemOptionsFailBeforeSideEffects(t *testing.T) {
	remote := newRemote()
	c := newCache[string](t, newLocal(), remote)
	ids := NewT[int, string](c.Cache)
	require.NoError(t, c.Set(ctx, "key", "old"))
	require.NoError(t, ids.Set(ctx, "id", 1, "old"))
	writes := remote.writes.Load()
	calls := 0
	origin := func(context.Context) (any, error) { calls++; return "candidate", nil }
	typedOrigin := func(context.Context, string) (string, error) { calls++; return "candidate", nil }
	idOrigin := func(context.Context, int) (string, error) { calls++; return "candidate", nil }
	for _, invalid := range []struct {
		name   string
		option ItemOption
	}{{"nil interface", nil}, {"nil adapter", ItemOptionFunc(nil)}} {
		t.Run(invalid.name, func(t *testing.T) {
			for _, tc := range []struct {
				name string
				run  func() error
			}{
				{"Set", func() error { return c.Cache.Set(ctx, "key", Do(origin), invalid.option) }},
				{"Once", func() error { var out string; return c.Once(ctx, "key", Value(&out), Do(origin), invalid.option) }},
				{"Typed.Set", func() error { return c.Set(ctx, "key", "candidate", invalid.option) }},
				{"Typed.GetOrLoad", func() error { _, err := c.GetOrLoad(ctx, "key", typedOrigin, invalid.option); return err }},
				{"T.Set", func() error { return ids.Set(ctx, "id", 1, "candidate", invalid.option) }},
				{"T.GetOrLoad", func() error { _, err := ids.GetOrLoad(ctx, "id", 1, idOrigin, invalid.option); return err }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					var err error
					require.NotPanics(t, func() { err = tc.run() }, "nil item options must behave like nil constructor options")
					require.ErrorIs(t, err, ErrInvalidOptions)
					require.Zero(t, calls)
					require.Equal(t, writes, remote.writes.Load())
					value, err := c.Get(ctx, "key")
					require.NoError(t, err)
					require.Equal(t, "old", value)
					value, err = ids.Get(ctx, "id", 1, nil)
					require.NoError(t, err)
					require.Equal(t, "old", value)
				})
			}

		})
	}
}
