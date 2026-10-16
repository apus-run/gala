package redisstore

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apus-run/gala/components/layercache"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRedisClusterIntegration(t *testing.T) {
	raw := os.Getenv("LAYERCACHE_REDIS_CLUSTER_ADDRS")
	if raw == "" {
		t.Skip("set LAYERCACHE_REDIS_CLUSTER_ADDRS to run against a real Redis Cluster")
	}
	addrs := strings.Split(raw, ",")
	for _, protocol := range []int{2, 3} {
		t.Run(fmt.Sprintf("RESP%d", protocol), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			t.Cleanup(cancel)
			client := redis.NewClusterClient(&redis.ClusterOptions{Addrs: addrs, Protocol: protocol, ContextTimeoutEnabled: true, DialTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, MaxRetries: 1})
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			topology, err := client.ClusterSlots(ctx).Result()
			require.NoError(t, err)
			masters := make(map[string]bool)
			total := 0
			for _, slot := range topology {
				require.NotEmpty(t, slot.Nodes)
				masters[slot.Nodes[0].Addr] = true
				total += slot.End - slot.Start + 1
				t.Logf("slots %d-%d -> %s", slot.Start, slot.End, slot.Nodes[0].Addr)
			}
			require.Len(t, masters, 3)
			require.Equal(t, 16384, total)
			runRedisIntegration(t, client)
			store, err := New(client)
			require.NoError(t, err)
			namespace := "layercache-cluster-" + rand.Text()
			cleanupRedisNamespace(t, client, namespace)

			t.Run("cross_slot_batch", func(t *testing.T) {
				ns := namespace + ":batch"
				keys := keysOnThreeMasters(t, ctx, client, ns, topology)
				base := newRedisIntegrationCache(t, store, ns)
				view := layercache.NewTyped[string](base)
				want := make(map[string]string)
				var physical []string
				for _, key := range keys {
					want[key] = key
					physical = append(physical, redisIntegrationKey(ns, key))
				}
				_, err := client.MGet(ctx, physical...).Result()
				require.ErrorContains(t, err, "CROSSSLOT", "the fixture must really span slots")
				got, err := view.MGet(ctx, keys, func(context.Context, []string) (map[string]string, error) { return want, nil })
				require.NoError(t, err, "[cluster:cross_slot_batch]")
				require.Equal(t, want, got)
				packets := make(map[string][]byte)
				for i, key := range keys {
					packet, err := client.Get(ctx, physical[i]).Bytes()
					require.NoError(t, err)
					packets[physical[i]] = packet
					require.NoError(t, base.InvalidateLocal(ctx, key))
				}
				receipts, err := store.MSet(ctx, packets, 5*time.Second)
				require.NoError(t, err)
				require.Len(t, receipts, 3)
				snapshots, err := store.MGet(ctx, physical)
				require.NoError(t, err)
				require.Len(t, snapshots, 3)
				got, err = view.MGet(ctx, keys, nil)
				require.NoError(t, err)
				require.Equal(t, want, got)
				require.NoError(t, base.Delete(ctx, keys[0]))
				require.NoError(t, client.RPush(ctx, physical[0], "wrong-type").Err())
				partialSnapshots, err := store.MGet(ctx, physical)
				require.ErrorContains(t, err, "WRONGTYPE")
				require.Len(t, partialSnapshots, 2, "[cluster:partial_pipeline]")
				require.NotContains(t, partialSnapshots, physical[0])
				for _, key := range physical[1:] {
					require.Equal(t, packets[key], partialSnapshots[key].Data)
				}
				requested := append(slices.Clone(keys), "new-miss")
				got, err = view.MGet(ctx, requested, func(_ context.Context, missing []string) (map[string]string, error) {
					if !slices.Equal(missing, []string{"new-miss"}) {
						return nil, fmt.Errorf("unexpected origin keys: %v", missing)
					}
					return map[string]string{"new-miss": "loaded"}, nil
				})
				require.ErrorContains(t, err, "WRONGTYPE")
				delete(want, keys[0])
				want["new-miss"] = "loaded"
				require.Equal(t, want, got)
			})

			t.Run("ttl_across_masters", func(t *testing.T) {
				ns := namespace + ":ttl"
				keys := keysOnThreeMasters(t, ctx, client, ns, topology)
				base := newRedisIntegrationCache(t, store, ns, layercache.WithNotFoundTTL(300*time.Millisecond))
				view := layercache.NewTyped[string](base)
				for _, key := range keys {
					require.NoError(t, view.Set(ctx, key, "value", layercache.TTL(700*time.Millisecond)))
				}
				time.Sleep(200 * time.Millisecond)
				for _, key := range keys {
					require.NoError(t, base.InvalidateLocal(ctx, key))
				}
				got, err := view.MGet(ctx, keys, nil)
				require.NoError(t, err)
				require.Len(t, got, 3)
				require.Eventually(t, func() bool {
					for _, key := range keys {
						n, err := client.Exists(ctx, redisIntegrationKey(ns, key)).Result()
						if err != nil || n != 0 {
							return false
						}
					}
					return true
				}, 3*time.Second, 10*time.Millisecond)
				got, err = view.MGet(ctx, keys, nil)
				require.NoError(t, err)
				require.Empty(t, got, "batch L1 fill must not restart the remote TTL")
				_, err = view.GetOrLoad(ctx, keys[0], func(context.Context, string) (string, error) { return "", layercache.ErrNotFound })
				require.ErrorIs(t, err, layercache.ErrNotFound)
				require.Eventually(t, func() bool {
					n, err := client.Exists(ctx, redisIntegrationKey(ns, keys[0])).Result()
					return err == nil && n == 0
				}, 3*time.Second, 10*time.Millisecond)
				_, err = view.Get(ctx, keys[0])
				require.ErrorIs(t, err, layercache.ErrMiss)
			})

			t.Run("invalidation", func(t *testing.T) {
				for _, batch := range []bool{false, true} {
					for _, mutation := range []string{"set", "delete", "local"} {
						name := mutation + "_single"
						if batch {
							name = mutation + "_batch"
						}
						t.Run(name, func(t *testing.T) {
							ns := namespace + ":" + name
							keys := keysOnThreeMasters(t, ctx, client, ns, topology)
							base := newRedisIntegrationCache(t, store, ns, layercache.WithLoadTimeout(30*time.Second), layercache.WithRemoteTimeout(30*time.Second))
							view := layercache.NewTyped[string](base)
							started, release := make(chan struct{}), make(chan struct{})
							var once sync.Once
							resume := func() { once.Do(func() { close(release) }) }
							defer resume()
							load := func(task context.Context) error {
								close(started)
								select {
								case <-release:
									return nil
								case <-task.Done():
									return task.Err()
								}
							}
							finished := make(chan error, 1)
							go func() {
								var err error
								if batch {
									_, err = view.MGet(ctx, keys, func(task context.Context, _ []string) (map[string]string, error) {
										err := load(task)
										out := make(map[string]string)
										for _, k := range keys {
											out[k] = "old"
										}
										return out, err
									})
								} else {
									_, err = view.GetOrLoad(ctx, keys[0], func(task context.Context, _ string) (string, error) { return "old", load(task) })
								}
								finished <- err
							}()
							select {
							case <-started:
							case <-ctx.Done():
								t.Fatal(ctx.Err())
							}
							switch mutation {
							case "set":
								require.NoError(t, view.Set(ctx, keys[0], "new"))
							case "delete":
								require.NoError(t, base.Delete(ctx, keys[0]))
							case "local":
								require.NoError(t, base.InvalidateLocal(ctx, keys[0]))
							}
							resume()
							require.ErrorIs(t, <-finished, layercache.ErrInvalidated, "[cluster:mutation_fence]")
							got, err := view.Get(ctx, keys[0])
							if mutation == "set" {
								require.NoError(t, err)
								require.Equal(t, "new", got)
							} else {
								require.ErrorIs(t, err, layercache.ErrMiss)
							}
							if batch {
								for _, key := range keys[1:] {
									_, err := view.Get(ctx, key)
									require.ErrorIs(t, err, layercache.ErrMiss)
								}
							}
						})
					}
				}
			})

			for _, batch := range []bool{false, true} {
				name := "single"
				if batch {
					name = "batch"
				}
				t.Run("snapshot_"+name, func(t *testing.T) {
					ns := namespace + ":snapshot:" + name
					keys := keysOnThreeMasters(t, ctx, client, ns, topology)
					base := newRedisIntegrationCache(t, store, ns, layercache.WithLoadTimeout(30*time.Second), layercache.WithRemoteTimeout(30*time.Second))
					view := layercache.NewTyped[string](base)
					for _, key := range keys {
						require.NoError(t, view.Set(ctx, key, "old"))
						require.NoError(t, base.InvalidateLocal(ctx, key))
					}
					hook := newPausedClusterReply(t, client, "eval", redisIntegrationKey(ns, keys[0]))
					finished := make(chan error, 1)
					go func() {
						var err error
						if batch {
							_, err = view.MGet(ctx, keys, nil)
						} else {
							_, err = view.Get(ctx, keys[0])
						}
						finished <- err
					}()
					select {
					case <-hook.entered:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					require.NoError(t, view.Set(ctx, keys[0], "new"))
					hook.resume()
					require.ErrorIs(t, <-finished, layercache.ErrInvalidated, "[cluster:snapshot_fence]")
					got, err := view.Get(ctx, keys[0])
					require.NoError(t, err)
					require.Equal(t, "new", got)
				})
				t.Run("close_network_"+name, func(t *testing.T) {
					ns := namespace + ":close:" + name
					base := newRedisIntegrationCache(t, store, ns, layercache.WithLoadTimeout(30*time.Second), layercache.WithRemoteTimeout(30*time.Second))
					view := layercache.NewTyped[string](base)
					require.NoError(t, view.Set(ctx, "key", "value"))
					require.NoError(t, base.InvalidateLocal(ctx, "key"))
					hook := newPausedClusterReply(t, client, "eval", redisIntegrationKey(ns, "key"))
					finished := make(chan error, 1)
					go func() {
						var err error
						if batch {
							_, err = view.MGet(ctx, []string{"key"}, nil)
						} else {
							_, err = view.Get(ctx, "key")
						}
						finished <- err
					}()
					select {
					case <-hook.entered:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					closed := make(chan error, 1)
					go func() { closed <- base.Close() }()
					select {
					case err := <-closed:
						require.NoError(t, err)
					case <-time.After(3 * time.Second):
						t.Fatal("[cluster:close_cancel] Close did not cancel the 30s remote operation within 3s")
					}
					select {
					case <-hook.exited:
					default:
						t.Fatal("Close returned before remote processing exited")
					}
					require.ErrorIs(t, <-finished, layercache.ErrClosed)
					require.NoError(t, client.Ping(ctx).Err())
				})
			}

			t.Run("commit_gate", func(t *testing.T) {
				ns := namespace + ":commit"
				base := newRedisIntegrationCache(t, store, ns, layercache.WithLoadTimeout(30*time.Second), layercache.WithRemoteTimeout(30*time.Second))
				view := layercache.NewTyped[string](base)
				hook := newPausedClusterReply(t, client, "set", redisIntegrationKey(ns, "key"))
				finished := make(chan error, 1)
				go func() {
					_, err := view.GetOrLoad(ctx, "key", func(context.Context, string) (string, error) { return "value", nil })
					finished <- err
				}()
				select {
				case <-hook.entered:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				bounded, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
				err := base.Delete(bounded, "key")
				cancel()
				require.ErrorIs(t, err, context.DeadlineExceeded, "Delete must wait for the issued write")
				require.EqualValues(t, 1, client.Exists(ctx, redisIntegrationKey(ns, "key")).Val())
				hook.resume()
				require.NoError(t, <-finished)
				require.NoError(t, base.Delete(ctx, "key"))
				_, err = view.Get(ctx, "key")
				require.ErrorIs(t, err, layercache.ErrMiss)
			})

			t.Run("refresh_cross_master_lease", func(t *testing.T) {
				ns := namespace + ":refresh-cross"
				var key, leaseKey string
				for n := 0; n < 256; n++ {
					candidate := fmt.Sprintf("key-%d", n)
					data := redisIntegrationKey(ns, candidate)
					lease := "\x00layercache:refresh:" + data
					dataSlot, err := client.ClusterKeySlot(ctx, data).Result()
					require.NoError(t, err)
					leaseSlot, err := client.ClusterKeySlot(ctx, lease).Result()
					require.NoError(t, err)
					if primaryForSlot(topology, dataSlot) != primaryForSlot(topology, leaseSlot) {
						key, leaseKey = candidate, lease
						t.Logf("data slot=%d primary=%s; lease slot=%d primary=%s", dataSlot, primaryForSlot(topology, dataSlot), leaseSlot, primaryForSlot(topology, leaseSlot))
						break
					}
				}
				require.NotEmpty(t, key)
				options := *client.Options()
				options.Addrs = slices.Clone(options.Addrs)
				second := redis.NewClusterClient(&options)
				t.Cleanup(func() { require.NoError(t, second.Close()) })
				secondStore, err := New(second)
				require.NoError(t, err)
				opts := []layercache.Option{layercache.WithRefreshInterval(time.Second), layercache.WithRefreshIdleTimeout(10 * time.Second)}
				a := newRedisIntegrationCache(t, store, ns, opts...)
				b := newRedisIntegrationCache(t, secondStore, ns, opts...)
				av, bv := layercache.NewTyped[string](a), layercache.NewTyped[string](b)
				require.NoError(t, av.Set(ctx, key, "seed"))
				load := func(context.Context, string) (string, error) { return "fresh", nil }
				_, err = av.GetOrLoad(ctx, key, load, layercache.Refresh(true))
				require.NoError(t, err)
				_, err = bv.GetOrLoad(ctx, key, load, layercache.Refresh(true))
				require.NoError(t, err)
				require.Eventually(t, func() bool {
					x, ex := av.Get(ctx, key)
					y, ey := bv.Get(ctx, key)
					owner, err := client.Get(ctx, leaseKey).Result()
					return ex == nil && ey == nil && x == "fresh" && y == x && err == nil && (owner == a.SourceID() || owner == b.SourceID())
				}, 5*time.Second, 10*time.Millisecond)
				require.NoError(t, a.Close())
				require.NoError(t, b.Close())
			})

			t.Run("moved_conditional_and_lease", func(t *testing.T) {
				ns := namespace + ":moved"
				keys := keysOnThreeMasters(t, ctx, client, ns, topology)
				bad := make([]redis.ClusterSlot, len(topology))
				for i, slot := range topology {
					bad[i] = redis.ClusterSlot{Start: slot.Start, End: slot.End, Nodes: []redis.ClusterNode{{Addr: topology[(i+1)%len(topology)].Nodes[0].Addr}}}
				}
				stale := redis.NewClusterClient(&redis.ClusterOptions{Addrs: addrs, Protocol: protocol, ContextTimeoutEnabled: true, ReadTimeout: time.Second, WriteTimeout: time.Second, MaxRedirects: 8, ClusterSlots: func(context.Context) ([]redis.ClusterSlot, error) { return bad, nil }})
				t.Cleanup(func() { require.NoError(t, stale.Close()) })
				remote, err := New(stale)
				require.NoError(t, err)
				for _, key := range keys {
					physical := redisIntegrationKey(ns, key)
					slot, err := client.ClusterKeySlot(ctx, physical).Result()
					require.NoError(t, err)
					var wrong string
					for _, s := range bad {
						if slot >= int64(s.Start) && slot <= int64(s.End) {
							wrong = s.Nodes[0].Addr
						}
					}
					direct := redis.NewClient(&redis.Options{Addr: wrong, Protocol: protocol, ContextTimeoutEnabled: true})
					err = direct.Get(ctx, physical).Err()
					require.NoError(t, direct.Close())
					require.ErrorContains(t, err, "MOVED")
					_, stored, err := remote.SetIf(ctx, physical, []byte("value"), time.Minute, layercache.SetIfAbsent)
					require.NoError(t, err)
					require.True(t, stored)
					_, stored, err = remote.SetIf(ctx, physical, []byte("bad"), time.Minute, layercache.SetIfAbsent)
					require.NoError(t, err)
					require.False(t, stored)
					_, stored, err = remote.SetIf(ctx, physical, []byte("updated"), time.Minute, layercache.SetIfPresent)
					require.NoError(t, err)
					require.True(t, stored)
					entry, err := remote.Get(ctx, physical)
					require.NoError(t, err)
					require.Equal(t, []byte("updated"), entry.Data)
					_, acquired, err := remote.AcquireLease(ctx, physical+":lease", "owner-a", time.Minute)
					require.NoError(t, err)
					require.True(t, acquired)
					_, acquired, err = remote.AcquireLease(ctx, physical+":lease", "owner-b", time.Minute)
					require.NoError(t, err)
					require.False(t, acquired)
				}
			})
		})
	}
}

func keysOnThreeMasters(t *testing.T, ctx context.Context, client *redis.ClusterClient, namespace string, topology []redis.ClusterSlot) []string {
	t.Helper()
	byMaster := make(map[string]string)
	for n := 0; n < 256 && len(byMaster) < 3; n++ {
		key := fmt.Sprintf("key-%d", n)
		slot, err := client.ClusterKeySlot(ctx, redisIntegrationKey(namespace, key)).Result()
		require.NoError(t, err)
		for _, s := range topology {
			if slot >= int64(s.Start) && slot <= int64(s.End) {
				addr := s.Nodes[0].Addr
				if _, ok := byMaster[addr]; !ok {
					byMaster[addr] = key
					t.Logf("%s slot=%d primary=%s", key, slot, addr)
				}
				break
			}
		}
	}
	require.Len(t, byMaster, 3)
	keys := make([]string, 0, 3)
	for _, key := range byMaster {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func primaryForSlot(topology []redis.ClusterSlot, slot int64) string {
	for _, part := range topology {
		if slot >= int64(part.Start) && slot <= int64(part.End) {
			return part.Nodes[0].Addr
		}
	}
	return ""
}

// Pause only after a real Redis reply, so an old snapshot/write can be held
// across a concurrent mutation without replacing the network or its result.
type pausedClusterReply struct {
	command, key             string
	entered, exited, release chan struct{}
	claimed                  atomic.Bool
	once                     sync.Once
}

func newPausedClusterReply(t *testing.T, client *redis.ClusterClient, command, key string) *pausedClusterReply {
	h := &pausedClusterReply{command: command, key: key, entered: make(chan struct{}), exited: make(chan struct{}), release: make(chan struct{})}
	client.AddHook(h)
	t.Cleanup(h.resume)
	return h
}
func (h *pausedClusterReply) resume()                                     { h.once.Do(func() { close(h.release) }) }
func (h *pausedClusterReply) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *pausedClusterReply) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		return h.pause(ctx, []redis.Cmder{cmd}, err)
	}
}
func (h *pausedClusterReply) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		err := next(ctx, cmds)
		return h.pause(ctx, cmds, err)
	}
}
func (h *pausedClusterReply) pause(ctx context.Context, cmds []redis.Cmder, err error) error {
	if err != nil {
		return err
	}
	for _, cmd := range cmds {
		pos := 1
		if h.command == "eval" {
			pos = 3
		}
		if cmd.Name() != h.command || len(cmd.Args()) <= pos || cmd.Args()[pos] != h.key {
			continue
		}
		if !h.claimed.CompareAndSwap(false, true) {
			return nil
		}
		close(h.entered)
		defer close(h.exited)
		select {
		case <-h.release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
