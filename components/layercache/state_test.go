package layercache

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type result struct {
	value string
	err   error
}

func asyncLoad(c *Typed[string], request context.Context, key string, load Loader[string]) <-chan result {
	ch := make(chan result, 1)
	go func() { v, err := c.GetOrLoad(request, key, load); ch <- result{v, err} }()
	return ch
}

func TestSharedSlowPathIncludesRemoteReads(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRemote()
		readRelease, loadRelease := make(chan struct{}), make(chan struct{})
		r.get = func(context.Context, string) (RemoteEntry, error) { <-readRelease; return RemoteEntry{}, ErrMiss }
		c := newCache[string](t, newLocal(), r)
		var calls atomic.Int64
		load := func(context.Context, string) (string, error) { calls.Add(1); <-loadRelease; return "value", nil }
		const n = 30
		waiters := make([]<-chan result, n)
		for i := range n {
			waiters[i] = asyncLoad(c, ctx, "key", load)
		}
		synctest.Wait()
		readsBeforeRelease := r.reads.Load()
		callsBeforeRead := calls.Load()
		close(readRelease)
		synctest.Wait()
		callsBeforeRelease := calls.Load()
		close(loadRelease)
		results := make([]result, 0, len(waiters))
		for _, ch := range waiters {
			results = append(results, <-ch)
		}
		require.EqualValues(t, 1, readsBeforeRelease, "[ablation:shared_flight]")
		for _, got := range results {
			require.NoError(t, got.err)
			require.Equal(t, "value", got.value)
		}
		require.Zero(t, callsBeforeRead)
		require.EqualValues(t, 1, callsBeforeRelease)
		require.EqualValues(t, 1, r.writes.Load())
		assertIdle(t, c)
	})
}

func TestCallerCancellationDoesNotCancelSharedTask(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		type contextKey struct{}
		c := newCache[string](t, newLocal(), nil)
		request, cancel := context.WithCancel(context.WithValue(ctx, contextKey{}, "tenant"))
		release := make(chan struct{})
		var shared context.Context
		load := func(task context.Context, _ string) (string, error) { shared = task; <-release; return "value", nil }
		first := asyncLoad(c, request, "key", load)
		synctest.Wait()
		second := asyncLoad(c, ctx, "key", load)
		synctest.Wait()
		cancel()
		require.ErrorIs(t, (<-first).err, context.Canceled)
		sharedErr := shared.Err()
		close(release)
		require.NoError(t, sharedErr, "[ablation:caller_detachment]")
		require.Equal(t, "tenant", shared.Value(contextKey{}))
		got := <-second
		require.NoError(t, got.err)
		require.Equal(t, "value", got.value)
		assertIdle(t, c)
	})
}

func TestCancelledWaitersRetainGenerationAndAllowNewFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string](t, newLocal(), nil)
		request, cancel := context.WithCancel(ctx)
		release := make(chan struct{})
		first := asyncLoad(c, request, "key", func(context.Context, string) (string, error) { <-release; return "old", nil })
		synctest.Wait()
		cancel()
		require.ErrorIs(t, (<-first).err, context.Canceled)
		require.NoError(t, c.InvalidateLocal(ctx, "key"))
		next := asyncLoad(c, ctx, "key", func(context.Context, string) (string, error) { return "new", nil })
		synctest.Wait()
		close(release)
		gotNext := <-next
		require.NoError(t, gotNext.err)
		require.Equal(t, "new", gotNext.value, "[ablation:task_reference]")
		synctest.Wait()
		got, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "new", got, "[ablation:task_reference]")
		assertIdle(t, c)
	})
}

