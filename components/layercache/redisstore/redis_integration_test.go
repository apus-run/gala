package redisstore

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apus-run/gala/components/ecache"
	"github.com/apus-run/gala/components/layercache"
	msgpackcodec "github.com/apus-run/gala/components/layercache/codec/msgpack"
	soniccodec "github.com/apus-run/gala/components/layercache/codec/sonic"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Opt in with the address reported by compose.test.yaml. Every run uses a fresh
// namespace and removes only its own keys; it never flushes the Redis database.
func TestRedisIntegration(t *testing.T) {
	addr := os.Getenv("LAYERCACHE_REDIS_ADDR")
	if addr == "" {
		t.Skip("set LAYERCACHE_REDIS_ADDR to run against a real Redis server")
	}
	for _, protocol := range []int{2, 3} {
		t.Run(fmt.Sprintf("RESP%d", protocol), func(t *testing.T) {
			client := redis.NewClient(&redis.Options{Addr: addr, Protocol: protocol, ContextTimeoutEnabled: true, DialTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, MaxRetries: 1})
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			runRedisIntegration(t, client)
		})
	}
}

// Both topologies run the same public-API contract tests.
func runRedisIntegration(t *testing.T, client redis.UniversalClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, client.Ping(ctx).Err())
	info, err := client.Info(ctx, "server").Result()
	require.NoError(t, err)
	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "redis_version:") {
			t.Log(strings.TrimSpace(line))
		}
	}
	store, err := New(client)
	require.NoError(t, err)
	namespace := "layercache-integration-" + rand.Text()
	cleanupRedisNamespace(t, client, namespace)
	newCache := func(t *testing.T, suffix string, opts ...layercache.Option) *layercache.Cache {
		return newRedisIntegrationCache(t, store, namespace+suffix, opts...)
	}

	t.Run("read_write_batch_ttl_events", func(t *testing.T) {
		seen := make(chan layercache.Event, 32)
		base := newCache(t, ":core", layercache.WithEventHandler(func(_ context.Context, e layercache.Event) error { seen <- e; return nil }))
		view := layercache.NewTyped[string](base)
		require.NoError(t, view.Set(ctx, "cached", "old"))
		require.ErrorIs(t, view.Set(ctx, "cached", "rejected", layercache.SetNX(true)), layercache.ErrNotStored)
		got, err := view.Get(ctx, "cached")
		require.NoError(t, err)
		require.Equal(t, "old", got)
		require.NoError(t, view.Set(ctx, "cached", "updated", layercache.SetXX(true)))
		require.ErrorIs(t, view.Set(ctx, "absent", "rejected", layercache.SetXX(true)), layercache.ErrNotStored)
		require.NoError(t, view.Set(ctx, "remote", "R"))
		require.NoError(t, view.InvalidateLocal(ctx, "remote"))
		var calls atomic.Int64
		load := func(_ context.Context, keys []string) (map[string]string, error) {
			calls.Add(1)
			if strings.Join(keys, ",") != "absent,missing" {
				return nil, fmt.Errorf("unexpected missing keys: %v", keys)
			}
			return map[string]string{"missing": "M"}, nil
		}
		for range 2 {
			values, err := view.MGet(ctx, []string{"remote", "cached", "absent", "missing", "missing"}, load)
			require.NoError(t, err)
			require.Equal(t, map[string]string{"cached": "updated", "remote": "R", "missing": "M"}, values)
		}
		require.EqualValues(t, 1, calls.Load())
		require.NoError(t, view.InvalidateLocal(ctx, "absent"))
		_, err = view.Get(ctx, "absent")
		require.ErrorIs(t, err, layercache.ErrNotFound)
		broken := strconv.Itoa(len(base.Namespace())) + ":" + base.Namespace() + ":broken"
		require.NoError(t, client.RPush(ctx, broken, "wrong-type").Err())
		partial, err := view.MGet(ctx, []string{"broken", "independent"}, func(_ context.Context, keys []string) (map[string]string, error) {
			if strings.Join(keys, ",") != "independent" {
				return nil, fmt.Errorf("backend failure reached loader: %v", keys)
			}
			return map[string]string{"independent": "available"}, nil
		})
		require.ErrorContains(t, err, "WRONGTYPE")
		require.Equal(t, map[string]string{"independent": "available"}, partial)
		require.Equal(t, "list", client.Type(ctx, broken).Val())
		require.NoError(t, view.Set(ctx, "short", "expires", layercache.TTL(300*time.Millisecond)))
		physical := strconv.Itoa(len(base.Namespace())) + ":" + base.Namespace() + ":short"
		require.Eventually(t, func() bool { n, err := client.Exists(ctx, physical).Result(); return err == nil && n == 0 }, 3*time.Second, 10*time.Millisecond)
		_, err = view.Get(ctx, "short")
		require.ErrorIs(t, err, layercache.ErrMiss, "L1 must expire no later than Redis")
		require.Eventually(t, func() bool { return len(seen) > 0 }, time.Second, 10*time.Millisecond)
		event := <-seen
		require.Equal(t, base.Namespace(), event.Namespace)
		require.Equal(t, base.SourceID(), event.SourceID)
		require.Equal(t, layercache.EventSet, event.Type)
		require.NoError(t, view.Delete(ctx, "cached"))
		_, err = view.Get(ctx, "cached")
		require.ErrorIs(t, err, layercache.ErrMiss)
	})

	for _, codec := range []struct {
		name  string
		value layercache.Codec
	}{{"json", nil}, {"msgpack", msgpackcodec.Codec{}}, {"sonic", soniccodec.Codec{}}} {
		t.Run(codec.name+"_concurrent_load", func(t *testing.T) {
			var opts []layercache.Option
			if codec.value != nil {
				opts = append(opts, layercache.WithCodec(codec.value))
			}
			base := newCache(t, ":"+codec.name, opts...)
			type record struct {
				ID   int
				Name string
			}
			view := layercache.NewTyped[record](base)
			want := record{42, strings.Repeat("value", 40)}
			var calls atomic.Int64
			start := make(chan struct{})
			results := make(chan error, 32)
			var wg sync.WaitGroup
			for range cap(results) {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					got, err := view.GetOrLoad(ctx, "shared", func(task context.Context, _ string) (record, error) {
						calls.Add(1)
						select {
						case <-task.Done():
							return record{}, task.Err()
						case <-time.After(30 * time.Millisecond):
							return want, nil
						}
					})
					if err == nil && got != want {
						err = errors.New("incorrect shared result")
					}
					results <- err
				}()
			}
			close(start)
			wg.Wait()
			close(results)
			for err := range results {
				require.NoError(t, err)
			}
			require.EqualValues(t, 1, calls.Load())
			require.NoError(t, base.InvalidateLocal(ctx, "shared"))
			got, err := view.Get(ctx, "shared")
			require.NoError(t, err)
			require.Equal(t, want, got, "codec must round-trip through Redis")
		})
	}

	t.Run("lease_contention", func(t *testing.T) {
		key := namespace + ":contended-lease"
		winners := make(chan string, 32)
		results := make(chan error, cap(winners))
		var wg sync.WaitGroup
		for n := range cap(winners) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				owner := strconv.Itoa(n)
				deadline, acquired, err := store.AcquireLease(ctx, key, owner, time.Minute)
				if acquired {
					winners <- owner
					if deadline.IsZero() {
						err = errors.New("successful lease has no deadline")
					}
				}
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		for err := range results {
			require.NoError(t, err)
		}
		require.Len(t, winners, 1, "competing owners must not share a live lease")
		owner := <-winners
		_, acquired, err := store.AcquireLease(ctx, key, owner, time.Minute)
		require.NoError(t, err)
		require.True(t, acquired, "the same owner must be able to renew")
		_, acquired, err = store.AcquireLease(ctx, key, "another-owner", time.Minute)
		require.NoError(t, err)
		require.False(t, acquired)
	})

	t.Run("refresh_lease_and_close", func(t *testing.T) {
		options := []layercache.Option{layercache.WithRefreshInterval(time.Second), layercache.WithRefreshIdleTimeout(10 * time.Second)}
		a := newCache(t, ":refresh", options...)
		b := newCache(t, ":refresh", options...)
		av, bv := layercache.NewTyped[string](a), layercache.NewTyped[string](b)
		require.NoError(t, av.Set(ctx, "key", "seed"))
		var calls atomic.Int64
		load := func(context.Context, string) (string, error) { calls.Add(1); return "refreshed", nil }
		_, err := av.GetOrLoad(ctx, "key", load, layercache.Refresh(true))
		require.NoError(t, err)
		_, err = bv.GetOrLoad(ctx, "key", load, layercache.Refresh(true))
		require.NoError(t, err)
		leaseKey := "\x00layercache:refresh:" + strconv.Itoa(len(a.Namespace())) + ":" + a.Namespace() + ":key"
		require.Eventually(t, func() bool {
			x, ex := av.Get(ctx, "key")
			y, ey := bv.Get(ctx, "key")
			owner, leaseErr := client.Get(ctx, leaseKey).Result()
			return ex == nil && ey == nil && x == "refreshed" && y == x && leaseErr == nil && (owner == a.SourceID() || owner == b.SourceID())
		}, 5*time.Second, 10*time.Millisecond)
		require.NoError(t, a.Close())
		require.NoError(t, b.Close())
		require.Positive(t, calls.Load())
		_, err = av.Get(ctx, "key")
		require.ErrorIs(t, err, layercache.ErrClosed)
		require.NoError(t, a.Close())
		require.NoError(t, client.Ping(ctx).Err(), "Close must leave the application-owned client usable")
	})

	t.Run("close_cancels_load", func(t *testing.T) {
		base := newCache(t, ":close", layercache.WithLoadTimeout(30*time.Second))
		view := layercache.NewTyped[string](base)
		started := make(chan struct{})
		exited := make(chan struct{})
		release := make(chan struct{})
		defer close(release) // Release owned work even when the assertion fails.
		finished := make(chan error, 1)
		go func() {
			_, err := view.GetOrLoad(ctx, "key", func(task context.Context, _ string) (string, error) {
				defer close(exited)
				close(started)
				select {
				case <-task.Done():
				case <-release:
				}
				return "", task.Err()
			})
			finished <- err
		}()
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		closed := make(chan error, 1)
		go func() { closed <- base.Close() }()
		select {
		case err := <-closed:
			require.NoError(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("Close did not cancel the loader within 3s; task expiry is 30s")
		}
		select {
		case <-exited:
		default:
			t.Fatal("Close returned before the loader exited")
		}
		select {
		case err := <-finished:
			require.ErrorIs(t, err, layercache.ErrClosed)
		case <-time.After(time.Second):
			t.Fatal("the canceled caller did not return")
		}
		require.NoError(t, client.Ping(ctx).Err())
	})
}

