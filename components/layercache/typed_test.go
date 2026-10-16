package layercache

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type featureRemote struct {
	*memRemote
	batchReads, batchWrites, leaseWins atomic.Int64
	beforeBatchWrite                   func(context.Context)
}

func newFeatureRemote() *featureRemote { return &featureRemote{memRemote: newRemote()} }
func (r *featureRemote) SetIf(ctx context.Context, key string, data []byte, ttl time.Duration, mode SetMode) (time.Time, bool, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.data[key]
	ok = ok && time.Now().Before(old.ExpiresAt)
	if mode == SetIfAbsent && ok || mode == SetIfPresent && !ok {
		return time.Time{}, false, nil
	}
	deadline := time.Now().Add(ttl)
	r.data[key] = Entry{Data: append([]byte(nil), data...), ExpiresAt: deadline}
	if strings.HasPrefix(key, "\x00layercache:refresh:") {
		r.leaseWins.Add(1)
	}
	return deadline, true, nil
}
func (r *featureRemote) MGet(ctx context.Context, keys []string) (map[string]Entry, error) {
	r.batchReads.Add(1)
	out := make(map[string]Entry)
	var errs error
	for _, key := range keys {
		e, err := r.Get(ctx, key)
		if err == nil {
			out[key] = e
		} else if !errors.Is(err, ErrMiss) {
			errs = errors.Join(errs, err)
		}
	}
	return out, errs
}
func (r *featureRemote) MSet(ctx context.Context, values map[string][]byte, ttl time.Duration) (map[string]time.Time, error) {
	r.batchWrites.Add(1)
	if r.beforeBatchWrite != nil {
		r.beforeBatchWrite(ctx)
	}
	out := make(map[string]time.Time)
	var errs error
	for key, data := range values {
		deadline, err := r.Set(ctx, key, data, ttl)
		if err == nil {
			out[key] = deadline
		} else {
			errs = errors.Join(errs, err)
		}
	}
	return out, errs
}

