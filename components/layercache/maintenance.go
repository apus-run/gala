package layercache

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

const (
	statLocalHit = iota
	statLocalMiss
	statRemoteHit
	statRemoteMiss
	statLoad
	statLoadError
	statRefresh
	statRefreshError
	statDroppedEvent
)

func (c *Cache) incr(n int) {
	if !c.opts.statsDisabled {
		c.metrics[n].Add(1)
	}
}
func (c *Cache) Stats() Stats {
	return Stats{LocalHits: c.metrics[statLocalHit].Load(), LocalMisses: c.metrics[statLocalMiss].Load(), RemoteHits: c.metrics[statRemoteHit].Load(), RemoteMisses: c.metrics[statRemoteMiss].Load(), Loads: c.metrics[statLoad].Load(), LoadErrors: c.metrics[statLoadError].Load(), Refreshes: c.metrics[statRefresh].Load(), RefreshErrors: c.metrics[statRefreshError].Load(), DroppedEvents: c.metrics[statDroppedEvent].Load()}
}

type refreshTask struct {
	key        string
	item       ItemOptions
	lastAccess time.Time
	busy       bool
}

func (c *Cache) TaskSize() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.refresh) }

// Caller holds the key gate. Never retain the caller's output pointer in a task.
func (c *Cache) registerLocked(key string, i ItemOptions) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.workCtx.Err() != nil {
		return ErrClosed
	}
	if job := c.refresh[key]; job != nil {
		job.lastAccess = time.Now()
		return nil
	}
	if len(c.refresh) >= c.opts.maxRefreshTasks {
		return ErrOverloaded
	}
	i.value = nil
	i.refresh = false
	c.refresh[key] = &refreshTask{key: key, item: i, lastAccess: time.Now()}
	return nil
}
func (c *Cache) startMaintenance() {
	if c.opts.eventHandler != nil {
		c.events = make(chan Event, c.opts.eventBuffer)
		c.wg.Add(1)
		go c.eventLoop()
	}
	if c.opts.refreshInterval > 0 {
		c.wg.Add(1)
		go c.refreshLoop()
	}
	if c.opts.statsHandler != nil && !c.opts.statsDisabled {
		c.wg.Add(1)
		go c.statsLoop()
	}
}
func (c *Cache) backgroundError(err error) {
	if err != nil && c.workCtx.Err() == nil && !errors.Is(err, ErrInvalidated) {
		_ = safeCall(func() error { c.report(err); return nil })
	}
}
func (c *Cache) send(kind EventType, keys ...string) {
	if c.opts.eventHandler == nil || c.workCtx.Err() != nil {
		return
	}
	event := Event{c.opts.namespace, c.opts.sourceID, kind, slices.Clone(keys)}
	select {
	case c.events <- event:
	default:
		c.incr(statDroppedEvent)
	}
}
func (c *Cache) eventLoop() {
	defer c.wg.Done()
	for {
		select {
		case <-c.workCtx.Done():
			return
		case e := <-c.events:
			if c.workCtx.Err() != nil {
				return
			}
			err := safeCall(func() error { return c.opts.eventHandler(c.workCtx, e) })
			c.backgroundError(err)
		}
	}
}
func (c *Cache) refreshLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.opts.refreshInterval)
	defer ticker.Stop()
	slots := make(chan struct{}, c.opts.refreshConcurrency)
	for {
		select {
		case <-c.workCtx.Done():
			return
		case <-ticker.C:
			c.dispatchRefresh(slots)
		}
	}
}
func (c *Cache) statsLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.opts.statsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.workCtx.Done():
			return
		case <-ticker.C:
			if c.workCtx.Err() != nil {
				return
			}
			c.backgroundError(safeCall(func() error { return c.opts.statsHandler(c.workCtx, c.Stats()) }))
		}
	}
}
func (c *Cache) dispatchRefresh(slots chan struct{}) {
	c.mu.Lock()
	jobs := make([]*refreshTask, 0, len(c.refresh))
	now := time.Now()
	for key, job := range c.refresh {
		if now.Sub(job.lastAccess) >= c.opts.refreshIdle {
			delete(c.refresh, key)
		} else if !job.busy {
			jobs = append(jobs, job)
		}
	}
	c.mu.Unlock()
	// Waiting for a slot is cancellable and never holds mu. This bounds work
	// without dropping all but the first few keys of every refresh tick.
	for _, job := range jobs {
		select {
		case slots <- struct{}{}:
		case <-c.workCtx.Done():
			return
		}
		c.mu.Lock()
		if c.refresh[job.key] != job || job.busy || time.Since(job.lastAccess) >= c.opts.refreshIdle {
			c.mu.Unlock()
			<-slots
			continue
		}
		job.busy = true
		i := job.item
		c.mu.Unlock()
		t := c.retain(job.key)
		after := func(err error) {
			c.mu.Lock()
			job.busy = false
			c.mu.Unlock()
			<-slots
			if err != nil && !c.IsNotFound(err) && !errors.Is(err, ErrInvalidated) {
				c.incr(statRefreshError)
				c.backgroundError(err)
			}
		}
		_, err := c.startTask(c.workCtx, fmt.Sprintf("refresh/%p", job), []*ticket{t}, func(ctx context.Context) (any, error) { return c.refreshOne(ctx, t, job, i) }, after)
		c.release(t)
		if err != nil {
			after(err)
		}
	}
}
func (c *Cache) refreshOne(ctx context.Context, t *ticket, job *refreshTask, i ItemOptions) (any, error) {
	if err := c.lockKey(ctx, t); err != nil {
		return nil, err
	}
	err := c.checkGeneration(t)
	c.mu.Lock()
	valid := c.refresh[t.key] == job
	c.mu.Unlock()
	unlockKey(t)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, ErrInvalidated
	}
	c.incr(statRefresh)
	if c.remote != nil {
		remote := c.remote.(LeaseRemote)
		op, cancel := context.WithTimeout(ctx, c.opts.remoteTimeout)
		// A reserved domain cannot collide with data keys, which start with the
		// decimal namespace length. Lease expiry, not unconditional DEL, releases it.
		deadline, acquired, err := remote.AcquireLease(op, "\x00layercache:refresh:"+t.key, c.opts.sourceID, c.opts.refreshInterval)
		if x := c.contextError(op); x != nil {
			err = x
		}
		cancel()
		if err != nil {
			return nil, err
		}
		if !acquired {
			if c.local == nil || i.skipLocal {
				return nil, nil
			}
			delay := c.opts.refreshInterval / 5
			if end, ok := ctx.Deadline(); ok {
				remaining := time.Until(end)
				if remaining <= 0 {
					return nil, context.DeadlineExceeded
				}
				delay = min(delay, remaining/2)
			}
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
			}
			return nil, c.refreshLocal(ctx, t)
		}
		if deadline.IsZero() {
			return nil, ErrInvalidTTL
		}
		bounded, stop := context.WithDeadline(ctx, deadline)
		defer stop()
		ctx = bounded
	}
	if err := c.checkGeneration(t); err != nil {
		return nil, err
	}
	data, err := c.load(ctx, t.key[len(c.prefix):], t, i, true)
	if c.IsNotFound(err) {
		return nil, nil
	}
	return data, err
}

