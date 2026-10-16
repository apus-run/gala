package layercache

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRefreshStopsAfterIdleAndRenewsOnAccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string](t, newLocal(), nil, WithRefreshInterval(time.Second), WithRefreshIdleTimeout(2500*time.Millisecond))
		var calls atomic.Int64
		load := func(context.Context, string) (string, error) { return strconv.FormatInt(calls.Add(1), 10), nil }
		_, err := c.GetOrLoad(ctx, "key", load, Refresh(true))
		require.NoError(t, err)
		synctest.Wait()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		require.EqualValues(t, 3, calls.Load())
		require.Equal(t, 1, c.TaskSize())
		_, err = c.GetOrLoad(ctx, "key", load, Refresh(true))
		require.NoError(t, err)
		time.Sleep(2 * time.Second)
		synctest.Wait()
		require.Equal(t, 1, c.TaskSize())
		time.Sleep(time.Second)
		synctest.Wait()
		require.Zero(t, c.TaskSize(), "[ablation:refresh_idle]")
		count := calls.Load()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		require.Equal(t, count, calls.Load())
	})
}
func TestRefreshConcurrencyAndTaskCap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string](t, newLocal(), nil, WithRefreshInterval(time.Second), WithRefreshIdleTimeout(10*time.Second), WithRefreshConcurrency(2), WithMaxRefreshTasks(4))
		var calls [4]atomic.Int64
		var active, peak atomic.Int64
		release := make(chan struct{})
		for n := range 4 {
			_, err := c.GetOrLoad(ctx, strconv.Itoa(n), func(task context.Context, _ string) (string, error) {
				if calls[n].Add(1) > 1 {
					now := active.Add(1)
					defer active.Add(-1)
					for old := peak.Load(); now > old && !peak.CompareAndSwap(old, now); old = peak.Load() {
					}
					select {
					case <-release:
					case <-task.Done():
						return "", task.Err()
					}
				}
				return "value", nil
			}, Refresh(true))
			require.NoError(t, err)
		}
		_, err := c.GetOrLoad(ctx, "overflow", func(context.Context, string) (string, error) { return "bad", nil }, Refresh(true))
		require.ErrorIs(t, err, ErrOverloaded)
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		observed := active.Load()
		close(release)
		synctest.Wait()
		require.EqualValues(t, 2, observed, "[ablation:refresh_concurrency]")
		require.LessOrEqual(t, peak.Load(), int64(2))
		for n := range 4 {
			require.EqualValues(t, 2, calls[n].Load())
		}
	})
}
func TestMutationCancelsRefreshRegistrationAndFencesRunningLoad(t *testing.T) {
	for _, mutation := range []string{"set", "delete"} {
		t.Run(mutation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := newCache[string](t, newLocal(), nil, WithRefreshInterval(time.Second), WithRefreshIdleTimeout(10*time.Second))
				var calls atomic.Int64
				release := make(chan struct{})
				load := func(context.Context, string) (string, error) {
					if calls.Add(1) > 1 {
						<-release
					}
					return "old", nil
				}
				_, err := c.GetOrLoad(ctx, "key", load, Refresh(true))
				require.NoError(t, err)
				synctest.Wait()
				time.Sleep(time.Second)
				synctest.Wait()
				if mutation == "set" {
					require.NoError(t, c.Set(ctx, "key", "new"))
				} else {
					require.NoError(t, c.Delete(ctx, "key"))
				}
				close(release)
				synctest.Wait()
				require.Zero(t, c.TaskSize(), "[ablation:refresh_cancel]")
				got, err := c.Get(ctx, "key")
				if mutation == "set" {
					require.NoError(t, err)
					require.Equal(t, "new", got)
				} else {
					require.ErrorIs(t, err, ErrMiss)
				}
				time.Sleep(2 * time.Second)
				synctest.Wait()
				require.EqualValues(t, 2, calls.Load())
			})
		})
	}
}
func TestRefreshLeaseCoordinatesTwoInstances(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newFeatureRemote()
		opts := []Option{WithRefreshInterval(time.Second), WithRefreshIdleTimeout(10 * time.Second)}
		first := newCache[string](t, newLocal(), r, opts...)
		second := newCache[string](t, newLocal(), r, opts...)
		var calls atomic.Int64
		load := func(context.Context, string) (string, error) { return strconv.FormatInt(calls.Add(1), 10), nil }
		_, err := first.GetOrLoad(ctx, "key", load, Refresh(true))
		require.NoError(t, err)
		_, err = second.GetOrLoad(ctx, "key", load, Refresh(true))
		require.NoError(t, err)
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()
		require.EqualValues(t, 2, calls.Load(), "[ablation:refresh_lease]")
		require.EqualValues(t, 1, r.leaseWins.Load())
		for _, c := range []*Typed[string]{first, second} {
			v, err := c.Get(ctx, "key")
			require.NoError(t, err)
			require.Equal(t, "2", v)
		}
	})
}
func TestCloseCancelsRefreshAndNoMoreWorkRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string](t, newLocal(), nil, WithRefreshInterval(time.Second), WithRefreshIdleTimeout(10*time.Second))
		var calls atomic.Int64
		_, err := c.GetOrLoad(ctx, "key", func(task context.Context, _ string) (string, error) {
			if calls.Add(1) > 1 {
				<-task.Done()
				return "", task.Err()
			}
			return "initial", nil
		}, Refresh(true))
		require.NoError(t, err)
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		require.NoError(t, c.Close())
		require.NoError(t, c.Close())
		require.Zero(t, c.TaskSize())
		assertIdle(t, c)
		count := calls.Load()
		time.Sleep(3 * time.Second)
		synctest.Wait()
		require.Equal(t, count, calls.Load())
	})
}
func TestRefreshFailuresAreReported(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reported := make(chan error, 4)
		c := newCache[string](t, newLocal(), nil, WithRefreshInterval(time.Second), WithErrorHandler(func(err error) { reported <- err }))
		var calls atomic.Int64
		_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) {
			if calls.Add(1) > 1 {
				return "", errBackend
			}
			return "old", nil
		}, Refresh(true))
		require.NoError(t, err)
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		require.ErrorIs(t, <-reported, errBackend)
		require.EqualValues(t, 1, c.Stats().RefreshErrors)
	})
}
func TestEventsAreBoundedNamespacedAndCloseDrainsHandler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newFeatureRemote()
		release := make(chan struct{})
		seen := make(chan Event, 8)
		c := newCache[string](t, newLocal(), r, WithSourceID("instance-1"), WithEventBuffer(1), WithEventHandler(func(task context.Context, e Event) error {
			select {
			case <-release:
			case <-task.Done():
				return task.Err()
			}
			seen <- e
			return nil
		}))
		require.NoError(t, c.Set(ctx, "a", "A"))
		synctest.Wait()
		require.NoError(t, c.Set(ctx, "b", "B"))
		require.NoError(t, c.Set(ctx, "c", "C"))
		require.EqualValues(t, 1, c.Stats().DroppedEvents)
		close(release)
		synctest.Wait()
		for _, key := range []string{"a", "b"} {
			e := <-seen
			require.Equal(t, "test", e.Namespace)
			require.Equal(t, "instance-1", e.SourceID)
			require.Equal(t, EventSet, e.Type)
			require.Equal(t, []string{key}, e.Keys)
		}
		require.NoError(t, c.Close())
		require.NoError(t, c.Close())
	})
	synctest.Test(t, func(t *testing.T) {
		r := newFeatureRemote()
		var exited atomic.Bool
		c := newCache[string](t, newLocal(), r, WithEventHandler(func(task context.Context, _ Event) error { <-task.Done(); exited.Store(true); return task.Err() }))
		require.NoError(t, c.Set(ctx, "key", "value"))
		synctest.Wait()
		require.NoError(t, c.Close())
		require.True(t, exited.Load())
	})
}