func TestBatchCollectsOnlyMissingAndPartitionsNegativeWrites(t *testing.T) {
	r := newFeatureRemote()
	c := newCache[string](t, newLocal(), r, WithNotFoundTTL(time.Second))
	require.NoError(t, c.Set(ctx, "local", "L"))
	require.NoError(t, c.Set(ctx, "remote", "R"))
	require.NoError(t, c.InvalidateLocal(ctx, "remote"))
	calls := 0
	load := func(_ context.Context, keys []string) (map[string]string, error) {
		calls++
		require.Equal(t, []string{"absent", "miss"}, keys)
		return map[string]string{"miss": "M"}, nil
	}
	want := map[string]string{"local": "L", "remote": "R", "miss": "M"}
	for range 2 {
		got, err := c.MGet(ctx, []string{"miss", "remote", "local", "absent", "miss"}, load)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	require.Equal(t, 1, calls)
	require.EqualValues(t, 1, r.batchReads.Load())
	require.EqualValues(t, 2, r.batchWrites.Load())
	_, err := c.Get(ctx, "absent")
	require.ErrorIs(t, err, ErrNotFound)
	assertIdle(t, c)
}

func TestBatchReadErrorDoesNotSuppressUnrelatedMiss(t *testing.T) {
	r := newFeatureRemote()
	r.get = func(_ context.Context, key string) (Entry, error) {
		switch {
		case strings.HasSuffix(key, ":hit"):
			return Entry{wireString("cached"), time.Now().Add(time.Minute)}, nil
		case strings.HasSuffix(key, ":broken"):
			return Entry{}, errBackend
		default:
			return Entry{}, ErrMiss
		}
	}
	c := newCache[string](t, newLocal(), r, WithNotFoundTTL(time.Minute))
	var queried []string
	got, err := c.MGet(ctx, []string{"hit", "missing", "broken"}, func(_ context.Context, keys []string) (map[string]string, error) {
		queried = keys
		return map[string]string{"missing": "loaded"}, nil
	})
	require.ErrorIs(t, err, errBackend)
	require.Equal(t, []string{"missing"}, queried, "[ablation:batch_miss_resolution]")
	require.Equal(t, map[string]string{"hit": "cached", "missing": "loaded"}, got)
	_, found := c.local.Get(c.prefix + "broken")
	require.False(t, found, "a backend error must not become a negative entry")
}

func TestBatchCanonicalMissingSetSharesAndDetachesCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newFeatureRemote()
		c := newCache[string](t, nil, r)
		release := make(chan struct{})
		var calls atomic.Int64
		load := func(task context.Context, keys []string) (map[string]string, error) {
			calls.Add(1)
			<-release
			return map[string]string{"a": "A", "b": "B"}, task.Err()
		}
		firstCtx, cancel := context.WithCancel(ctx)
		first := make(chan error, 1)
		second := make(chan error, 1)
		go func() { _, err := c.MGet(firstCtx, []string{"b", "a"}, load); first <- err }()
		synctest.Wait()
		go func() { _, err := c.MGet(ctx, []string{"a", "b"}, load); second <- err }()
		synctest.Wait()
		cancel()
		require.ErrorIs(t, <-first, context.Canceled)
		observed := calls.Load()
		close(release)
		secondErr := <-second
		require.EqualValues(t, 1, observed, "[ablation:batch_canonical]")
		require.NoError(t, secondErr)
		require.EqualValues(t, 1, r.batchReads.Load())
		assertIdle(t, c)
	})
}
func TestBatchLoaderInvalidatedBeforeCommit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newFeatureRemote()
		c := newCache[string](t, newLocal(), r)
		release := make(chan struct{})
		finished := make(chan error, 1)
		go func() {
			_, err := c.MGet(ctx, []string{"a", "b"}, func(context.Context, []string) (map[string]string, error) {
				<-release
				return map[string]string{"a": "old", "b": "old"}, nil
			})
			finished <- err
		}()
		synctest.Wait()
		require.NoError(t, c.Set(ctx, "a", "new"))
		close(release)
		require.ErrorIs(t, <-finished, ErrInvalidated)
		got, err := c.Get(ctx, "a")
		require.NoError(t, err)
		require.Equal(t, "new", got)
		_, err = c.Get(ctx, "b")
		require.ErrorIs(t, err, ErrMiss)
		require.Zero(t, r.batchWrites.Load())
	})
}
func TestBatchCommitHoldsKeyGates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newFeatureRemote()
		release := make(chan struct{})
		r.beforeBatchWrite = func(context.Context) { <-release }
		c := newCache[string](t, newLocal(), r)
		read := make(chan error, 1)
		go func() {
			_, err := c.MGet(ctx, []string{"a", "b"}, func(context.Context, []string) (map[string]string, error) {
				return map[string]string{"a": "A", "b": "B"}, nil
			})
			read <- err
		}()
		synctest.Wait()
		deleted := make(chan error, 1)
		go func() { deleted <- c.Delete(ctx, "a") }()
		synctest.Wait()
		early := r.deletes.Load()
		close(release)
		require.NoError(t, <-deleted)
		<-read
		require.Zero(t, early, "[ablation:batch_commit_gate]")
		_, err := c.Get(ctx, "a")
		require.ErrorIs(t, err, ErrMiss)
		v, err := c.Get(ctx, "b")
		require.NoError(t, err)
		require.Equal(t, "B", v)
	})
}

type batchRecord struct{ Name string }
type selectiveCodec struct{}