func TestMutationsFenceOldLoaders(t *testing.T) {
	for _, operation := range []string{"Set", "Delete", "InvalidateLocal"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newRemote()
				c := newCache[string](t, newLocal(), r)
				release := make(chan struct{})
				old := asyncLoad(c, ctx, "key", func(context.Context, string) (string, error) { <-release; return "old", nil })
				synctest.Wait()
				switch operation {
				case "Set":
					require.NoError(t, c.Set(ctx, "key", "new"))
				case "Delete":
					require.NoError(t, c.Delete(ctx, "key"))
				case "InvalidateLocal":
					require.NoError(t, c.InvalidateLocal(ctx, "key"))
				}
				// A new generation must never join the old blocked task.
				got, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "new", nil })
				require.NoError(t, err)
				require.Equal(t, "new", got, "[ablation:generation_fence]")
				close(release)
				require.ErrorIs(t, (<-old).err, ErrInvalidated, "[ablation:generation_fence]")
				got, err = c.Get(ctx, "key")
				require.NoError(t, err)
				require.Equal(t, "new", got, "[ablation:generation_fence]")
				require.EqualValues(t, 1, r.writes.Load(), "old source result must never reach L2")
				assertIdle(t, c)
			})
		})
	}
}

func TestMutationsFenceRemoteReadSnapshots(t *testing.T) {
	for _, shared := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			r := newRemote()
			release := make(chan struct{})
			r.get = func(context.Context, string) (RemoteEntry, error) {
				<-release
				return RemoteEntry{Data: []byte{'L', 'C', 1, 0, '"', 'o', 'l', 'd', '"'}, ExpiresAt: time.Now().Add(time.Minute)}, nil
			}
			c := newCache[string](t, newLocal(), r)
			old := make(chan error, 1)
			go func() {
				var err error
				if shared {
					_, err = c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "unexpected", nil })
				} else {
					_, err = c.Get(ctx, "key")
				}
				old <- err
			}()
			synctest.Wait()
			// Remote reads must not hold the mutation gate.
			require.NoError(t, c.Delete(ctx, "key"))
			close(release)
			require.ErrorIs(t, <-old, ErrInvalidated)
			_, ok := c.local.Get(c.prefix + "key")
			require.False(t, ok)
			assertIdle(t, c)
		})
	}
}

func TestFlightLimitCountsTasksAndInvalidatedGenerations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string](t, newLocal(), nil, WithMaxConcurrentLoads(1))
		release := make(chan struct{})
		load := func(context.Context, string) (string, error) { <-release; return "old", nil }
		first := asyncLoad(c, ctx, "key", load)
		synctest.Wait()
		second := asyncLoad(c, ctx, "key", load)
		synctest.Wait()
		_, err := c.GetOrLoad(ctx, "other", load)
		require.ErrorIs(t, err, ErrOverloaded)
		require.NoError(t, c.InvalidateLocal(ctx, "key"))
		_, err = c.GetOrLoad(ctx, "key", load)
		require.ErrorIs(t, err, ErrOverloaded)
		close(release)
		require.ErrorIs(t, (<-first).err, ErrInvalidated)
		require.ErrorIs(t, (<-second).err, ErrInvalidated)
		got, err := c.GetOrLoad(ctx, "other", func(context.Context, string) (string, error) { return "ok", nil })
		require.NoError(t, err)
		require.Equal(t, "ok", got)
		assertIdle(t, c)
	})
}

func TestKeyGatesAreIndependentAndCancellable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRemote()
		release := make(chan struct{})
		r.set = func(task context.Context, key string, _ []byte, _ time.Duration) error {
			if key == "4:test:blocked" {
				select {
				case <-release:
				case <-task.Done():
					return task.Err()
				}
			}
			return nil
		}
		c := newCache[string](t, newLocal(), r)
		first := make(chan error, 1)
		go func() { first <- c.Set(ctx, "blocked", "a") }()
		synctest.Wait()
		require.NoError(t, c.Set(ctx, "other", "b"))
		request, cancel := context.WithCancel(ctx)
		waiting := make(chan error, 1)
		go func() { waiting <- c.Delete(request, "blocked") }()
		synctest.Wait()
		cancel()
		require.ErrorIs(t, <-waiting, context.Canceled)
		require.Zero(t, r.deletes.Load())
		close(release)
		require.NoError(t, <-first)
		assertIdle(t, c)
	})
}