func TestSetNotifiesAfterCommitEvenWhenRefreshRegistrationFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		seen := make(chan Event, 4)
		c := newCache[string](t, newLocal(), newFeatureRemote(), WithRefreshInterval(time.Second), WithMaxRefreshTasks(1), WithEventHandler(func(_ context.Context, e Event) error {
			seen <- e
			return nil
		}))
		load := Do(func(context.Context) (any, error) { return "value", nil })
		require.NoError(t, c.Cache.Set(ctx, "occupied", load, Refresh(true)))
		require.ErrorIs(t, c.Cache.Set(ctx, "committed", load, Refresh(true)), ErrOverloaded)
		got, err := c.GetSkippingLocal(ctx, "committed")
		require.NoError(t, err)
		require.Equal(t, "value", got)
		synctest.Wait()
		require.Len(t, seen, 2, "[ablation:set_event_commit]")
		<-seen
		e := <-seen
		require.Equal(t, EventSet, e.Type)
		require.Equal(t, []string{"committed"}, e.Keys)
	})
}

type stalledNegativeRemote struct{ *featureRemote }

func (r *stalledNegativeRemote) MSet(ctx context.Context, values map[string][]byte, ttl time.Duration) (map[string]time.Time, error) {
	for _, data := range values {
		if isNegative(data) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
	}
	return r.featureRemote.MSet(ctx, values, ttl)
}

