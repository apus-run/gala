package conf_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	conf "github.com/apus-run/gala/components/conf/v2"
)

type watchFunc func(context.Context) (<-chan struct{}, error)

func (f watchFunc) Watch(ctx context.Context) (<-chan struct{}, error) { return f(ctx) }

func TestRunCoalescesAndRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		ch1, ch2 := make(chan struct{}, 4), make(chan struct{}, 4)
		var reads atomic.Int32
		value := 1
		failure := errors.New("temporarily unavailable")
		source := conf.SourceFunc(func(context.Context) (map[string]any, error) {
			if reads.Add(1) == 2 {
				return nil, failure
			}
			return map[string]any{"Value": value}, nil
		})
		cfg, err := conf.Open(ctx, struct{ Value int }{}, source)
		if err != nil {
			t.Fatal(err)
		}
		var failures atomic.Int32
		result := make(chan error, 1)
		go func() {
			result <- conf.Run(ctx, cfg, conf.RunOptions{Debounce: 10 * time.Millisecond, RetryInitial: time.Second, RetryMax: time.Second, OnError: func(err error) {
				if !errors.Is(err, failure) {
					t.Error(err)
				}
				failures.Add(1)
			}},
				watchFunc(func(context.Context) (<-chan struct{}, error) { return ch1, nil }), watchFunc(func(context.Context) (<-chan struct{}, error) { return ch2, nil }))
		}()
		synctest.Wait()
		value = 2
		ch1 <- struct{}{}
		synctest.Wait()
		time.Sleep(7 * time.Millisecond)
		ch2 <- struct{}{}
		synctest.Wait()
		time.Sleep(6 * time.Millisecond)
		synctest.Wait()
		if reads.Load() != 1 {
			t.Fatal("reloaded during burst", reads.Load())
		}
		time.Sleep(10 * time.Millisecond)
		synctest.Wait()
		if failures.Load() != 1 || reads.Load() != 2 || cfg.Current().Value != 1 {
			t.Fatal(failures.Load(), reads.Load(), cfg.Current())
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if reads.Load() != 3 || cfg.Current().Value != 2 {
			t.Fatal("did not retry", reads.Load())
		}
		cancel()
		if !errors.Is(<-result, context.Canceled) {
			t.Fatal("cancellation not returned")
		}
	})
}
func TestRunRollbackAndLastError(t *testing.T) {
	cfg, e := conf.OpenTree(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	denied := errors.New("watch failed")
	var started context.Context
	w := watchFunc(func(ctx context.Context) (<-chan struct{}, error) { started = ctx; return make(chan struct{}), nil })
	if e = conf.Run(t.Context(), cfg, conf.RunOptions{}, w, watchFunc(func(context.Context) (<-chan struct{}, error) { return nil, denied })); !errors.Is(e, denied) || started.Err() == nil {
		t.Fatal(e)
	}
	s := &fakeSource{}
	c, e := conf.Open(t.Context(), typed{}, s)
	if e != nil {
		t.Fatal(e)
	}
	s.err = denied
	w = func(context.Context) (<-chan struct{}, error) {
		ch := make(chan struct{}, 1)
		ch <- struct{}{}
		close(ch)
		return ch, nil
	}
	if e = conf.Run(t.Context(), c, conf.RunOptions{}, w); !errors.Is(e, denied) {
		t.Fatal(e)
	}
}

func TestOptional(t *testing.T) {
	ctx := t.Context()
	src := &fakeSource{err: fmt.Errorf("missing: %w", fs.ErrNotExist)}
	c, e := conf.Open(ctx, typed{Port: 8}, conf.Optional(src))
	if e != nil || c.Current().Port != 8 {
		t.Fatal(e)
	}
	old := c.Current()
	other := errors.New("decoder failed")
	for _, failure := range []error{fs.ErrPermission, other, fmt.Errorf("read: %w", errors.Join(fs.ErrNotExist, other))} {
		src.err = failure
		if e = c.Reload(ctx); !errors.Is(e, failure) || c.Current() != old {
			t.Fatal(e)
		}
	}
	if _, e = conf.Open(ctx, typed{}, conf.Optional((*fakeSource)(nil))); e == nil {
		t.Fatal("accepted nil")
	}
}

func TestRunCancelDuringRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		signals := make(chan struct{}, 1)
		signals <- struct{}{}
		var reads atomic.Int32
		src := conf.SourceFunc(func(context.Context) (map[string]any, error) {
			if reads.Add(1) > 1 {
				return nil, errors.New("load failed")
			}
			return nil, nil
		})
		cfg, err := conf.OpenTree(ctx, src)
		if err != nil {
			t.Fatal(err)
		}
		failed := make(chan struct{}, 1)
		done := make(chan error, 1)
		go func() {
			done <- conf.Run(ctx, cfg, conf.RunOptions{OnError: func(error) { failed <- struct{}{} }}, watchFunc(func(context.Context) (<-chan struct{}, error) { return signals, nil }))
		}()
		<-failed
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) || reads.Load() != 2 {
			t.Fatal(err, reads.Load())
		}
	})
}
