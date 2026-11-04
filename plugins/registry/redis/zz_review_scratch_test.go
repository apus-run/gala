package redis_test

import (
	"context"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/rdb/rdbtest"
	"github.com/apus-run/gala/plugins/registry/redis"
	"github.com/apus-run/gala/registry"
)

// 1. Key collision: service "svc" (hash gala:services:svc:data) vs service
// "svc:data" (zset gala:services:svc:data).
func TestZZColonNameCollision(t *testing.T) {
	cli := rdbtest.New(t)
	reg, _ := redis.NewRegistry(cli)
	defer reg.Close()
	ctx := context.Background()
	if err := reg.Register(ctx, &registry.ServiceInstance{Name: "svc", ID: "a", Endpoints: []string{"grpc://1.1.1.1:1"}}); err != nil {
		t.Fatalf("register svc: %v", err)
	}
	err := reg.Register(ctx, &registry.ServiceInstance{Name: "svc:data", ID: "b", Endpoints: []string{"grpc://1.1.1.1:2"}})
	t.Logf("register svc:data err = %v", err)
}

// 2. Register with an already-canceled ctx: error returned, but is the instance
// kept locally and resurrected by the heartbeat?
func TestZZCanceledCtxPhantom(t *testing.T) {
	cli := rdbtest.New(t)
	reg, _ := redis.NewRegistry(cli, redis.WithLeaseTTL(300*time.Millisecond))
	dis, _ := redis.NewDiscovery(cli)
	defer dis.Close()
	defer reg.Close()

	ctxDead, cancel := context.WithCancel(context.Background())
	cancel()
	ins := &registry.ServiceInstance{Name: "svc", ID: "phantom", Endpoints: []string{"grpc://1.1.1.1:1"}}
	err := reg.Register(ctxDead, ins)
	t.Logf("register err = %v", err)

	got, _ := dis.GetService(context.Background(), "svc")
	t.Logf("immediately after failed register: %d instances", len(got))

	time.Sleep(500 * time.Millisecond) // heartbeats fire
	got, _ = dis.GetService(context.Background(), "svc")
	t.Logf("after heartbeats: %d instances: %v", len(got), got)
}

// 3. Does Close remove instances from redis? And does a valid-JSON foreign
// hash field (no id) surface as an empty instance?
func TestZZCloseRemovesAndForeignField(t *testing.T) {
	cli := rdbtest.New(t)
	reg, _ := redis.NewRegistry(cli)
	dis, _ := redis.NewDiscovery(cli)
	defer dis.Close()
	ctx := context.Background()
	_ = reg.Register(ctx, &registry.ServiceInstance{Name: "svc", ID: "a", Endpoints: []string{"grpc://1.1.1.1:1"}})
	if err := reg.Close(); err != nil {
		t.Fatal(err)
	}
	n, _ := cli.ZCard(ctx, "gala:services:{svc}:z").Result()
	h, _ := cli.HLen(ctx, "gala:services:{svc}:h").Result()
	t.Logf("after Close: zcard=%d hlen=%d", n, h)

	// Foreign field with valid JSON but no id.
	_ = cli.ZAdd(ctx, "gala:services:{svc}:z", goredis.Z{
		Score:  float64(time.Now().Add(30 * time.Second).UnixMilli()),
		Member: "weird",
	}).Err()
	_ = cli.HSet(ctx, "gala:services:{svc}:h", "weird", `{"foo":1}`).Err()
	got, err := dis.GetService(ctx, "svc")
	t.Logf("GetService with foreign field: %+v err=%v", got, err)
}

// 4. Poll error mid-subscription: does the subscription survive a redis outage?
func TestZZPollOutage(t *testing.T) {
	cli := rdbtest.New(t)
	reg, _ := redis.NewRegistry(cli)
	dis, _ := redis.NewDiscovery(cli, redis.WithPollInterval(50*time.Millisecond))
	defer dis.Close()
	defer reg.Close()
	ctx := context.Background()
	_ = reg.Register(ctx, &registry.ServiceInstance{Name: "svc", ID: "a", Endpoints: []string{"grpc://1.1.1.1:1"}})
	ch, err := dis.Subscribe(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	ev := <-ch
	t.Logf("replay: %v", ev.Instance.ID)

	// Kill redis, wait, ensure channel not closed.
	if c, ok := cli.(interface{ Close() error }); ok {
		_ = c.Close()
	}
	deadline := time.After(600 * time.Millisecond)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				t.Fatal("channel closed during outage")
			}
		case <-deadline:
			t.Logf("channel survived outage")
			return
		}
	}
}

// 5. Context leak check: subscriptions that end via Close or overflow leave
// an uncanceled child context on the caller's parent ctx.
func TestZZSubCancelNeverCalled(t *testing.T) {
	// covered by reading the source; here we just exercise overflow + Close
	cli := rdbtest.New(t)
	reg, _ := redis.NewRegistry(cli)
	dis, _ := redis.NewDiscovery(cli, redis.WithPollInterval(50*time.Millisecond))
	defer dis.Close()
	defer reg.Close()
	ctx := context.Background()
	ch, err := dis.Subscribe(ctx, "svc")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 200 { // overflow the 64+0 buffer
		_ = reg.Register(ctx, &registry.ServiceInstance{Name: "svc", ID: fmtSprintf(i), Endpoints: []string{"grpc://1.1.1.1:1"}})
	}
	time.Sleep(300 * time.Millisecond)
	// drain until closed
	for range 300 {
		if _, ok := <-ch; !ok {
			t.Logf("channel closed after overflow")
			return
		}
	}
	t.Fatal("channel never closed")
}

func fmtSprintf(i int) string {
	return "i-" + time.Duration(i).String() + "-" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