type delayedBatchReceiptRemote struct{ *featureRemote }

func (r *delayedBatchReceiptRemote) MSet(ctx context.Context, values map[string][]byte, ttl time.Duration) (map[string]time.Time, error) {
	receipts, err := r.featureRemote.MSet(ctx, values, ttl)
	<-ctx.Done()
	return receipts, errors.Join(err, ctx.Err())
}

type delayedWriteReceiptRemote struct{ *memRemote }

func (r *delayedWriteReceiptRemote) SetIf(ctx context.Context, key string, data []byte, ttl time.Duration, _ SetMode) (time.Time, bool, error) {
	deadline, err := r.Set(ctx, key, data, ttl)
	return deadline, err == nil, err
}

func TestConfirmedWriteTimeoutStillNotifies(t *testing.T) {
	for _, operation := range []string{"set", "conditional", "load", "batch", "delete"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				seen := make(chan Event, 4)
				r := &delayedWriteReceiptRemote{newRemote()}
				r.set = func(task context.Context, key string, data []byte, ttl time.Duration) error {
					r.mu.Lock()
					r.data[key] = Entry{Data: data, ExpiresAt: time.Now().Add(ttl)}
					r.mu.Unlock()
					<-task.Done()
					return nil // The backend acknowledges the write despite the late reply.
				}
				r.del = func(task context.Context, key string) error {
					r.mu.Lock()
					delete(r.data, key)
					r.mu.Unlock()
					<-task.Done()
					return nil
				}
				c := newCache[string](t, newLocal(), r, WithRemoteTimeout(20*time.Millisecond), WithEventHandler(func(_ context.Context, e Event) error {
					seen <- e
					return nil
				}))
				var err error
				kind := EventSet
				switch operation {
				case "set":
					err = c.Set(ctx, "key", "value")
				case "conditional":
					err = c.Set(ctx, "key", "value", SetNX(true))
				case "load":
					kind = EventLoad
					var got string
					got, err = c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "value", nil })
					require.Equal(t, "value", got)
				case "batch":
					kind = EventBatch
					_, err = c.MGet(ctx, []string{"key"}, func(context.Context, []string) (map[string]string, error) {
						return map[string]string{"key": "value"}, nil
					})
				case "delete":
					kind = EventDelete
					r.data[c.prefix+"key"] = Entry{Data: wireString("old"), ExpiresAt: time.Now().Add(time.Minute)}
					err = c.Delete(ctx, "key")
				}
				if operation == "load" {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				}
				synctest.Wait()
				marker := "[ablation:write_receipt_event]"
				if operation == "delete" {
					marker = "[ablation:delete_receipt_event]"
				}
				require.Len(t, seen, 1, marker)
				e := <-seen
				require.Equal(t, kind, e.Type)
				require.Equal(t, []string{"key"}, e.Keys)
				_, found := c.local.Get(c.prefix + "key")
				require.False(t, found, "late acknowledgments must not fill L1")
			})
		})
	}
}

