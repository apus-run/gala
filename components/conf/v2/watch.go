package conf

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// ErrWatchUnsupported lets a wrapper delegate optional watch support.
var ErrWatchUnsupported = errors.New("source does not support watching")

// Watcher reports invalidation, not configuration values. Next returns nil on
// change and must unblock when ctx is canceled. Close releases owned resources.
// A watch error is retried by opening a fresh watcher after one second.
type Watcher interface {
	Next(context.Context) error
	Close() error
}

// Watchable is optional. Sources without it can be monitored with polling.
// Watch must establish monitoring before returning and honor ctx.
type Watchable interface {
	Watch(context.Context) (Watcher, error)
}

type sourceWatcher struct {
	watcher Watcher
	source  Watchable
	index   int
}
type watchSession struct {
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	trigger     chan struct{}
	failures    chan error
	closeFailed atomic.Bool
}

func openWatchers(ctx context.Context, sources []Source, offset int) ([]sourceWatcher, error) {
	var result []sourceWatcher
	for i, source := range sources {
		if err := ctx.Err(); err != nil {
			closeWatchers(result)
			return nil, newError(StageCanceled, offset+i, "", "", err)
		}
		s, ok := source.(Watchable)
		if !ok {
			continue
		}
		w, err := s.Watch(ctx)
		if ctx.Err() != nil {
			if !isNilSource(w) {
				w.Close()
			}
			closeWatchers(result)
			return nil, newError(StageCanceled, offset+i, "", "", ctx.Err())
		}
		if errors.Is(err, ErrWatchUnsupported) {
			if !isNilSource(w) {
				w.Close()
			}
			continue
		}
		if err == nil && isNilSource(w) {
			err = newConfigError(offset+i, "watcher must not be nil")
		}
		if ctx.Err() != nil || err != nil {
			if !isNilSource(w) {
				w.Close()
			}
			closeWatchers(result)
			if ctx.Err() != nil {
				return nil, newError(StageCanceled, offset+i, "", "", ctx.Err())
			}
			return nil, &Error{Stage: StageWatch, Source: offset + i, reason: "could not start source watcher"}
		}
		result = append(result, sourceWatcher{watcher: w, source: s, index: offset + i})
	}
	return result, nil
}
func closeWatchers(watchers []sourceWatcher) {
	for _, w := range watchers {
		w.watcher.Close()
	}
}

// Start begins optional source watchers and configured polling. Calling it again
// while running is harmless. A canceled session can be started again. A startup
// failure closes all watchers already opened and leaves the instance usable.
func (c *Config[T]) Start(ctx context.Context) error {
	watchCtx, cancel := c.operationContext(ctx)
	started := false
	defer func() {
		if !started {
			cancel()
		}
	}()
	ctx = watchCtx
	if err := c.acquire(ctx); err != nil {
		return err
	}
	defer c.release()
	if c.session != nil {
		if c.session.ctx.Err() == nil {
			return nil
		}
		c.session.wg.Wait()
		if c.session.closeFailed.Load() {
			return &Error{Stage: StageWatch, Source: -1, reason: "could not close source watcher"}
		}
	}
	watchers, err := openWatchers(watchCtx, c.sources, 0)
	if err != nil {
		cancel()
		return err
	}
	if len(watchers) == 0 && c.opts.poll == 0 {
		cancel()
		return newConfigError(-1, "no watchable sources; configure polling")
	}
	session := &watchSession{
		ctx:      watchCtx,
		cancel:   cancel,
		trigger:  make(chan struct{}, 1),
		failures: make(chan error, 1),
	}
	c.session = session
	started = true
	session.startWorkers(watchers)
	session.wg.Go(func() {
		defer session.cancel()
		retryTimer := time.NewTimer(time.Second)
		retryTimer.Stop()
		defer retryTimer.Stop()
		var retry <-chan time.Time
		var ticks <-chan time.Time
		if c.opts.poll > 0 {
			ticker := time.NewTicker(c.opts.poll)
			defer ticker.Stop()
			ticks = ticker.C
		}
		for {
			select {
			case <-watchCtx.Done():
				return
			case err := <-session.failures:
				c.broadcast(Event[T]{Current: c.Current(), Err: err})
				continue
			case <-session.trigger:
			case <-ticks:
			case <-retry:
			}
			if err := c.Reload(watchCtx); err != nil {
				if watchCtx.Err() != nil {
					return
				}
				c.broadcast(Event[T]{Current: c.Current(), Err: err})
				// A transient read failure must not consume the only invalidation.
				retryTimer.Reset(time.Second)
				retry = retryTimer.C
				continue
			}
			retryTimer.Stop()
			retry = nil
		}
	})
	// Reconcile changes that happened between initial loading and watcher setup.
	session.signal()
	return nil
}

func (s *watchSession) signal() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}
func (s *watchSession) report(index int) {
	err := &Error{Stage: StageWatch, Source: index, reason: "source watcher failed; retrying"}
	select {
	case s.failures <- err:
	default:
	}
}
func (s *watchSession) startWorkers(watchers []sourceWatcher) {
	for _, initial := range watchers {
		s.wg.Go(func() {
			w := initial.watcher
			defer func() {
				if !isNilSource(w) {
					s.closeWatcher(w)
				}
			}()
			for {
				if s.ctx.Err() != nil {
					return
				}
				if err := w.Next(s.ctx); err == nil {
					s.signal()
					continue
				}
				if s.ctx.Err() != nil {
					return
				}
				s.report(initial.index)
				s.closeWatcher(w)
				w = nil
				for w == nil {
					timer := time.NewTimer(time.Second)
					select {
					case <-s.ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
					next, err := initial.source.Watch(s.ctx)
					if err != nil || isNilSource(next) {
						if !isNilSource(next) {
							s.closeWatcher(next)
						}
						if s.ctx.Err() != nil {
							return
						}
						s.report(initial.index)
						continue
					}
					w = next
					s.signal() // Recover updates missed while disconnected.
				}
			}
		})
	}
}

func (s *watchSession) closeWatcher(w Watcher) {
	if err := w.Close(); err != nil {
		s.closeFailed.Store(true)
	}
}

// Close is idempotent. It stops monitoring and closes subscriptions, but never
// closes borrowed source clients. Current remains readable; new loads fail.
func (c *Config[T]) Close() error {
	if c.sem == nil {
		return nil
	}
	if !c.closed.CompareAndSwap(false, true) {
		<-c.closeDone
		return c.closeErr
	}
	c.cancelLifetime()
	c.sem <- struct{}{}
	session := c.session
	if session != nil {
		session.cancel()
	}
	c.release()
	if session != nil {
		session.wg.Wait()
		if session.closeFailed.Load() {
			c.closeErr = &Error{Stage: StageWatch, Source: -1, reason: "could not close source watcher"}
		}
	}
	c.subMu.Lock()
	for ch := range c.subscribers {
		close(ch)
		delete(c.subscribers, ch)
	}
	c.subMu.Unlock()
	close(c.closeDone)
	return c.closeErr
}
