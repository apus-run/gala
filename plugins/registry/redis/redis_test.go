package redis_test

import (
	"context"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/rdb"
	"github.com/apus-run/gala/components/rdb/rdbtest"
	"github.com/apus-run/gala/plugins/registry/redis"
	"github.com/apus-run/gala/registry"
	"github.com/apus-run/gala/registry/registrytest"
)

// newPair builds a Registry and Discovery over one in-memory redis, both
// closed by the test | newPair 在同一个内存 redis 上建一对 Registry 与
// Discovery，测试结束关闭.
func newPair(t *testing.T, regOpts []redis.Option, disOpts []redis.DiscoveryOption) (*redis.Registry, *redis.Discovery, rdb.Cmdable) {
	t.Helper()
	cli := rdbtest.New(t)
	reg, err := redis.NewRegistry(cli, regOpts...)
	if err != nil {
		t.Fatal(err)
	}
	dis, err := redis.NewDiscovery(cli, disOpts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reg.Close()
		_ = dis.Close()
	})
	return reg, dis, cli
}

// TestConformance runs the shared contract suite | TestConformance 跑共享契约套件.
func TestConformance(t *testing.T) {
	registrytest.Run(t, func(t *testing.T) (registry.Registry, registry.Discovery) {
		// The shared slow-consumer test waits 100ms before draining events.
		// Poll within that window so overflow does not depend on registration speed.
		reg, dis, _ := newPair(t, nil, []redis.DiscoveryOption{redis.WithPollInterval(50 * time.Millisecond)})
		return reg, dis
	})
}

// TestHeartbeatKeepsInstanceAlive: the lease is shorter than the sleep, so
// only the heartbeat goroutine can keep the instance visible.
// TestHeartbeatKeepsInstanceAlive：租约比 sleep 短，实例能存活只能靠心跳.
func TestHeartbeatKeepsInstanceAlive(t *testing.T) {
	reg, dis, _ := newPair(t, []redis.Option{redis.WithLeaseTTL(300 * time.Millisecond)}, nil)
	ctx := context.Background()
	ins := &registry.ServiceInstance{Name: "svc", ID: "a", Endpoints: []string{"grpc://127.0.0.1:9000"}}
	if err := reg.Register(ctx, ins); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond) // > 2 leases; ~2 heartbeats fired | 超两个租约；约两次心跳
	got, err := dis.GetService(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("heartbeat lost the instance: %v", got)
	}
}