// Follower synchronization is a publication too: older reads/loaders must not
// overwrite the observed L2 state. A miss clears L1 without creating a negative.
func (c *Cache) refreshLocal(ctx context.Context, t *ticket) error {
	e, readErr := c.readCache(ctx, t, true)
	if readErr != nil && !errors.Is(readErr, ErrMiss) {
		return readErr
	}
	if err := c.lockKey(ctx, t); err != nil {
		return err
	}
	defer unlockKey(t)
	if err := c.checkGeneration(t); err != nil {
		return err
	}
	c.invalidate(t, false)
	if readErr == nil {
		c.setLocal(t.key, e, e.ExpiresAt)
	} else {
		c.removeLocal(t.key)
	}
	return nil
}

// Absence is a valid refreshed state even with negative storage disabled.
// Remove the superseded positive entry while retaining the refresh registration.
func (c *Cache) refreshAbsent(ctx context.Context, t *ticket) error {
	if err := c.lockKey(ctx, t); err != nil {
		return err
	}
	defer unlockKey(t)
	if err := c.checkGeneration(t); err != nil {
		return err
	}
	c.invalidate(t, false)
	c.removeLocal(t.key)
	committed, err := c.deleteRemote(ctx, t.key)
	if committed {
		c.send(EventRefresh, t.key[len(c.prefix):])
	}
	return err
}