func TestBatchNotifiesConfirmedWritesAfterReceiptTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		seen := make(chan Event, 4)
		r := &delayedBatchReceiptRemote{newFeatureRemote()}
		c := newCache[string](t, newLocal(), r, WithLoadTimeout(time.Second), WithRemoteTimeout(20*time.Millisecond), WithEventHandler(func(_ context.Context, e Event) error {
			seen <- e
			return nil
		}))
		_, err := c.MGet(ctx, []string{"committed"}, func(context.Context, []string) (map[string]string, error) {
			return map[string]string{"committed": "value"}, nil
		})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		synctest.Wait()
		require.Len(t, seen, 1, "[ablation:batch_receipt_event]")
		require.Equal(t, []string{"committed"}, (<-seen).Keys)
		_, found := c.local.Get(c.prefix + "committed")
		require.False(t, found, "late receipts must not publish L1")
	})
}

func TestBatchNotifiesCommittedKeysBeforeLaterGroupTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		seen := make(chan Event, 4)
		r := &stalledNegativeRemote{newFeatureRemote()}
		c := newCache[string](t, newLocal(), r, WithNotFoundTTL(time.Minute), WithLoadTimeout(20*time.Millisecond), WithRemoteTimeout(time.Second), WithEventHandler(func(_ context.Context, e Event) error {
			seen <- e
			return nil
		}))
		_, err := c.MGet(ctx, []string{"positive", "negative"}, func(context.Context, []string) (map[string]string, error) {
			return map[string]string{"positive": "committed"}, nil
		})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		synctest.Wait()
		r.mu.Lock()
		_, committed := r.data[c.prefix+"positive"]
		r.mu.Unlock()
		require.True(t, committed)
		require.Len(t, seen, 1, "[ablation:batch_event_partial]")
		e := <-seen
		require.Equal(t, EventBatch, e.Type)
		require.Equal(t, []string{"positive"}, e.Keys)
	})
}

func TestStatsSnapshotObserverAndDisable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		samples := make(chan Stats, 4)
		c := newCache[string](t, newLocal(), nil, WithStatsInterval(time.Second), WithStatsHandler(func(_ context.Context, s Stats) error { samples <- s; return nil }))
		for range 2 {
			_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "v", nil })
			require.NoError(t, err)
		}
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		s := <-samples
		require.EqualValues(t, 1, s.Loads)
		require.Positive(t, s.LocalHits)
		require.NoError(t, c.Close())
		time.Sleep(2 * time.Second)
		synctest.Wait()
		require.Empty(t, samples)
		disabled := newCache[string](t, newLocal(), nil, WithStatsDisabled(true))
		_, err := disabled.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "v", nil })
		require.NoError(t, err)
		require.Equal(t, Stats{}, disabled.Stats())
	})
}

func TestLoadTimeoutStatsAcrossAPIs(t *testing.T) {
	for _, operation := range []string{"set", "once", "batch"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := newCache[string](t, newLocal(), nil, WithLoadTimeout(10*time.Millisecond))
				load := func(task context.Context) (string, error) {
					<-task.Done()
					return "", task.Err()
				}
				var err error
				switch operation {
				case "set":
					err = c.Cache.Set(ctx, "key", Do(func(task context.Context) (any, error) { return load(task) }))
				case "once":
					_, err = c.GetOrLoad(ctx, "key", func(task context.Context, _ string) (string, error) { return load(task) })
				case "batch":
					_, err = c.MGet(ctx, []string{"key"}, func(task context.Context, _ []string) (map[string]string, error) {
						_, err := load(task)
						return nil, err
					})
				}
				require.ErrorIs(t, err, context.DeadlineExceeded)
				synctest.Wait()
				require.EqualValues(t, 1, c.Stats().Loads)
				require.EqualValues(t, 1, c.Stats().LoadErrors, "[ablation:set_load_error]")
			})
		})
	}
}

func TestExpiredLoaderResultCountsAsFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string](t, newLocal(), nil, WithLoadTimeout(10*time.Millisecond))
		_, err := c.GetOrLoad(ctx, "key", func(task context.Context, _ string) (string, error) {
			<-task.Done()
			return "late", nil
		})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		synctest.Wait()
		require.EqualValues(t, 1, c.Stats().Loads)
		require.EqualValues(t, 1, c.Stats().LoadErrors, "[ablation:load_budget_error]")
		_, found := c.local.Get(c.prefix + "key")
		require.False(t, found)
	})
}
func TestObserverPanicDoesNotKillMaintenance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		seen := make(chan error, 4)
		var calls atomic.Int64
		c := newCache[string](t, newLocal(), newFeatureRemote(), WithEventHandler(func(context.Context, Event) error { calls.Add(1); panic("observer") }), WithErrorHandler(func(err error) { seen <- err }))
		require.NoError(t, c.Set(ctx, "one", "v"))
		synctest.Wait()
		require.NoError(t, c.Set(ctx, "two", "v"))
		synctest.Wait()
		require.EqualValues(t, 2, calls.Load())
		for range 2 {
			var p *PanicError
			require.True(t, errors.As(<-seen, &p))
		}
	})
}

func TestLocalInvalidationKeepsFutureRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string](t, newLocal(), nil, WithRefreshInterval(time.Second), WithRefreshIdleTimeout(10*time.Second))
		var calls atomic.Int64
		_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return strconv.FormatInt(calls.Add(1), 10), nil }, Refresh(true))
		require.NoError(t, err)
		require.NoError(t, c.InvalidateLocal(ctx, "key"))
		require.Equal(t, 1, c.TaskSize())
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		v, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "2", v)
	})
}

func TestRefreshPublicationFencesEarlierRemoteRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newFeatureRemote()
		c := newCache[string](t, newLocal(), r, WithRemoteTimeout(2*time.Second), WithRefreshInterval(time.Second), WithRefreshIdleTimeout(10*time.Second))
		require.NoError(t, c.Set(ctx, "key", "old"))
		_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "new", nil }, Refresh(true))
		require.NoError(t, err)
		require.NoError(t, c.InvalidateLocal(ctx, "key"))
		release := make(chan struct{})
		r.get = func(_ context.Context, key string) (Entry, error) {
			r.mu.Lock()
			e := r.data[key]
			r.mu.Unlock()
			<-release
			return e, nil
		}
		read := make(chan error, 1)
		go func() { _, err := c.Get(ctx, "key"); read <- err }()
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		close(release)
		readErr := <-read
		require.ErrorIs(t, readErr, ErrInvalidated, "[ablation:auto_publication]")
		got, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "new", got)
	})
}
func TestRefreshPublicationFencesOlderBatchLoader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newFeatureRemote()
		c := newCache[string](t, newLocal(), r, WithRefreshInterval(time.Second), WithRefreshIdleTimeout(10*time.Second))
		_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "new", nil }, Refresh(true))
		require.NoError(t, err)
		require.NoError(t, c.InvalidateLocal(ctx, "key"))
		require.NoError(t, r.Delete(ctx, c.prefix+"key"))
		release := make(chan struct{})
		read := make(chan error, 1)
		go func() {
			_, err := c.MGet(ctx, []string{"key"}, func(context.Context, []string) (map[string]string, error) {
				<-release
				return map[string]string{"key": "old"}, nil
			})
			read <- err
		}()
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		close(release)
		require.ErrorIs(t, <-read, ErrInvalidated)
		got, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "new", got)
	})
}

type expiredWriteRemote struct {
	*featureRemote
	expire atomic.Bool
}