func (selectiveCodec) Marshal(v any) ([]byte, error) {
	if v.(batchRecord).Name == "bad" {
		return nil, errBackend
	}
	return json.Marshal(v)
}
func (selectiveCodec) Unmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
func TestBatchEncodingFailureIsNotNegativeCache(t *testing.T) {
	r := newFeatureRemote()
	c := newCache[batchRecord](t, newLocal(), r, WithCodec(selectiveCodec{}), WithNotFoundTTL(time.Second))
	got, err := c.MGet(ctx, []string{"bad", "good", "missing"}, func(context.Context, []string) (map[string]batchRecord, error) {
		return map[string]batchRecord{"bad": {"bad"}, "good": {"good"}}, nil
	})
	require.ErrorIs(t, err, errBackend)
	require.Equal(t, map[string]batchRecord{"good": {"good"}}, got)
	_, err = c.Get(ctx, "bad")
	require.ErrorIs(t, err, ErrMiss)
	_, err = c.Get(ctx, "missing")
	require.ErrorIs(t, err, ErrNotFound)
}
func TestBatchSourceErrorsDoNotCacheAbsence(t *testing.T) {
	for _, sourceErr := range []error{errBackend, ErrNotFound} {
		c := newCache[string](t, newLocal(), newFeatureRemote(), WithNotFoundTTL(time.Second))
		_, err := c.MGet(ctx, []string{"key"}, func(context.Context, []string) (map[string]string, error) { return nil, sourceErr })
		require.ErrorIs(t, err, sourceErr)
		_, err = c.Get(ctx, "key")
		require.ErrorIs(t, err, ErrMiss)
	}
}
func TestBatchPartialRemoteFailure(t *testing.T) {
	r := newFeatureRemote()
	c := newCache[string](t, newLocal(), r)
	r.set = func(_ context.Context, key string, data []byte, ttl time.Duration) error {
		if strings.HasSuffix(key, ":a") {
			return errBackend
		}
		r.mu.Lock()
		r.data[key] = Entry{data, time.Now().Add(ttl)}
		r.mu.Unlock()
		return nil
	}
	got, err := c.MGet(ctx, []string{"a", "b"}, func(context.Context, []string) (map[string]string, error) {
		return map[string]string{"a": "A", "b": "B"}, nil
	})
	require.ErrorIs(t, err, errBackend)
	require.Len(t, got, 2)
	_, ok := c.local.Get(c.prefix + "a")
	require.False(t, ok)
	_, ok = c.local.Get(c.prefix + "b")
	require.True(t, ok)
}
func TestBatchRemoteReadFailureDoesNotQueryOrigin(t *testing.T) {
	r := newFeatureRemote()
	r.get = func(context.Context, string) (Entry, error) { return Entry{}, errBackend }
	c := newCache[string](t, newLocal(), r)
	called := false
	_, err := c.MGet(ctx, []string{"a"}, func(context.Context, []string) (map[string]string, error) { called = true; return nil, nil })
	require.ErrorIs(t, err, errBackend)
	require.False(t, called)
}
func TestOrderedFacadeBatchAndCacheOnlyRead(t *testing.T) {
	base := newCache[string](t, newLocal(), nil)
	c := NewT[int, string](base.Cache)
	require.NoError(t, c.Set(ctx, "user", 1, "alice"))
	got, err := c.Get(ctx, "user", 1, nil)
	require.NoError(t, err)
	require.Equal(t, "alice", got)
	values, err := c.MGet(ctx, "user", []int{3, 2, 1, 3}, func(_ context.Context, ids []int) (map[int]string, error) {
		require.Equal(t, []int{2, 3}, ids)
		return map[int]string{2: "bob", 3: "carol"}, nil
	})
	require.NoError(t, err)
	require.Len(t, values, 3)
	require.NoError(t, c.Delete(ctx, "user", 2))
	ok, err := c.Exists(ctx, "user", 2)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestOrderedFacadeUsesIDDomainForEveryOperation(t *testing.T) {
	base := newCache[string](t, newLocal(), newFeatureRemote(), WithRefreshInterval(time.Second))
	c := NewT[int, string](base.Cache)
	require.NoError(t, c.Set(ctx, "user", 7, "remote"))
	base.local.Set(base.prefix+"user:7", Entry{Data: wireString("local"), ExpiresAt: time.Now().Add(time.Minute)}, time.Minute)
	got, err := c.GetSkippingLocal(ctx, "user", 7)
	require.NoError(t, err)
	require.Equal(t, "remote", got)
	got, err = c.Get(ctx, "user", 7, nil)
	require.NoError(t, err)
	require.Equal(t, "local", got)
	require.NoError(t, c.InvalidateLocal(ctx, "user", 7))
	got, err = c.Get(ctx, "user", 7, nil)
	require.NoError(t, err)
	require.Equal(t, "remote", got)

	var loadedID int
	got, err = c.GetOrLoad(ctx, "user", 8, func(_ context.Context, id int) (string, error) {
		loadedID = id
		return "loaded", nil
	}, Refresh(true), TTL(time.Minute))
	require.NoError(t, err)
	require.Equal(t, "loaded", got)
	require.Equal(t, 8, loadedID)
	require.Equal(t, 1, base.TaskSize())
	require.NoError(t, c.InvalidateLocal(ctx, "user", 8))
	require.Equal(t, 1, base.TaskSize())
	require.NoError(t, c.Delete(ctx, "user", 8))
	require.Zero(t, base.TaskSize())
	_, err = c.GetOrLoad(ctx, "user", 9, nil)
	require.ErrorIs(t, err, ErrInvalidOptions)
}
func TestOnceOptionsAndConditionalWrites(t *testing.T) {
	for _, remote := range []Remote{nil, newFeatureRemote()} {
		c := newCache[string](t, newLocal(), remote)
		require.NoError(t, c.Cache.Set(ctx, "key", Value("old"), SetNX(true)))
		require.ErrorIs(t, c.Cache.Set(ctx, "key", Value("candidate"), SetNX(true)), ErrNotStored)
		got, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "old", got, "[ablation:conditional_l1]")
		require.NoError(t, c.Cache.Set(ctx, "key", Value("new"), SetXX(true)))
		require.ErrorIs(t, c.Cache.Set(ctx, "absent", Value("bad"), SetXX(true)), ErrNotStored)
		require.ErrorIs(t, c.Cache.Set(ctx, "key", Value("bad"), SetNX(true), SetXX(true)), ErrInvalidOptions)
		require.ErrorIs(t, c.Cache.Set(ctx, "key", Value("bad"), TTL(0)), ErrInvalidTTL)
	}
}
func TestPerCallTTLAndSkipLocal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newFeatureRemote()
		c := newCache[string](t, newLocal(), r)
		require.NoError(t, c.Set(ctx, "short", "value", TTL(10*time.Millisecond)))
		time.Sleep(10 * time.Millisecond)
		_, err := c.Get(ctx, "short")
		require.ErrorIs(t, err, ErrMiss)
		require.NoError(t, c.Set(ctx, "key", "remote"))
		c.local.Set(c.prefix+"key", Entry{wireString("local"), time.Now().Add(time.Minute)}, time.Minute)
		got, err := c.GetSkippingLocal(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "remote", got)
		got, err = c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "local", got)
	})
}
func TestByteCoreClonesResultsAndDoesNotConfusePlaceholder(t *testing.T) {
	base := newCache[any](t, newLocal(), nil)
	require.NoError(t, base.Cache.Set(ctx, "key", Value([]byte("*"))))
	var first, second []byte
	require.NoError(t, base.Cache.Get(ctx, "key", &first))
	first[0] = 'X'
	require.NoError(t, base.Cache.Get(ctx, "key", &second))
	require.Equal(t, []byte("*"), second)
	a := NewTyped[map[string]int](base.Cache)
	input := map[string]int{"n": 1}
	require.NoError(t, a.Set(ctx, "map", input))
	input["n"] = 2
	value, err := a.Get(ctx, "map")
	require.NoError(t, err)
	require.Equal(t, 1, value["n"])
	value["n"] = 3
	value, err = a.Get(ctx, "map")
	require.NoError(t, err)
	require.Equal(t, 1, value["n"])
}

