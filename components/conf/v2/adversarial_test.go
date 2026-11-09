package conf_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	conf "github.com/apus-run/gala/components/conf/v2"
)

type transientReadSource struct {
	*watchedSource
	failNext atomic.Bool
}

func (s *transientReadSource) Load(ctx context.Context) (map[string]any, error) {
	if s.failNext.Swap(false) {
		return nil, errors.New("temporary read failure")
	}
	return s.watchedSource.Load(ctx)
}

func TestWatchRetriesFailedReloadWithoutAnotherEvent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &transientReadSource{watchedSource: &watchedSource{data: map[string]any{"port": 1}}}
		c, err := conf.Open[typed](t.Context(), conf.WithSources(source), conf.WithWatch())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		synctest.Wait()
		events, cancel := c.Subscribe()
		defer cancel()
		source.failNext.Store(true)
		source.set(map[string]any{"port": 2})
		synctest.Wait()
		assertFailure(t, (<-events).Err, conf.StageRead)
		if c.Current().Port != 1 {
			t.Fatal("failed read published")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if c.Current().Port != 2 {
			t.Fatal("transient read failure permanently lost the only update")
		}
		calls, _ := source.counts()
		time.Sleep(3 * time.Second)
		synctest.Wait()
		if after, _ := source.counts(); after != calls {
			t.Fatal("successful recovery left a periodic retry running")
		}
	})
}

// Hides the parent's internal cancellation key so context uses the public
// AfterFunc hook. Count outstanding registrations without inspecting internals.
type trackedContext struct {
	context.Context
	active atomic.Int64
}

func (*trackedContext) Value(any) any { return nil }
func (c *trackedContext) AfterFunc(f func()) func() bool {
	c.active.Add(1)
	stop := context.AfterFunc(c.Context, f)
	return func() bool {
		if stop() {
			c.active.Add(-1)
			return true
		}
		return false
	}
}

func TestObserverReleasesContextOnConfigClose(t *testing.T) {
	c, err := conf.Open[typed](t.Context(), conf.WithSources(&fakeSource{data: map[string]any{"port": 1}}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	parent := &trackedContext{Context: t.Context()}
	changes, stop := conf.Observe(parent, c, func(v *typed) int { return v.Port })
	defer stop()
	if parent.active.Load() != 1 {
		t.Fatal("test parent did not track the observer's cancellation registration")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-changes; ok {
		t.Fatal("observer did not close")
	}
	if parent.active.Load() != 0 {
		t.Fatal("observer exited but retained its parent cancellation registration")
	}
}

func TestReloadRemovesAbsentKeys(t *testing.T) {
	data := map[string]any{"port": 1}
	source := conf.SourceFunc(func(context.Context) (map[string]any, error) { return data, nil })
	c, err := conf.Open[typed](t.Context(), conf.WithSources(source))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	old := c.Current()
	data = map[string]any{}
	if err := c.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c.Current().Port != 0 || old.Port != 1 {
		t.Fatal("removed key survived a full rebuild or old snapshot changed")
	}
}

func TestLoadUsesOwnedSourceSnapshots(t *testing.T) {
	type app struct {
		Port int      `mapstructure:"port"`
		Tags []string `mapstructure:"tags"`
	}
	tags := []string{"old"}
	data := map[string]any{"port": 1, "tags": tags}
	source := conf.SourceFunc(func(context.Context) (map[string]any, error) { return data, nil })
	c, err := conf.Open[app](t.Context(), conf.WithSources(source))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	data["port"] = 9
	tags[0] = "changed"
	if err := c.Load(t.Context(), conf.SourceFunc(func(context.Context) (map[string]any, error) {
		return map[string]any{}, nil
	})); err != nil {
		t.Fatal(err)
	}
	if c.Current().Port != 1 || c.Current().Tags[0] != "old" {
		t.Fatal("source mutation escaped into cached layer")
	}
}

// Every accepted object representation must have the same merge semantics.
func TestTypedObjectsMergeRecursively(t *testing.T) {
	type key string
	type object map[key]int
	type app struct {
		Limits struct {
			Read  int `mapstructure:"read"`
			Write int `mapstructure:"write"`
		} `mapstructure:"limits"`
	}
	for name, override := range map[string]any{
		"ordinary": map[string]any{"read": 3},
		"typed":    map[string]int{"read": 3},
		"named":    object{"read": 3},
	} {
		t.Run(name, func(t *testing.T) {
			base := &fakeSource{data: map[string]any{"limits": object{"read": 1, "write": 2}}}
			high := &fakeSource{data: map[string]any{"limits": override}}
			c, err := conf.Open[app](t.Context(), conf.WithSources(base, high))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if got := c.Current().Limits; got.Read != 3 || got.Write != 2 {
				t.Fatalf("object merge lost lower-layer fields: %+v", got)
			}
			if base.data["limits"].(object)["read"] != 1 {
				t.Fatal("merge modified source data")
			}
		})
	}
}

type setupChangeSource struct{ *watchedSource }

func (s setupChangeSource) Watch(ctx context.Context) (conf.Watcher, error) {
	// The update happened after Open's read, but before monitoring was established.
	s.set(map[string]any{"port": 2})
	return s.watchedSource.Watch(ctx)
}

func TestWatchSetupReconcilesMissedUpdate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := setupChangeSource{&watchedSource{data: map[string]any{"port": 1}}}
		c, err := conf.Open[typed](t.Context(), conf.WithSources(source), conf.WithWatch())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		synctest.Wait()
		if c.Current().Port != 2 {
			t.Fatal("update between read and watch was permanently missed")
		}
	})
}

func TestSlowSubscriberReceivesLatestAndCancelIsConcurrent(t *testing.T) {
	source := &fakeSource{data: map[string]any{"port": 1}}
	c, err := conf.Open[typed](t.Context(), conf.WithSources(source))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	events, cancel := c.Subscribe()
	defer cancel()
	for port := 2; port <= 100; port++ {
		source.data = map[string]any{"port": port}
		if err := c.Reload(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if event := <-events; event.Current.Port != 100 {
		t.Fatalf("slow subscriber retained stale value: %d", event.Current.Port)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(cancel)
	}
	wg.Go(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	if _, ok := <-events; ok {
		t.Fatal("cancel did not close subscription")
	}
}
