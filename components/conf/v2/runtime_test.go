package conf_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	conf "github.com/apus-run/gala/components/conf/v2"
)

type watchedSource struct {
	mu       sync.Mutex
	data     map[string]any
	calls    int
	watchErr error
	watchers map[*testWatcher]struct{}
}

func (s *watchedSource) Load(context.Context) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.data, nil
}
func (s *watchedSource) Watch(context.Context) (conf.Watcher, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watchErr != nil {
		return nil, s.watchErr
	}
	if s.watchers == nil {
		s.watchers = make(map[*testWatcher]struct{})
	}
	w := &testWatcher{source: s, events: make(chan error, 1)}
	s.watchers[w] = struct{}{}
	return w, nil
}
func (s *watchedSource) set(data map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = data
	for w := range s.watchers {
		select {
		case w.events <- nil:
		default:
		}
	}
}
func (s *watchedSource) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, len(s.watchers)
}

type testWatcher struct {
	source *watchedSource
	events chan error
}

func (w *testWatcher) Next(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-w.events:
		return err
	}
}
func (w *testWatcher) Close() error {
	w.source.mu.Lock()
	defer w.source.mu.Unlock()
	delete(w.source.watchers, w)
	return nil
}

func TestAutomaticReloadPrecedenceAndRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		base := &watchedSource{data: map[string]any{"port": 1}}
		high := &watchedSource{data: map[string]any{"port": 2}}
		c, err := conf.Open[typed](t.Context(), conf.WithSources(base, high), conf.WithWatch())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		synctest.Wait()
		events, cancel := c.Subscribe()
		defer cancel()
		old := c.Current()
		base.set(map[string]any{"port": 3})
		synctest.Wait()
		if c.Current().Port != 2 {
			t.Fatal("source precedence changed")
		}
		select {
		case <-events:
			t.Fatal("unchanged configuration notified")
		default:
		}
		high.set(map[string]any{"port": "SECRET"})
		synctest.Wait()
		event := <-events
		assertFailure(t, event.Err, conf.StageBind)
		if c.Current().Port != 2 || old.Port != 2 {
			t.Fatal("failed batch published")
		}
		high.set(map[string]any{"port": 4})
		synctest.Wait()
		event = <-events
		if event.Err != nil || event.Current.Port != 4 || event.Previous.Port != 2 {
			t.Fatalf("recovery event: %+v", event)
		}
		high.set(map[string]any{})
		synctest.Wait()
		if c.Current().Port != 3 || old.Port != 2 {
			t.Fatal("override removal did not restore lower layer")
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		_, n := base.counts()
		_, m := high.counts()
		if n != 0 || m != 0 {
			t.Fatal("watcher leak")
		}
		if err := c.Reload(t.Context()); err == nil {
			t.Fatal("closed config reloaded")
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestLoadAppendsTransactionallyWhileWatching(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		base := &watchedSource{data: map[string]any{"port": 1}}
		c, err := conf.Open[typed](t.Context(), conf.WithSources(base), conf.WithWatch())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		synctest.Wait()
		calls, _ := base.counts()
		high := &watchedSource{data: map[string]any{"port": 2}}
		if err := c.Load(t.Context(), high); err != nil {
			t.Fatal(err)
		}
		if after, _ := base.counts(); after != calls {
			t.Fatal("Load reread existing layer")
		}
		if c.Current().Port != 2 {
			t.Fatal("appended layer not applied")
		}
		rejected := &watchedSource{data: map[string]any{"port": "SECRET"}}
		old := c.Current()
		assertFailure(t, c.Load(t.Context(), rejected), conf.StageBind)
		if c.Current() != old {
			t.Fatal("failed append published")
		}
		rejectedCalls, watchers := rejected.counts()
		if watchers != 0 {
			t.Fatal("failed append leaked watcher")
		}
		high.set(map[string]any{"port": 3})
		synctest.Wait()
		if c.Current().Port != 3 {
			t.Fatal("new source not watched")
		}
		if after, _ := rejected.counts(); after != rejectedCalls {
			t.Fatal("rejected source retained")
		}
	})
}

func TestWatcherStartupRollbackAndRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		first := &watchedSource{data: map[string]any{"port": 1}}
		broken := &watchedSource{data: map[string]any{}, watchErr: errors.New("SECRET")}
		c, err := conf.Open[typed](t.Context(), conf.WithSources(first, broken), conf.WithWatch())
		if c != nil {
			t.Fatal("failed startup returned config")
		}
		assertFailure(t, err, conf.StageWatch)
		if _, n := first.counts(); n != 0 {
			t.Fatal("startup leaked watcher")
		}
		c, err = conf.Open[typed](t.Context(), conf.WithSources(first), conf.WithWatch())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		synctest.Wait()
		events, cancel := c.Subscribe()
		defer cancel()
		first.mu.Lock()
		for w := range first.watchers {
			w.events <- errors.New("SECRET")
		}
		first.mu.Unlock()
		synctest.Wait()
		assertFailure(t, (<-events).Err, conf.StageWatch)
		first.set(map[string]any{"port": 9}) // update while disconnected
		time.Sleep(time.Second)
		synctest.Wait()
		if c.Current().Port != 9 {
			t.Fatal("watch recovery missed update")
		}
	})
}