func TestDeleteWaitsForAlreadyIssuedBackfill(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRemote()
		release := make(chan struct{})
		r.set = func(context.Context, string, []byte, time.Duration) error { <-release; return nil }
		c := newCache[string](t, newLocal(), r)
		read := asyncLoad(c, ctx, "key", func(context.Context, string) (string, error) { return "value", nil })
		synctest.Wait()
		deleted := make(chan error, 1)
		go func() { deleted <- c.Delete(ctx, "key") }()
		synctest.Wait()
		earlyDeletes := r.deletes.Load()
		close(release)
		require.NoError(t, <-deleted)
		<-read // Overlapping read may finish before or after invalidation.
		require.Zero(t, earlyDeletes, "[ablation:commit_gate]")
		_, ok := c.local.Get(c.prefix + "key")
		require.False(t, ok)
		require.EqualValues(t, 1, r.deletes.Load())
		assertIdle(t, c)
	})
}

func TestCloseCancelsAndWaitsForOwnedWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string](t, newLocal(), nil)
		stopped := make(chan struct{})
		read := asyncLoad(c, ctx, "key", func(task context.Context, _ string) (string, error) {
			<-task.Done()
			close(stopped)
			return "", task.Err()
		})
		synctest.Wait()
		require.NoError(t, c.Close())
		<-stopped
		require.ErrorIs(t, (<-read).err, ErrClosed)
		require.NoError(t, c.Close())
		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, ErrClosed)
		_, err = c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "bad", nil })
		require.ErrorIs(t, err, ErrClosed)
		require.ErrorIs(t, c.Set(ctx, "key", "bad"), ErrClosed)
		require.ErrorIs(t, c.Delete(ctx, "key"), ErrClosed)
		require.ErrorIs(t, c.InvalidateLocal(ctx, "key"), ErrClosed)
		assertIdle(t, c)
	})
}

func TestRemoteTimeoutAndClose(t *testing.T) {
	for _, operation := range []string{"Get", "Set", "Delete"} {
		for _, closeCache := range []bool{false, true} {
			synctest.Test(t, func(t *testing.T) {
				r := newRemote()
				block := func(task context.Context) error { <-task.Done(); return task.Err() }
				r.get = func(task context.Context, _ string) (RemoteEntry, error) { return RemoteEntry{}, block(task) }
				r.set = func(task context.Context, _ string, _ []byte, _ time.Duration) error { return block(task) }
				r.del = func(task context.Context, _ string) error { return block(task) }
				c := newCache[string](t, nil, r, WithRemoteTimeout(50*time.Millisecond))
				result := make(chan error, 1)
				go func() {
					switch operation {
					case "Get":
						_, err := c.Get(ctx, "key")
						result <- err
					case "Set":
						result <- c.Set(ctx, "key", "value")
					case "Delete":
						result <- c.Delete(ctx, "key")
					}
				}()
				synctest.Wait()
				want := context.DeadlineExceeded
				if closeCache {
					require.NoError(t, c.Close())
					want = ErrClosed
				}
				require.ErrorIs(t, <-result, want)
				assertIdle(t, c)
			})
		}
	}
}