func (r *featureRemote) AcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (time.Time, bool, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	old, exists := r.data[key]
	if exists && time.Now().Before(old.ExpiresAt) && string(old.Data) != owner {
		return time.Time{}, false, nil
	}
	deadline := time.Now().Add(ttl)
	r.data[key] = Entry{Data: []byte(owner), ExpiresAt: deadline}
	r.leaseWins.Add(1)
	return deadline, true, nil
}

func TestPublishingTaskRecognizesItsOwnGeneration(t *testing.T) {
	c := newCache[string](t, newLocal(), newFeatureRemote())
	got, err := c.GetOrLoad(ctx, "single", func(context.Context, string) (string, error) { return "value", nil })
	require.NoError(t, err, "[ablation:publication_ticket]")
	require.Equal(t, "value", got)
	values, err := c.MGet(ctx, []string{"a", "b"}, func(context.Context, []string) (map[string]string, error) {
		return map[string]string{"a": "A", "b": "B"}, nil
	})
	require.NoError(t, err)
	require.Len(t, values, 2)
}
func TestBatchPublicationFencesEarlierRemoteRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newFeatureRemote()
		c := newCache[string](t, newLocal(), r)
		require.NoError(t, c.Set(ctx, "key", "old"))
		require.NoError(t, c.InvalidateLocal(ctx, "key"))
		release := make(chan struct{})
		var reads atomic.Int64
		r.get = func(_ context.Context, key string) (Entry, error) {
			if reads.Add(1) == 1 {
				r.mu.Lock()
				e := r.data[key]
				r.mu.Unlock()
				<-release
				return e, nil
			}
			return Entry{}, ErrMiss
		}
		old := make(chan error, 1)
		go func() { _, err := c.Get(ctx, "key"); old <- err }()
		synctest.Wait()
		values, err := c.MGet(ctx, []string{"key"}, func(context.Context, []string) (map[string]string, error) {
			return map[string]string{"key": "new"}, nil
		})
		close(release)
		oldErr := <-old
		require.NoError(t, err)
		require.Equal(t, "new", values["key"])
		require.ErrorIs(t, oldErr, ErrInvalidated, "[ablation:batch_publication]")
		value, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "new", value)
	})
}

func TestLocalOnlyUsesLocalDefaultTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string](t, newLocal(), nil, WithLocalTTL(time.Hour), WithRemoteTTL(time.Minute))
		require.NoError(t, c.Set(ctx, "key", "value"))
		time.Sleep(2 * time.Minute)
		v, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "value", v)
	})
}
func TestConfiguredAbsenceAndJitter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		missing := errors.New("no rows")
		r := newFeatureRemote()
		c := newCache[string](t, newLocal(), r, WithErrNotFound(missing), WithNotFoundTTL(time.Second), WithTTLJitter(100*time.Millisecond))
		calls := 0
		load := func(context.Context, string) (string, error) { calls++; return "", missing }
		for range 2 {
			_, err := c.GetOrLoad(ctx, "key", load)
			require.ErrorIs(t, err, missing)
		}
		require.Equal(t, 1, calls)
		r.mu.Lock()
		ttl := time.Until(r.data[c.prefix+"key"].ExpiresAt)
		r.mu.Unlock()
		require.GreaterOrEqual(t, ttl, time.Second)
		require.Less(t, ttl, 1100*time.Millisecond)
		time.Sleep(1100 * time.Millisecond)
		_, err := c.GetOrLoad(ctx, "key", load)
		require.ErrorIs(t, err, missing)
		require.Equal(t, 2, calls)
	})
}

func TestUnsupportedSkipLocalHasNoMutationSideEffects(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string](t, newLocal(), nil, WithRefreshInterval(time.Second))
		_, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "old", nil }, Refresh(true))
		require.NoError(t, err)
		called := false
		err = c.Cache.Set(ctx, "key", SkipLocal(true), Do(func(context.Context) (any, error) { called = true; return "new", nil }))
		require.ErrorIs(t, err, ErrUnsupported)
		require.False(t, called)
		require.Equal(t, 1, c.TaskSize())
		value, err := c.Get(ctx, "key")
		require.NoError(t, err)
		require.Equal(t, "old", value)
		release := make(chan struct{})
		read := asyncLoad(c, ctx, "loading", func(context.Context, string) (string, error) { <-release; return "value", nil })
		synctest.Wait()
		err = c.Cache.Set(ctx, "loading", Value("bad"), SkipLocal(true))
		close(release)
		require.ErrorIs(t, err, ErrUnsupported)
		require.NoError(t, (<-read).err)
	})
}

type blockingBatchCodec struct {
	entered, release chan struct{}
	once             sync.Once
}