func cleanupRedisNamespace(t *testing.T, client redis.UniversalClient, namespace string) {
	t.Helper()
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		clean := func(ctx context.Context, node redis.Cmdable) error {
			iter := node.Scan(ctx, 0, "*"+namespace+"*", 100).Iterator()
			pipe := node.Pipeline()
			for iter.Next(ctx) {
				pipe.Del(ctx, iter.Val()) // A separate command per key avoids CROSSSLOT.
			}
			if err := iter.Err(); err != nil {
				return err
			}
			_, err := pipe.Exec(ctx)
			return err
		}
		if cluster, ok := client.(*redis.ClusterClient); ok {
			require.NoError(t, cluster.ForEachMaster(cleanup, func(ctx context.Context, node *redis.Client) error { return clean(ctx, node) }))
		} else {
			require.NoError(t, clean(cleanup, client))
		}
	})
}

func newRedisIntegrationCache(t *testing.T, remote layercache.Remote, namespace string, opts ...layercache.Option) *layercache.Cache {
	t.Helper()
	defaults := []layercache.Option{layercache.WithNamespace(namespace), layercache.WithLocal(ecache.New[string, layercache.Entry](ecache.WithMaxKeys(100))), layercache.WithRemote(remote), layercache.WithLocalTTL(2 * time.Second), layercache.WithRemoteTTL(5 * time.Second), layercache.WithRemoteTimeout(time.Second), layercache.WithNotFoundTTL(time.Second)}
	base, err := layercache.New(append(defaults, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, base.Close()) })
	return base
}

func redisIntegrationKey(namespace, key string) string {
	return strconv.Itoa(len(namespace)) + ":" + namespace + ":" + key
}