func TestSharedTimeoutAndPanicReleaseState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string](t, newLocal(), nil, WithLoadTimeout(100*time.Millisecond))
		_, err := c.GetOrLoad(ctx, "timeout", func(task context.Context, _ string) (string, error) { <-task.Done(); return "", task.Err() })
		require.ErrorIs(t, err, context.DeadlineExceeded)
		_, err = c.GetOrLoad(ctx, "panic", func(context.Context, string) (string, error) { panic("loader failed") })
		var p *PanicError
		require.ErrorAs(t, err, &p)
		require.Equal(t, "loader failed", p.Value)
		require.NotEmpty(t, p.Stack)
		assertIdle(t, c)
		got, err := c.GetOrLoad(ctx, "panic", func(context.Context, string) (string, error) { return "ok", nil })
		require.NoError(t, err)
		require.Equal(t, "ok", got)
	})
	r := newRemote()
	r.set = func(context.Context, string, []byte, time.Duration) error { return errBackend }
	c := newCache[string](t, newLocal(), r, WithErrorHandler(func(error) { panic("hook failed") }))
	_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "ok", nil })
	var p *PanicError
	require.ErrorAs(t, err, &p)
	require.Equal(t, "hook failed", p.Value)
	assertIdle(t, c)
}

func TestRemoteWriteTimeoutStillReturnsSourceValue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRemote()
		r.set = func(task context.Context, _ string, _ []byte, _ time.Duration) error {
			<-task.Done()
			return task.Err()
		}
		var reported error
		c := newCache[string](t, newLocal(), r, WithRemoteTimeout(10*time.Millisecond), WithErrorHandler(func(err error) { reported = err }))
		got, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "source", nil })
		require.NoError(t, err)
		require.Equal(t, "source", got)
		require.ErrorIs(t, reported, context.DeadlineExceeded)
		_, ok := c.local.Get(c.prefix + "key")
		require.False(t, ok)
		assertIdle(t, c)
	})
}

func TestCloseReleasesGateWaitersAndDetachedTask(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRemote()
		r.set = func(task context.Context, _ string, _ []byte, _ time.Duration) error {
			<-task.Done()
			return task.Err()
		}
		c := newCache[string](t, newLocal(), r)
		request, cancel := context.WithCancel(ctx)
		read := asyncLoad(c, request, "key", func(context.Context, string) (string, error) { return "source", nil })
		synctest.Wait()
		cancel()
		require.ErrorIs(t, (<-read).err, context.Canceled)
		deleted := make(chan error, 1)
		go func() { deleted <- c.Delete(ctx, "key") }()
		synctest.Wait()
		closed := make(chan error, 2)
		for range 2 {
			go func() { closed <- c.Close() }()
		}
		require.NoError(t, <-closed)
		require.NoError(t, <-closed)
		require.ErrorIs(t, <-deleted, ErrClosed)
		assertIdle(t, c)
	})
}

// A reader admitted during a mutation sees its new generation. It must wait
// for that mutation before taking an L2 snapshot, not merely before committing it.
func TestReadersArrivingDuringMutationCannotResurrectOldValue(t *testing.T) {
	for _, mutation := range []string{"Set", "Delete"} {
		for _, shared := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/shared=%t", mutation, shared), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					r := newRemote()
					c := newCache[string](t, newLocal(), r)
					require.NoError(t, c.Set(ctx, "key", "old"))
					release := make(chan struct{})
					r.set = func(_ context.Context, key string, data []byte, ttl time.Duration) error {
						<-release
						r.mu.Lock()
						r.data[key] = RemoteEntry{Data: data, ExpiresAt: time.Now().Add(ttl)}
						r.mu.Unlock()
						return nil
					}
					r.del = func(_ context.Context, key string) error {
						<-release
						r.mu.Lock()
						delete(r.data, key)
						r.mu.Unlock()
						return nil
					}
					written := make(chan error, 1)
					go func() {
						if mutation == "Set" {
							written <- c.Set(ctx, "key", "new")
						} else {
							written <- c.Delete(ctx, "key")
						}
					}()
					synctest.Wait()
					read := make(chan result, 1)
					go func() {
						var value string
						var err error
						if shared {
							value, err = c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "", ErrNotFound })
						} else {
							value, err = c.Get(ctx, "key")
						}
						read <- result{value, err}
					}()
					synctest.Wait()
					close(release)
					require.NoError(t, <-written)
					got := <-read
					if mutation == "Set" {
						require.NoError(t, got.err)
						require.Equal(t, "new", got.value, "[ablation:read_admission]")
					} else if shared {
						require.ErrorIs(t, got.err, ErrNotFound, "[ablation:read_admission]")
					} else {
						require.ErrorIs(t, got.err, ErrMiss, "[ablation:read_admission]")
					}
					if entry, ok := c.local.Get(c.prefix + "key"); ok {
						require.NotEqual(t, "old", string(entry.Data[4:]))
					}
					assertIdle(t, c)
				})
			})
		}
	}
}

func TestCloseCancelsReadersWaitingForMutationAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRemote()
		r.set = func(task context.Context, _ string, _ []byte, _ time.Duration) error {
			<-task.Done()
			return task.Err()
		}
		c := newCache[string](t, newLocal(), r)
		written := make(chan error, 1)
		go func() { written <- c.Set(ctx, "key", "new") }()
		synctest.Wait()
		read := make(chan error, 1)
		go func() { _, err := c.Get(ctx, "key"); read <- err }()
		var loaded atomic.Bool
		shared := asyncLoad(c, ctx, "key", func(context.Context, string) (string, error) {
			loaded.Store(true)
			return "source", nil
		})
		synctest.Wait()
		require.NoError(t, c.Close())
		require.ErrorIs(t, <-written, ErrClosed)
		require.ErrorIs(t, <-read, ErrClosed)
		require.ErrorIs(t, (<-shared).err, ErrClosed)
		require.Zero(t, r.reads.Load())
		require.False(t, loaded.Load())
		assertIdle(t, c)
	})
}

func TestCloseWaitsForSynchronousHookAfterWaiterLeaves(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRemote()
		r.set = func(context.Context, string, []byte, time.Duration) error { return errBackend }
		release := make(chan struct{})
		c := newCache[string](t, newLocal(), r, WithErrorHandler(func(error) { <-release }))
		request, cancel := context.WithCancel(ctx)
		read := asyncLoad(c, request, "key", func(context.Context, string) (string, error) { return "value", nil })
		synctest.Wait()
		cancel()
		require.ErrorIs(t, (<-read).err, context.Canceled)
		closed := make(chan error, 1)
		go func() { closed <- c.Close() }()
		synctest.Wait()
		returned := false
		select {
		case <-closed:
			returned = true
			t.Error("[ablation:close_drain] Close returned while the shared task was still in its hook")
		default:
		}
		close(release)
		if !returned {
			require.NoError(t, <-closed)
		}
		assertIdle(t, c)
	})
}

type pausedLocal struct {
	Local
	once    sync.Once
	release chan struct{}
}

func (l *pausedLocal) Set(key string, entry Entry, ttl time.Duration) {
	l.once.Do(func() { <-l.release })
	l.Local.Set(key, entry, ttl)
}

func TestLocalOnlyReadRechecksAfterMutationAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		local := &pausedLocal{Local: newLocal(), release: make(chan struct{})}
		c := newCache[string](t, local, nil)
		written := make(chan error, 1)
		go func() { written <- c.Set(ctx, "key", "new") }()
		synctest.Wait()
		var calls atomic.Int64
		read := asyncLoad(c, ctx, "key", func(context.Context, string) (string, error) {
			calls.Add(1)
			return "old", nil
		})
		synctest.Wait()
		close(local.release)
		require.NoError(t, <-written)
		got := <-read
		require.NoError(t, got.err)
		require.Equal(t, "new", got.value, "[ablation:local_recheck]")
		require.Zero(t, calls.Load())
		value, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "new", value)
	})
}