func (c *blockingBatchCodec) Marshal(v any) ([]byte, error) { return json.Marshal(v) }
func (c *blockingBatchCodec) Unmarshal(b []byte, v any) error {
	c.once.Do(func() { close(c.entered) })
	<-c.release
	return json.Unmarshal(b, v)
}
func TestCloseWaitsForBatchDecode(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		codec := &blockingBatchCodec{entered: make(chan struct{}), release: make(chan struct{})}
		c := newCache[batchRecord](t, newLocal(), nil, WithCodec(codec))
		require.NoError(t, c.Set(ctx, "key", batchRecord{"value"}))
		read := make(chan error, 1)
		go func() { _, err := c.MGet(ctx, []string{"key"}, nil); read <- err }()
		<-codec.entered
		closed := make(chan error, 1)
		go func() { closed <- c.Close() }()
		synctest.Wait()
		early := false
		select {
		case <-closed:
			early = true
		default:
		}
		close(codec.release)
		<-read
		if !early {
			require.NoError(t, <-closed)
		}
		require.False(t, early, "[ablation:batch_decode_drain]")
	})
}

func TestTypedOptionsPreserveBorrowedSliceAndArgumentPriority(t *testing.T) {
	for _, operation := range []string{"Set", "GetOrLoad"} {
		t.Run(operation, func(t *testing.T) {
			c := newCache[string](t, newLocal(), nil)
			overriddenCalls := 0
			backing := []ItemOption{
				TTL(time.Minute), Value("from-option"), Do(func(context.Context) (any, error) { overriddenCalls++; return "from-option", nil }),
				SkipLocal(true), Refresh(true),
			}
			opts := backing[:3]
			if operation == "Set" {
				require.NoError(t, c.Set(ctx, "key", "explicit", opts...))
				value, err := c.Get(ctx, "key")
				require.NoError(t, err)
				require.Equal(t, "explicit", value)
			} else {
				value, err := c.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "explicit", nil }, opts...)
				require.NoError(t, err)
				require.Equal(t, "explicit", value)
			}
			require.Zero(t, overriddenCalls)
			// These options were outside the supplied slice length. Appending typed
			// Value/Do overrides must not overwrite the caller's spare backing slots.
			var tail ItemOptions
			for _, opt := range backing[3:] {
				opt.apply(&tail)
			}
			require.True(t, tail.skipLocal)
			require.True(t, tail.refresh)
		})
	}
}

func TestItemOptionsValidateAfterLastOverride(t *testing.T) {
	c := newCache[string](t, newLocal(), newFeatureRemote())
	for _, opts := range [][]ItemOption{
		{TTL(0), TTL(time.Second)},
		{SkipLocal(true), SkipLocal(false)},
		{Refresh(true), Refresh(false)},
		{SetNX(true), SetXX(true), SetNX(false)},
	} {
		require.NoError(t, c.Set(ctx, "key", "value", opts...))
	}
	require.ErrorIs(t, c.Set(ctx, "key", "value", TTL(time.Second), TTL(0)), ErrInvalidTTL)
	require.ErrorIs(t, c.Set(ctx, "key", "value", SetNX(true), SetXX(true)), ErrInvalidOptions)
}

func TestItemOptionProfileIsPerCallAndUsesFinalTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string](t, newLocal(), nil)
		profile := ItemOptionFunc(func(o *ItemOptions) {
			// Apply assembles even temporarily invalid values; only the final call
			// configuration is validated against cache defaults and capabilities.
			o.Apply(TTL(0))
			o.Apply(TTL(time.Second))
		})
		require.NoError(t, c.Set(ctx, "short", "one", profile))
		require.NoError(t, c.Set(ctx, "long", "two", profile, TTL(2*time.Second)))
		time.Sleep(time.Second)
		_, err := c.Get(ctx, "short")
		require.ErrorIs(t, err, ErrMiss)
		value, err := c.Get(ctx, "long")
		require.NoError(t, err)
		require.Equal(t, "two", value)
		time.Sleep(time.Second)
		_, err = c.Get(ctx, "long")
		require.ErrorIs(t, err, ErrMiss)
	})
}

func TestNilItemOptionIsRejectedBeforeProfilesRun(t *testing.T) {
	c := newCache[string](t, newLocal(), nil)
	called := false
	profile := ItemOptionFunc(func(o *ItemOptions) { called = true; o.Apply(TTL(time.Second)) })
	var invalid ItemOptionFunc
	require.ErrorIs(t, c.Set(ctx, "key", "value", profile, invalid), ErrInvalidOptions)
	require.False(t, called)
}