func TestPollingAndTypedObservers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var value atomic.Int64
		value.Store(1)
		source := conf.SourceFunc(func(context.Context) (map[string]any, error) { return map[string]any{"port": value.Load()}, nil })
		c, err := conf.Open[typed](t.Context(), conf.WithSources(source), conf.WithPollInterval(time.Second), conf.WithWatch())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		synctest.Wait()
		changes, cancel := conf.Observe(t.Context(), c, func(v *typed) int { return v.Port })
		defer cancel()
		synctest.Wait()
		value.Store(2)
		time.Sleep(time.Second)
		synctest.Wait()
		change := <-changes
		if change.Previous != 1 || change.Current != 2 {
			t.Fatalf("change=%+v", change)
		}
		value.Store(3)
		if err := c.Reload(t.Context()); err != nil {
			t.Fatal(err)
		}
		value.Store(4)
		if err := c.Reload(t.Context()); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		change = <-changes
		if change.Current != 4 {
			t.Fatal("coalesced observer lost latest value")
		}
		cancel()
	})
}

func TestMergeResolverAndIncrementalCache(t *testing.T) {
	type app struct {
		Port int      `mapstructure:"port"`
		Tags []string `mapstructure:"tags"`
	}
	source := &fakeSource{data: map[string]any{"port": 1, "tags": []string{"a"}}}
	fail := false
	c, err := conf.Open[app](t.Context(), conf.WithSources(source), conf.WithMerge(func(dst, src map[string]any) error {
		for k, v := range src {
			if k == "tags" {
				previous, _ := dst[k].([]any)
				dst[k] = append(previous, v.([]any)...)
			} else {
				dst[k] = v
			}
		}
		return nil
	}), conf.WithResolver(func(ctx context.Context, m map[string]any) error {
		if fail {
			return errors.New("SECRET")
		}
		return ctx.Err()
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	source.data["port"] = 9
	if err := c.Load(t.Context(), &fakeSource{data: map[string]any{"tags": []string{"b"}}}); err != nil {
		t.Fatal(err)
	}
	if c.Current().Port != 1 || !slices.Equal(c.Current().Tags, []string{"a", "b"}) {
		t.Fatal("cached layers or custom merge incorrect")
	}
	old := c.Current()
	fail = true
	assertFailure(t, c.Reload(t.Context()), conf.StageResolve)
	if c.Current() != old {
		t.Fatal("resolver failure published")
	}
	fail = false
	if err := c.Reload(t.Context()); err != nil || c.Current().Port != 9 {
		t.Fatalf("refresh: %v", err)
	}
}

func TestCloseCancelsInFlightReload(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		entered := make(chan struct{})
		source := conf.SourceFunc(func(ctx context.Context) (map[string]any, error) {
			calls++
			if calls == 1 {
				return map[string]any{"port": 1}, nil
			}
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		c, err := conf.Open[typed](t.Context(), conf.WithSources(source))
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- c.Reload(t.Context()) }()
		<-entered
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("reload not canceled: %v", err)
		}
		if c.Current().Port != 1 {
			t.Fatal("canceled reload published")
		}
	})
}

func TestStartCancelRestartAndCloseSubscribers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &watchedSource{data: map[string]any{"port": 1}}
		c, err := conf.Open[typed](t.Context(), conf.WithSources(source))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		ctx, cancel := context.WithCancel(t.Context())
		if err := c.Start(ctx); err != nil {
			t.Fatal(err)
		}
		if err := c.Start(ctx); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if _, n := source.counts(); n != 1 {
			t.Fatal("duplicate watcher")
		}
		cancel()
		synctest.Wait()
		if _, n := source.counts(); n != 0 {
			t.Fatal("canceled watcher leaked")
		}
		if err := c.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		events, unsubscribe := c.Subscribe()
		defer unsubscribe()
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		if _, ok := <-events; ok {
			t.Fatal("subscription not closed")
		}
	})
}

type blockingWatchSource struct{ entered chan struct{} }

func (s blockingWatchSource) Load(context.Context) (map[string]any, error) {
	return map[string]any{}, nil
}
func (s blockingWatchSource) Watch(ctx context.Context) (conf.Watcher, error) {
	close(s.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestLoadCancellationStopsWatcherSetup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		base := &watchedSource{data: map[string]any{"port": 1}}
		c, err := conf.Open[typed](t.Context(), conf.WithSources(base), conf.WithWatch())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		synctest.Wait()
		pending := blockingWatchSource{entered: make(chan struct{})}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- c.Load(ctx, pending) }()
		<-pending.entered
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("watch setup ignored load cancellation: %v", err)
		}
		if c.Current().Port != 1 {
			t.Fatal("canceled append published")
		}
	})
}

type closeFailureSource struct{}

func (closeFailureSource) Load(context.Context) (map[string]any, error) { return map[string]any{}, nil }
func (closeFailureSource) Watch(context.Context) (conf.Watcher, error) {
	return closeFailureWatcher{}, nil
}

type closeFailureWatcher struct{}

func (closeFailureWatcher) Next(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (closeFailureWatcher) Close() error                   { return errors.New("SECRET") }
func TestCloseReportsRedactedWatcherFailure(t *testing.T) {
	c, err := conf.Open[typed](t.Context(), conf.WithSources(closeFailureSource{}), conf.WithWatch())
	if err != nil {
		t.Fatal(err)
	}
	first := c.Close()
	assertFailure(t, first, conf.StageWatch)
	if c.Close() != first {
		t.Fatal("Close result is not stable")
	}
}
