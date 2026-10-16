package layercache

import (
	"context"
	"runtime/debug"
	"strconv"
	"strings"
)

// All coordination state is owned here. Network calls and callbacks never run
// under mu. Key gates are acquired in sorted key order for batch commits.
type keyState struct {
	refs       int
	generation uint64
	gate       chan struct{}
}
type ticket struct {
	key        string
	state      *keyState
	generation uint64
}
type flight struct {
	done  chan struct{}
	value any
	err   error
}

func (c *Cache) begin(ctx context.Context) (func(), error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.workCtx.Err() != nil {
		return nil, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.wg.Add(1)
	return c.wg.Done, nil
}
func (c *Cache) operationContext(ctx context.Context) (context.Context, func()) {
	bound, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.workCtx, cancel)
	return bound, func() { stop(); cancel() }
}
func (c *Cache) contextError(ctx context.Context) error {
	if c.workCtx.Err() != nil {
		return ErrClosed
	}
	return ctx.Err()
}
func (c *Cache) retain(key string) *ticket {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.states[key]
	if s == nil {
		s = &keyState{gate: make(chan struct{}, 1)}
		c.states[key] = s
	}
	s.refs++
	return &ticket{key, s, s.generation}
}
func (c *Cache) release(t *ticket) { c.mu.Lock(); defer c.mu.Unlock(); c.releaseLocked(t) }
func (c *Cache) releaseLocked(t *ticket) {
	t.state.refs--
	if t.state.refs == 0 {
		delete(c.states, t.key)
	}
}
func (c *Cache) lockKey(ctx context.Context, t *ticket) error {
	select {
	case t.state.gate <- struct{}{}:
		if err := c.contextError(ctx); err != nil {
			unlockKey(t)
			return err
		}
		return nil
	case <-ctx.Done():
		return c.contextError(ctx)
	case <-c.workCtx.Done():
		return ErrClosed
	}
}
func unlockKey(t *ticket) { <-t.state.gate }
func (c *Cache) checkGeneration(t *ticket) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.workCtx.Err() != nil {
		return ErrClosed
	}
	if t.state.generation != t.generation {
		return ErrInvalidated
	}
	return nil
}

// Called under the key gate. Task identities include generation, so new callers
// cannot join old work without maintaining a second per-key flight pointer.
func (c *Cache) invalidate(t *ticket, stopRefresh bool) {
	c.mu.Lock()
	t.state.generation++
	t.generation = t.state.generation
	if stopRefresh {
		delete(c.refresh, t.key)
	}
	c.mu.Unlock()
}

func taskKey(kind string, ts []*ticket) string {
	var b strings.Builder
	b.WriteString(kind)
	b.WriteByte('/')
	for _, t := range ts {
		b.WriteString(strconv.Itoa(len(t.key)))
		b.WriteByte(':')
		b.WriteString(t.key)
		b.WriteByte(':')
		b.WriteString(strconv.FormatUint(t.generation, 10))
		b.WriteByte(';')
	}
	return b.String()
}

// startTask implements JetCache's singleflight algorithm with bounded admission,
// independent task references, cancellation isolation and generation fencing.
func (c *Cache) startTask(parent context.Context, kind string, ts []*ticket, fn func(context.Context) (any, error), after func(error)) (*flight, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.workCtx.Err() != nil {
		return nil, ErrClosed
	}
	for _, t := range ts {
		if t.state.generation != t.generation {
			return nil, ErrInvalidated
		}
	}
	id := taskKey(kind, ts)
	if f := c.flights[id]; f != nil {
		return f, nil
	}
	if len(c.flights) >= c.opts.maxConcurrentLoads {
		return nil, ErrOverloaded
	}
	f := &flight{done: make(chan struct{})}
	c.flights[id] = f
	for _, t := range ts {
		t.state.refs++
	} // task references outlive all waiters
	c.wg.Add(1)
	go c.runTask(context.WithoutCancel(parent), id, ts, f, fn, after)
	return f, nil
}
func (c *Cache) runTask(parent context.Context, id string, ts []*ticket, f *flight, fn func(context.Context) (any, error), after func(error)) {
	ctx, cancel := context.WithTimeout(parent, c.opts.loadTimeout)
	stop := context.AfterFunc(c.workCtx, cancel)
	var value any
	var err error
	defer func() {
		if p := recover(); p != nil {
			err = &PanicError{p, debug.Stack()}
			value = nil
		}
		c.mu.Lock()
		if c.workCtx.Err() != nil {
			err = ErrClosed
			value = nil
		} else {
			for _, t := range ts {
				if t.state.generation != t.generation {
					err = ErrInvalidated
					value = nil
					break
				}
			}
			if err != ErrInvalidated && ctx.Err() != nil {
				err = ctx.Err()
				value = nil
			}
		}
		f.value, f.err = value, err
		delete(c.flights, id)
		for _, t := range ts {
			c.releaseLocked(t)
		}
		close(f.done)
		c.mu.Unlock()
		stop()
		cancel()
		if after != nil {
			_ = safeCall(func() error { after(err); return nil })
		}
		c.wg.Done()
	}()
	value, err = fn(ctx)
}
func (c *Cache) wait(ctx context.Context, f *flight) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.workCtx.Done():
		return nil, ErrClosed
	case <-f.done:
		if err := c.contextError(ctx); err != nil {
			return nil, err
		}
		return f.value, f.err
	}
}
func safeCall(fn func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = &PanicError{p, debug.Stack()}
		}
	}()
	return fn()
}

// Close cancels and drains operations, shared tasks, refresh and observer workers.
// It is idempotent and never closes application-owned backends. User callbacks
// must cooperate and must not synchronously call Close or reenter this Cache.
func (c *Cache) Close() error {
	c.mu.Lock()
	c.cancel()
	clear(c.refresh)
	c.mu.Unlock()
	c.wg.Wait()
	return nil
}