func (r *expiredWriteRemote) Set(ctx context.Context, key string, data []byte, ttl time.Duration) (time.Time, error) {
	d, err := r.featureRemote.Set(ctx, key, data, ttl)
	if r.expire.Load() {
		d = time.Now()
	}
	return d, err
}
func TestRefreshExpiredReceiptDropsPreviousLocalValue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &expiredWriteRemote{featureRemote: newFeatureRemote()}
		c := newCache[string](t, newLocal(), r, WithRefreshInterval(time.Second), WithRefreshIdleTimeout(10*time.Second))
		require.NoError(t, c.Set(ctx, "key", "old"))
		_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "new", nil }, Refresh(true), TTL(time.Millisecond))
		require.NoError(t, err)
		r.expire.Store(true)
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		time.Sleep(2 * time.Millisecond)
		_, err = c.Get(ctx, "key")
		require.ErrorIs(t, err, ErrMiss)
	})
}
func TestRejectedConditionalWritePreservesRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newFeatureRemote()
		c := newCache[string](t, newLocal(), r, WithRefreshInterval(time.Second), WithRefreshIdleTimeout(10*time.Second))
		var calls atomic.Int64
		_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { calls.Add(1); return "old", nil }, Refresh(true))
		require.NoError(t, err)
		require.ErrorIs(t, c.Cache.Set(ctx, "key", Value("candidate"), SetNX(true)), ErrNotStored)
		require.Equal(t, 1, c.TaskSize())
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		require.EqualValues(t, 2, calls.Load())
	})
}
func TestStatsAreNotBlockedByRefreshAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		samples := make(chan Stats, 10)
		release := make(chan struct{})
		var calls [3]atomic.Int64
		c := newCache[string](t, newLocal(), nil, WithRefreshInterval(time.Second), WithRefreshConcurrency(1), WithRefreshIdleTimeout(10*time.Second), WithStatsInterval(1500*time.Millisecond), WithStatsHandler(func(_ context.Context, s Stats) error { samples <- s; return nil }))
		for n := range 3 {
			_, err := c.GetOrLoad(ctx, strconv.Itoa(n), func(task context.Context, _ string) (string, error) {
				if calls[n].Add(1) > 1 {
					select {
					case <-release:
					case <-task.Done():
						return "", task.Err()
					}
				}
				return "v", nil
			}, Refresh(true))
			require.NoError(t, err)
		}
		synctest.Wait()
		time.Sleep(1500 * time.Millisecond)
		synctest.Wait()
		observed := len(samples)
		close(release)
		require.NoError(t, c.Close())
		require.Positive(t, observed)
	})
}

type delayedLeaseRemote struct {
	*featureRemote
	leaseCalls atomic.Int64
}

func (r *delayedLeaseRemote) SetIf(ctx context.Context, key string, data []byte, ttl time.Duration, mode SetMode) (time.Time, bool, error) {
	if r.leaseCalls.Add(1) == 1 {
		time.Sleep(20 * time.Millisecond)
	}
	return r.featureRemote.SetIf(ctx, key, data, ttl, mode)
}
func TestLeaseLatencyDoesNotSkipHealthyRefreshTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &delayedLeaseRemote{featureRemote: newFeatureRemote()}
		c := newCache[string](t, newLocal(), r, WithRefreshInterval(time.Second), WithRefreshIdleTimeout(10*time.Second))
		var calls atomic.Int64
		_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return strconv.FormatInt(calls.Add(1), 10), nil }, Refresh(true), TTL(1500*time.Millisecond))
		require.NoError(t, err)
		synctest.Wait()
		time.Sleep(2100 * time.Millisecond)
		synctest.Wait()
		require.NoError(t, c.Close())
		require.EqualValues(t, 3, calls.Load())
	})
}

func (r *delayedLeaseRemote) AcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (time.Time, bool, error) {
	if r.leaseCalls.Add(1) == 1 {
		time.Sleep(20 * time.Millisecond)
	}
	return r.featureRemote.AcquireLease(ctx, key, owner, ttl)
}

type lateRejectRemote struct{ *featureRemote }

func (r *lateRejectRemote) SetIf(ctx context.Context, key string, data []byte, ttl time.Duration, mode SetMode) (time.Time, bool, error) {
	<-ctx.Done()
	return time.Time{}, false, nil
}
func TestKnownConditionalRejectionSurvivesCancellationRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &lateRejectRemote{featureRemote: newFeatureRemote()}
		c := newCache[string](t, newLocal(), r, WithRefreshInterval(time.Second))
		_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "old", nil }, Refresh(true))
		require.NoError(t, err)
		err = c.Cache.Set(ctx, "key", Value("new"), SetNX(true))
		require.ErrorIs(t, err, ErrNotStored)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, 1, c.TaskSize())
		value, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "old", value)
	})
}

func TestFollowerDelayFitsTaskBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newFeatureRemote()
		c := newCache[string](t, newLocal(), r, WithLocalTTL(2*time.Minute), WithRefreshInterval(time.Minute), WithRefreshIdleTimeout(2*time.Minute))
		require.NoError(t, c.Set(ctx, "key", "old"))
		_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "should not load", nil }, Refresh(true))
		require.NoError(t, err)
		_, _, err = r.AcquireLease(ctx, "\x00layercache:refresh:"+c.prefix+"key", "other-owner", 10*time.Minute)
		require.NoError(t, err)
		_, err = r.Set(ctx, c.prefix+"key", wireString("new"), 5*time.Minute)
		require.NoError(t, err)
		synctest.Wait()
		time.Sleep(63 * time.Second)
		synctest.Wait()
		value, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "new", value, "[ablation:follower_budget]")
		require.Zero(t, c.Stats().RefreshErrors)
	})
}
func TestFollowerPublicationFencesEarlierRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newFeatureRemote()
		c := newCache[string](t, newLocal(), r, WithRemoteTimeout(3*time.Second), WithRefreshInterval(time.Second), WithRefreshIdleTimeout(10*time.Second))
		require.NoError(t, c.Set(ctx, "key", "old"))
		_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "unexpected", nil }, Refresh(true))
		require.NoError(t, err)
		_, _, err = r.AcquireLease(ctx, "\x00layercache:refresh:"+c.prefix+"key", "other-owner", time.Minute)
		require.NoError(t, err)
		require.NoError(t, c.InvalidateLocal(ctx, "key"))
		release := make(chan struct{})
		var reads atomic.Int64
		r.get = func(_ context.Context, key string) (Entry, error) {
			r.mu.Lock()
			e := r.data[key]
			r.mu.Unlock()
			if reads.Add(1) == 1 {
				<-release
			}
			return e, nil
		}
		read := make(chan error, 1)
		go func() { _, err := c.Get(ctx, "key"); read <- err }()
		synctest.Wait()
		_, err = r.Set(ctx, c.prefix+"key", wireString("new"), time.Minute)
		require.NoError(t, err)
		time.Sleep(time.Second)
		synctest.Wait()
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()
		close(release)
		require.ErrorIs(t, <-read, ErrInvalidated, "[ablation:follower_publication]")
		value, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "new", value)
	})
}
func TestFollowerMissClearsLocalWithoutQueryingSource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newFeatureRemote()
		c := newCache[string](t, newLocal(), r, WithRefreshInterval(time.Second))
		require.NoError(t, c.Set(ctx, "key", "old"))
		var loaded atomic.Bool
		_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { loaded.Store(true); return "unexpected", nil }, Refresh(true))
		require.NoError(t, err)
		_, _, err = r.AcquireLease(ctx, "\x00layercache:refresh:"+c.prefix+"key", "other-owner", time.Minute)
		require.NoError(t, err)
		require.NoError(t, r.Delete(ctx, c.prefix+"key"))
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()
		_, err = c.Get(ctx, "key")
		require.ErrorIs(t, err, ErrMiss)
		require.False(t, loaded.Load())
		require.Zero(t, c.Stats().RefreshErrors)
	})
}

func TestRefreshAbsenceWithoutNegativeStorageRemovesPositive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newFeatureRemote()
		c := newCache[string](t, newLocal(), r, WithRefreshInterval(time.Second), WithRefreshIdleTimeout(10*time.Second))
		require.NoError(t, c.Set(ctx, "key", "old"))
		var exists atomic.Bool
		_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) {
			if exists.Load() {
				return "new", nil
			}
			return "", ErrNotFound
		}, Refresh(true))
		require.NoError(t, err)
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		_, err = c.Get(ctx, "key")
		require.ErrorIs(t, err, ErrMiss, "[ablation:refresh_absence]")
		require.Equal(t, 1, c.TaskSize())
		exists.Store(true)
		time.Sleep(time.Second)
		synctest.Wait()
		v, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "new", v)
	})
}