// TestCrashExpiresIndependently: an instance whose lease deadline has passed
// (a crash: heartbeat gone, no Deregister) is purged by the read path while
// its sibling stays — the lease is per member, not per key.
// TestCrashExpiresIndependently：租约已过（崩溃：心跳没了，也没 Deregister）的
// 实例在读路径被清掉而兄弟实例仍在——租约按 member 生效，不按 key.
func TestCrashExpiresIndependently(t *testing.T) {
	_, dis, cli := newPair(t, nil, nil)
	ctx := context.Background()
	// A live instance, as if a healthy registry had just renewed it, and a
	// crashed sibling written with a deadline already in the past. The test
	// imports go-redis for the Z struct only: rdb does not alias it, and a
	// test is not a reason to grow that component.
	// 一个存活实例（如同健康 registry 刚续约过），加一个到期时间已在过去的崩溃
	// 兄弟。测试仅为 Z 结构体引入 go-redis：rdb 没有别名它，测试不是给那个组件
	// 加别名的理由.
	now := time.Now()
	zadd := func(id string, deadline time.Time) {
		t.Helper()
		if err := cli.ZAdd(ctx, "gala:services:{svc}:z", goredis.Z{
			Score:  float64(deadline.UnixMilli()),
			Member: id,
		}).Err(); err != nil {
			t.Fatal(err)
		}
	}
	zadd("live", now.Add(30*time.Second))
	zadd("crashed", now.Add(-time.Second))
	if err := cli.HSet(ctx, "gala:services:{svc}:h", "crashed", "{}").Err(); err != nil {
		t.Fatal(err)
	}
	if err := cli.HSet(ctx, "gala:services:{svc}:h", "live",
		`{"id":"live","name":"svc","endpoints":["grpc://10.0.0.1:9000"]}`).Err(); err != nil {
		t.Fatal(err)
	}

	got, err := dis.GetService(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "live" {
		t.Fatalf("want only the live instance, got %v", got)
	}
	n, err := cli.ZCard(ctx, "gala:services:{svc}:z").Result()
	if err != nil {
		t.Fatal(err)
	}
	if exists, err := cli.HExists(ctx, "gala:services:{svc}:h", "crashed").Result(); err != nil || exists {
		t.Fatalf("expired payload remains: exists=%v err=%v", exists, err)
	}
	if n != 1 {
		t.Fatalf("stale member not purged, zcard = %d", n)
	}
}

// TestSubscribeStreamsChangesWithinPollInterval: an event lands within one
// poll interval plus slack | 事件在一个轮询间隔加余量内到达.
func TestSubscribeStreamsChangesWithinPollInterval(t *testing.T) {
	reg, dis, _ := newPair(t, nil, []redis.DiscoveryOption{redis.WithPollInterval(50 * time.Millisecond)})
	ctx := context.Background()
	ch, err := dis.Subscribe(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(ctx, &registry.ServiceInstance{Name: "svc", ID: "a", Endpoints: []string{"grpc://127.0.0.1:9000"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-ch:
		if ev.Type != registry.EventTypeAdd || ev.Instance.ID != "a" {
			t.Fatalf("got %v %v, want add a", ev.Type, ev.Instance)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("event did not arrive within one poll interval")
	}
}

// TestDeregisterRemovesFromDiscovery | 注销后从发现中消失.
func TestDeregisterRemovesFromDiscovery(t *testing.T) {
	reg, dis, _ := newPair(t, nil, nil)
	ctx := context.Background()
	ins := &registry.ServiceInstance{Name: "svc", ID: "a", Endpoints: []string{"grpc://127.0.0.1:9000"}}
	if err := reg.Register(ctx, ins); err != nil {
		t.Fatal(err)
	}
	if err := reg.Deregister(ctx, ins); err != nil {
		t.Fatal(err)
	}
	got, err := dis.GetService(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("after deregister got %v", got)
	}
}

// TestColonNamesDoNotCollide: a name containing ':' must not land one
// service's zset on another's hash key (same key, two types → WRONGTYPE).
// TestColonNamesDoNotCollide：含 ':' 的服务名不能让一个服务的 zset 落到另一个
// 服务的 hash key 上（同 key 异类型 → WRONGTYPE）.
func TestColonNamesDoNotCollide(t *testing.T) {
	reg, dis, _ := newPair(t, nil, nil)
	ctx := context.Background()
	for _, name := range []string{"svc", "svc:data"} {
		if err := reg.Register(ctx, &registry.ServiceInstance{Name: name, ID: "a", Endpoints: []string{"grpc://127.0.0.1:9000"}}); err != nil {
			t.Fatalf("Register(%q): %v", name, err)
		}
	}
	for _, name := range []string{"svc", "svc:data"} {
		got, err := dis.GetService(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Name != name {
			t.Fatalf("GetService(%q) = %v", name, got)
		}
	}
}

func TestFailedReplacementKeepsHeartbeat(t *testing.T) {
	reg, dis, cli := newPair(t, []redis.Option{redis.WithLeaseTTL(300 * time.Millisecond)}, nil)
	ctx := context.Background()
	ins := &registry.ServiceInstance{Name: "svc", ID: "a", Endpoints: []string{"grpc://localhost:1234"}}
	if err := reg.Register(ctx, ins); err != nil {
		t.Fatal(err)
	}
	dead, cancel := context.WithCancel(ctx)
	cancel()
	replacement := ins.Clone()
	replacement.Endpoints = []string{"grpc://localhost:5678"}
	if err := reg.Register(dead, replacement); err == nil {
		t.Fatal("canceled registration succeeded")
	}
	// Remove the remote record so that only a heartbeat can restore it.
	if err := cli.Del(ctx, "gala:services:{svc}:z", "gala:services:{svc}:h").Err(); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		got, err := dis.GetService(ctx, "svc")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 1 {
			if !got[0].Equal(ins) {
				t.Fatalf("renewed wrong instance: %v", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("previous registration was not renewed")
}

func TestSubscriberOwnsEventInstance(t *testing.T) {
	reg, dis, _ := newPair(t, nil, []redis.DiscoveryOption{redis.WithPollInterval(50 * time.Millisecond)})
	ctx := context.Background()
	ins := &registry.ServiceInstance{Name: "svc", ID: "a", Endpoints: []string{"grpc://localhost:1234"}}
	if err := reg.Register(ctx, ins); err != nil {
		t.Fatal(err)
	}
	ch, err := dis.Subscribe(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	(<-ch).Instance.ID = "consumer-edit"
	if err := reg.Deregister(ctx, ins); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-ch:
		if ev.Type != registry.EventTypeDelete || ev.Instance.ID != "a" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("missing delete")
	}
}

func TestSharedKeysOutliveShortLease(t *testing.T) {
	long, _, cli := newPair(t, nil, nil)
	short, err := redis.NewRegistry(cli, redis.WithLeaseTTL(300*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer short.Close()
	ctx := context.Background()
	for _, entry := range []struct {
		reg *redis.Registry
		id  string
	}{{long, "long"}, {short, "short"}} {
		if err := entry.reg.Register(ctx, &registry.ServiceInstance{Name: "svc", ID: entry.id, Endpoints: []string{"grpc://localhost:1234"}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"gala:services:{svc}:z", "gala:services:{svc}:h"} {
		if ttl, err := cli.PTTL(ctx, key).Result(); err != nil || ttl < 25*time.Second {
			t.Fatalf("%s TTL = %v, err = %v", key, ttl, err)
		}
	}
}