// These tests also serve as ablation oracles. Release work before asserting so
// a deliberately removed guard yields an assertion failure, not a stuck cleanup.
func TestMechanismTaskBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string](t, newLocal(), nil, WithMaxConcurrentLoads(1))
		var calls atomic.Int64
		load := func(task context.Context, _ string) (string, error) {
			calls.Add(1)
			<-task.Done()
			return "", task.Err()
		}
		first := asyncLoad(c, ctx, "first", load)
		synctest.Wait()
		second := asyncLoad(c, ctx, "second", load)
		synctest.Wait()
		require.NoError(t, c.Close())
		<-first
		got := <-second
		require.EqualValues(t, 1, calls.Load(), "[ablation:task_bound]")
		require.ErrorIs(t, got.err, ErrOverloaded)
	})
}

func TestMechanismCloseCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string](t, newLocal(), nil)
		release := make(chan struct{})
		var task context.Context
		read := asyncLoad(c, ctx, "key", func(shared context.Context, _ string) (string, error) {
			task = shared
			<-release
			return "", shared.Err()
		})
		synctest.Wait()
		closed := make(chan error, 1)
		go func() { closed <- c.Close() }()
		synctest.Wait()
		cancelled := task.Err()
		close(release)
		require.NoError(t, <-closed)
		<-read
		require.ErrorIs(t, cancelled, context.Canceled, "[ablation:close_cancel]")
		_, err := c.Get(ctx, "key")
		require.ErrorIs(t, err, ErrClosed)
	})
}

func TestMechanismInvalidationDoesNotWaitForRemoteRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRemote()
		r.get = func(task context.Context, _ string) (RemoteEntry, error) {
			<-task.Done()
			return RemoteEntry{}, task.Err()
		}
		c := newCache[string](t, nil, r)
		read := make(chan error, 1)
		go func() { _, err := c.Get(ctx, "key"); read <- err }()
		synctest.Wait()
		request, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()
		err := c.Delete(request, "key")
		require.NoError(t, c.Close())
		<-read
		require.NoError(t, err, "[ablation:serialize_remote_read] a blocked remote read must not prevent invalidation")
	})
}

func TestMechanismIndependentKeys(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRemote()
		r.set = func(task context.Context, key string, _ []byte, _ time.Duration) error {
			if key == "4:test:blocked" {
				<-task.Done()
				return task.Err()
			}
			return nil
		}
		c := newCache[string](t, nil, r)
		first := make(chan error, 1)
		go func() { first <- c.Set(ctx, "blocked", "first") }()
		synctest.Wait()
		request, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()
		err := c.Set(request, "independent", "second")
		require.NoError(t, c.Close())
		<-first
		require.NoError(t, err, "[ablation:one_global_gate] one key must not consume another key's wait budget")
	})
}

func TestMechanismFiniteBudgets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRemote()
		var remoteDeadline, loadDeadline time.Time
		r.get = func(task context.Context, _ string) (RemoteEntry, error) {
			remoteDeadline, _ = task.Deadline()
			return RemoteEntry{}, ErrMiss
		}
		c := newCache[string](t, nil, r)
		start := time.Now()
		_, err := c.GetOrLoad(ctx, "key", func(task context.Context, _ string) (string, error) {
			loadDeadline, _ = task.Deadline()
			return "", ErrNotFound
		})
		require.ErrorIs(t, err, ErrNotFound)
		require.Equal(t, start.Add(DefaultLoadTimeout), loadDeadline, "[ablation:task_timeout]")
		require.Equal(t, start.Add(DefaultRemoteTimeout), remoteDeadline, "[ablation:remote_timeout]")
	})
}

func TestMechanismLocalTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRemote()
		c := newCache[string](t, newLocal(), r, WithLocalTTL(10*time.Millisecond), WithRemoteTTL(time.Second))
		require.NoError(t, c.Set(ctx, "key", "value"))
		time.Sleep(10 * time.Millisecond)
		got, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "value", got)
		require.EqualValues(t, 1, r.reads.Load(), "[ablation:local_ttl_cap] L1 TTL must force revalidation through L2")
	})
}
