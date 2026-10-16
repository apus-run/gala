package redisstore

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apus-run/gala/components/layercache"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Destructive operations are restricted to six explicitly labelled test nodes.
// Only the Dialer translates Docker-advertised addresses to published ports;
// Redis commands, replication, redirects and elections use the real servers.
type faultNode struct{ id, addr, host, ip, network string }
type faultFleet struct {
	project string
	nodes   []faultNode
	hosts   map[string]string
}

func dockerFaultCommand(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("docker %v: %w: %s", args, err, out)
	}
	return out, nil
}
func loadFaultFleet(t *testing.T) *faultFleet {
	t.Helper()
	project := os.Getenv("LAYERCACHE_CLUSTER_FAULT_PROJECT")
	if project == "" {
		t.Skip("set LAYERCACHE_CLUSTER_FAULT_PROJECT to an exclusive fault fixture")
	}
	require.Regexp(t, `^gala-layercache-fault-[a-z0-9_-]+$`, project)
	out, err := dockerFaultCommand("ps", "-q", "--filter", "label=com.docker.compose.project="+project, "--filter", "label=gala.layercache.fault-fixture=true")
	require.NoError(t, err)
	ids := strings.Fields(string(out))
	require.Len(t, ids, 6)
	out, err = dockerFaultCommand(append([]string{"inspect"}, ids...)...)
	require.NoError(t, err)
	var inspected []struct {
		ID              string
		Config          struct{ Labels map[string]string }
		NetworkSettings struct {
			Networks map[string]struct{ IPAddress, NetworkID string }
			Ports    map[string][]struct{ HostIP, HostPort string }
		}
	}
	require.NoError(t, json.Unmarshal(out, &inspected))
	f := &faultFleet{project: project, hosts: make(map[string]string)}
	for _, item := range inspected {
		require.Equal(t, project, item.Config.Labels["com.docker.compose.project"])
		require.Equal(t, "true", item.Config.Labels["gala.layercache.fault-fixture"])
		require.Len(t, item.NetworkSettings.Networks, 1)
		var node faultNode
		node.id = item.ID
		for _, network := range item.NetworkSettings.Networks {
			node.ip, node.network = network.IPAddress, network.NetworkID
		}
		for port, bindings := range item.NetworkSettings.Ports {
			if len(bindings) == 0 {
				continue
			}
			require.Len(t, bindings, 1)
			require.Equal(t, "127.0.0.1", bindings[0].HostIP)
			node.addr = net.JoinHostPort(node.ip, strings.TrimSuffix(port, "/tcp"))
			node.host = net.JoinHostPort("127.0.0.1", bindings[0].HostPort)
		}
		require.NotEmpty(t, node.addr)
		f.nodes = append(f.nodes, node)
		f.hosts[node.addr] = node.host
		f.hosts[node.host] = node.host
	}
	sort.Slice(f.nodes, func(i, j int) bool { return f.nodes[i].addr < f.nodes[j].addr })
	for _, node := range f.nodes {
		t.Logf("fixture node %s -> %s", node.addr, node.host)
	}
	return f
}
func (f *faultFleet) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, ok := f.hosts[addr]
	if !ok {
		return nil, fmt.Errorf("address outside fault fixture: %s", addr)
	}
	return (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(ctx, network, host)
}
func (f *faultFleet) cluster(t *testing.T, protocol int) *redis.ClusterClient {
	t.Helper()
	var addrs []string
	for _, n := range f.nodes {
		addrs = append(addrs, n.host)
	}
	c := redis.NewClusterClient(&redis.ClusterOptions{Addrs: addrs, Protocol: protocol, Dialer: f.dial, DialerRetries: 1, ReadTimeout: 150 * time.Millisecond, WriteTimeout: 150 * time.Millisecond, ContextTimeoutEnabled: true, MaxRetries: -1, MaxRedirects: 2})
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	return c
}
func (f *faultFleet) direct(t *testing.T, addr string, protocol int) *redis.Client {
	t.Helper()
	require.Contains(t, f.hosts, addr)
	c := redis.NewClient(&redis.Options{Addr: addr, Protocol: protocol, Dialer: f.dial, DialerRetries: 1, ReadTimeout: 2 * time.Second, WriteTimeout: time.Second, ContextTimeoutEnabled: true, MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	return c
}
func (f *faultFleet) node(t *testing.T, addr string) faultNode {
	t.Helper()
	for _, n := range f.nodes {
		if n.addr == addr {
			return n
		}
	}
	t.Fatalf("unknown fixture node %s", addr)
	return faultNode{}
}
func (f *faultFleet) fault(t *testing.T, node faultNode, kind string) func() {
	t.Helper()
	require.Equal(t, f.node(t, node.addr).id, node.id)
	// Register restoration before issuing the fault: a CLI timeout can leave
	// the Docker action committed even though its reply was not received.
	heal := func() {
		out, err := dockerFaultCommand("inspect", node.id)
		require.NoError(t, err)
		var current []struct {
			State           struct{ Paused bool }
			NetworkSettings struct {
				Networks map[string]struct{ NetworkID string }
			}
		}
		require.NoError(t, json.Unmarshal(out, &current))
		require.Len(t, current, 1)
		if kind == "pause" {
			if !current[0].State.Paused {
				return
			}
			_, err = dockerFaultCommand("unpause", node.id)
		} else {
			for _, connected := range current[0].NetworkSettings.Networks {
				if connected.NetworkID == node.network {
					return
				}
			}
			_, err = dockerFaultCommand("network", "connect", "--ip", node.ip, node.network, node.id)
		}
		require.NoError(t, err)
		t.Logf("healed %s node=%s", kind, node.addr)
	}
	t.Cleanup(heal)
	var args []string
	if kind == "pause" {
		args = []string{"pause", node.id}
	} else {
		require.Equal(t, "isolate", kind)
		args = []string{"network", "disconnect", node.network, node.id}
	}
	_, err := dockerFaultCommand(args...)
	require.NoError(t, err)
	t.Logf("fault %s node=%s container=%s", kind, node.addr, node.id)
	return heal
}

// Redis is PID 1. SIGKILL cannot flush or gracefully close its AOF. Register
// recovery first, including for a command whose reply is lost after Docker acts.
func (f *faultFleet) crash(t *testing.T, nodes ...faultNode) {
	t.Helper()
	require.NotEmpty(t, nodes)
	args := []string{"kill", "--signal", "KILL"}
	for _, node := range nodes {
		require.Equal(t, f.node(t, node.addr).id, node.id)
		t.Cleanup(func() { f.restart(t, node) })
		args = append(args, node.id)
	}
	_, err := dockerFaultCommand(args...)
	require.NoError(t, err)
	for _, node := range nodes {
		out, err := dockerFaultCommand("inspect", "--format", "{{.State.Running}} {{.State.ExitCode}}", node.id)
		require.NoError(t, err)
		require.Equal(t, "false 137", strings.TrimSpace(string(out)), "[fault:crash_exit]")
		t.Logf("SIGKILL exit=137 node=%s container=%s", node.addr, node.id)
	}
}
func (f *faultFleet) restart(t *testing.T, node faultNode) {
	t.Helper()
	require.Equal(t, f.node(t, node.addr).id, node.id)
	out, err := dockerFaultCommand("inspect", "--format", "{{.State.Running}}", node.id)
	require.NoError(t, err)
	if strings.TrimSpace(string(out)) == "true" {
		return
	}
	_, err = dockerFaultCommand("start", node.id)
	require.NoError(t, err)
	out, err = dockerFaultCommand("inspect", node.id)
	require.NoError(t, err)
	var current []struct {
		NetworkSettings struct {
			Networks map[string]struct{ IPAddress, NetworkID string }
			Ports    map[string][]struct{ HostIP, HostPort string }
		}
	}
	require.NoError(t, json.Unmarshal(out, &current))
	require.Len(t, current, 1)
	for _, network := range current[0].NetworkSettings.Networks {
		require.Equal(t, node.network, network.NetworkID)
		require.Equal(t, node.ip, network.IPAddress, "fixture address must survive restart")
	}
	_, port, err := net.SplitHostPort(node.addr)
	require.NoError(t, err)
	bindings := current[0].NetworkSettings.Ports[port+"/tcp"]
	require.Len(t, bindings, 1)
	require.Equal(t, node.host, net.JoinHostPort(bindings[0].HostIP, bindings[0].HostPort), "use fixed fixture ports for process recovery")
	t.Logf("restarted node=%s container=%s", node.addr, node.id)
}

func faultTopology(t *testing.T, ctx context.Context, c *redis.ClusterClient) []redis.ClusterSlot {
	t.Helper()
	var slots []redis.ClusterSlot
	require.Eventually(t, func() bool {
		var err error
		slots, err = c.ClusterSlots(ctx).Result()
		if err != nil {
			return false
		}
		masters := map[string]bool{}
		replicas := map[string]bool{}
		total := 0
		for _, s := range slots {
			if len(s.Nodes) < 2 {
				return false
			}
			masters[s.Nodes[0].Addr] = true
			replicas[s.Nodes[1].Addr] = true
			total += s.End - s.Start + 1
		}
		if len(masters) != 3 || len(replicas) != 3 || total != 16384 {
			return false
		}
		// Slot ownership alone can look healthy while CLUSTERDOWN is still
		// propagating after recovery. Require every primary to serve again.
		c.ReloadState(ctx)
		return c.ForEachMaster(ctx, func(ctx context.Context, node *redis.Client) error {
			info, err := node.ClusterInfo(ctx).Result()
			if err != nil {
				return err
			}
			if !strings.Contains(info, "cluster_state:ok") {
				return fmt.Errorf("primary has not recovered: %s", node.Options().Addr)
			}
			return nil
		}) == nil
	}, 20*time.Second, 100*time.Millisecond)
	c.ReloadState(ctx)
	return slots
}

// WAIT must follow a write on this exact connection. Waiting on an unrelated
// connection would not establish that earlier data/lease writes were replicated.
func faultReplicationBarrier(t *testing.T, ctx context.Context, node *redis.Client, key string) {
	t.Helper()
	conn := node.Conn()
	defer conn.Close()
	require.NoError(t, conn.Set(ctx, key, "barrier", time.Minute).Err())
	n, err := conn.Wait(ctx, 1, time.Second).Result()
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, int64(1))
}
func faultTagForPrimary(t *testing.T, ctx context.Context, c *redis.ClusterClient, ns, primary string, slots []redis.ClusterSlot) string {
	t.Helper()
	for n := 0; n < 256; n++ {
		tag := fmt.Sprintf("{%s-%d}", ns, n)
		slot, err := c.ClusterKeySlot(ctx, redisIntegrationKey(ns, tag+":key")).Result()
		require.NoError(t, err)
		if primaryForSlot(slots, slot) == primary {
			return tag
		}
	}
	t.Fatal("no key for primary")
	return ""
}
func waitFaultSignal(t *testing.T, ctx context.Context, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(10 * time.Second):
		t.Fatal("fault phase did not start")
	}
}

func TestRedisClusterFaultIntegration(t *testing.T) {
	f := loadFaultFleet(t)
	for _, protocol := range []int{2, 3} {
		t.Run(fmt.Sprintf("RESP%d", protocol), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
			t.Cleanup(cancel)
			c := f.cluster(t, protocol)
			faultTopology(t, ctx, c)
			var coverage string
			for _, node := range f.nodes {
				config, err := f.direct(t, node.addr, protocol).ConfigGet(ctx, "cluster-require-full-coverage").Result()
				require.NoError(t, err)
				if coverage == "" {
					coverage = config["cluster-require-full-coverage"]
				}
				require.Equal(t, coverage, config["cluster-require-full-coverage"])
			}
			require.Contains(t, []string{"yes", "no"}, coverage)
			t.Logf("cluster-require-full-coverage=%s", coverage)
			t.Run("ask_migration", func(t *testing.T) { testFaultASK(t, ctx, f, c, protocol) })
			t.Run("native_socket_close", func(t *testing.T) {
				for _, batch := range []bool{false, true} {
					name := "single"
					if batch {
						name = "batch"
					}
					t.Run(name, func(t *testing.T) { testFaultSocketClose(t, ctx, f, c, protocol, batch) })
				}
			})
			for _, kind := range []string{"pause", "isolate"} {
				t.Run(kind+"_failover", func(t *testing.T) {
					if coverage == "yes" {
						testFaultClusterDown(t, ctx, f, c, protocol, kind)
					} else {
						testFaultFailover(t, ctx, f, c, protocol, kind)
					}
				})
			}
			t.Run("primary_crash_restart", func(t *testing.T) {
				for _, node := range f.nodes {
					config, err := f.direct(t, node.addr, protocol).ConfigGet(ctx, "append*").Result()
					require.NoError(t, err)
					require.Equal(t, "yes", config["appendonly"])
					require.Equal(t, "always", config["appendfsync"])
				}
				testFaultFailover(t, ctx, f, c, protocol, "crash")
			})
			if coverage == "yes" {
				t.Run("pair_crash_restart", func(t *testing.T) { testFaultClusterDown(t, ctx, f, c, protocol, "crash_pair") })
			}
		})
	}
}

func testFaultASK(t *testing.T, ctx context.Context, f *faultFleet, c *redis.ClusterClient, protocol int) {
	slots := faultTopology(t, ctx, c)
	sourceAddr := slots[0].Nodes[0].Addr
	var targetAddr string
	for _, s := range slots {
		if s.Nodes[0].Addr != sourceAddr {
			targetAddr = s.Nodes[0].Addr
			break
		}
	}
	source, target := f.direct(t, sourceAddr, protocol), f.direct(t, targetAddr, protocol)
	sourceID, err := source.ClusterMyID(ctx).Result()
	require.NoError(t, err)
	targetID, err := target.ClusterMyID(ctx).Result()
	require.NoError(t, err)
	ns := "fault-ask-" + rand.Text()
	tag := faultTagForPrimary(t, ctx, c, ns, sourceAddr, slots)
	key := tag + ":moved"
	physical := redisIntegrationKey(ns, key)
	slot, err := c.ClusterKeySlot(ctx, physical).Result()
	require.NoError(t, err)
	store, err := New(c)
	require.NoError(t, err)
	base := newRedisIntegrationCache(t, store, ns, layercache.WithLoadTimeout(30*time.Second))
	view := layercache.NewTyped[string](base)
	require.NoError(t, view.Set(ctx, key, "value", layercache.TTL(20*time.Second)))
	stable := redisIntegrationKey(ns, tag+":stable")
	require.NoError(t, source.Set(ctx, stable, "stable", 20*time.Second).Err())
	// Restore even if a SETSLOT reply is lost or the test context expires.
	ready := false
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if !ready {
			for _, node := range []*redis.Client{source, target} {
				if err := node.Do(cleanup, "CLUSTER", "SETSLOT", slot, "STABLE").Err(); err != nil {
					t.Errorf("restore partial ASK setup: %v", err)
				}
			}
			return
		}
		for {
			keys, err := source.ClusterGetKeysInSlot(cleanup, int(slot), 100).Result()
			if err != nil {
				t.Errorf("finish ASK migration: %v", err)
				return
			}
			if len(keys) == 0 {
				break
			}
			for _, key := range keys {
				host, port, _ := net.SplitHostPort(targetAddr)
				if err := source.Migrate(cleanup, host, port, key, 0, 2*time.Second).Err(); err != nil {
					t.Errorf("finish ASK migration: %v", err)
					return
				}
			}
		}
		// Every current primary learns the final owner; replicas follow their primary.
		seen := map[string]bool{}
		for _, s := range slots {
			addr := s.Nodes[0].Addr
			if seen[addr] {
				continue
			}
			seen[addr] = true
			node := f.direct(t, addr, protocol)
			if err := node.Do(cleanup, "CLUSTER", "SETSLOT", slot, "NODE", targetID).Err(); err != nil {
				t.Errorf("publish ASK slot owner: %v", err)
			}
		}
		c.ReloadState(cleanup)
	}()
	require.NoError(t, target.Do(ctx, "CLUSTER", "SETSLOT", slot, "IMPORTING", sourceID).Err())
	require.NoError(t, source.Do(ctx, "CLUSTER", "SETSLOT", slot, "MIGRATING", targetID).Err())
	ready = true
	host, port, _ := net.SplitHostPort(targetAddr)
	require.NoError(t, source.Migrate(ctx, host, port, physical, 0, 2*time.Second).Err())
	require.ErrorContains(t, source.Get(ctx, physical).Err(), "ASK ")
	require.ErrorContains(t, target.Get(ctx, physical).Err(), "MOVED ")
	t.Logf("ASK slot=%d source=%s target=%s", slot, sourceAddr, targetAddr)
	require.NoError(t, base.InvalidateLocal(ctx, key))
	got, err := view.Get(ctx, key)
	require.NoError(t, err, "[fault:ask_read]")
	require.Equal(t, "value", got)
	var third string
	for _, candidate := range keysOnThreeMasters(t, ctx, c, ns+":third", slots) {
		p := redisIntegrationKey(ns+":third", candidate)
		s, err := c.ClusterKeySlot(ctx, p).Result()
		require.NoError(t, err)
		if owner := primaryForSlot(slots, s); owner != sourceAddr && owner != targetAddr {
			third = p
			break
		}
	}
	require.NotEmpty(t, third)
	require.NoError(t, c.Set(ctx, third, "third", 20*time.Second).Err())
	values, err := store.MGet(ctx, []string{physical, stable, third})
	require.NoError(t, err)
	require.Len(t, values, 3)
	broken := redisIntegrationKey(ns, tag+":broken")
	require.NoError(t, c.RPush(ctx, broken, "wrong-type").Err())
	partial, err := store.MGet(ctx, []string{physical, stable, third, broken})
	require.ErrorContains(t, err, "WRONGTYPE")
	require.Len(t, partial, 3, "healthy ASK/source/third-primary snapshots must survive an unrelated error")
	conditional := redisIntegrationKey(ns, tag+":conditional")
	_, ok, err := store.SetIf(ctx, conditional, []byte("NX"), time.Minute, layercache.SetIfAbsent)
	require.NoError(t, err)
	require.True(t, ok)
	_, ok, err = store.SetIf(ctx, conditional, []byte("bad"), time.Minute, layercache.SetIfAbsent)
	require.NoError(t, err)
	require.False(t, ok)
	_, ok, err = store.SetIf(ctx, conditional, []byte("XX"), time.Minute, layercache.SetIfPresent)
	require.NoError(t, err)
	require.True(t, ok)
	lease := redisIntegrationKey(ns, tag+":lease")
	_, ok, err = store.AcquireLease(ctx, lease, "a", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	_, ok, err = store.AcquireLease(ctx, lease, "b", time.Minute)
	require.NoError(t, err)
	require.False(t, ok)
	receipts, err := store.MSet(ctx, map[string][]byte{physical: values[physical].Data, conditional: []byte("pipeline"), third: []byte("third-updated")}, 10*time.Second)
	require.NoError(t, err)
	require.Len(t, receipts, 3)
	written, err := store.MGet(ctx, []string{physical, conditional, third})
	require.NoError(t, err)
	require.Len(t, written, 3)
	require.Equal(t, []byte("pipeline"), written[conditional].Data)
	require.Equal(t, []byte("third-updated"), written[third].Data)
	// An old source result cannot overwrite a newer write routed through ASK.
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	defer resume()
	done := make(chan error, 1)
	missing := tag + ":late"
	go func() {
		_, err := view.GetOrLoad(ctx, missing, func(task context.Context, _ string) (string, error) {
			close(started)
			select {
			case <-release:
				return "old", nil
			case <-task.Done():
				return "", task.Err()
			}
		})
		done <- err
	}()
	waitFaultSignal(t, ctx, started)
	require.NoError(t, view.Set(ctx, missing, "new"))
	resume()
	require.ErrorIs(t, <-done, layercache.ErrInvalidated)
	// Refresh and lease scripts must both follow ASK, including NoRetry commands.
	a := newRedisIntegrationCache(t, store, ns+":refresh", layercache.WithRefreshInterval(time.Second), layercache.WithRefreshIdleTimeout(20*time.Second))
	b := newRedisIntegrationCache(t, store, ns+":refresh", layercache.WithRefreshInterval(time.Second), layercache.WithRefreshIdleTimeout(20*time.Second))
	av, bv := layercache.NewTyped[string](a), layercache.NewTyped[string](b)
	refreshKey := tag + ":refresh"
	require.NoError(t, av.Set(ctx, refreshKey, "seed"))
	load := func(context.Context, string) (string, error) { return "fresh", nil }
	_, err = av.GetOrLoad(ctx, refreshKey, load, layercache.Refresh(true))
	require.NoError(t, err)
	_, err = bv.GetOrLoad(ctx, refreshKey, load, layercache.Refresh(true))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		x, ex := av.Get(ctx, refreshKey)
		y, ey := bv.Get(ctx, refreshKey)
		return ex == nil && ey == nil && x == "fresh" && x == y
	}, 5*time.Second, 20*time.Millisecond)
	require.NoError(t, a.Close())
	require.NoError(t, b.Close())
	short := tag + ":short"
	require.NoError(t, view.Set(ctx, short, "short", layercache.TTL(600*time.Millisecond)))
	require.NoError(t, base.InvalidateLocal(ctx, short))
	_, err = view.Get(ctx, short)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		n, err := c.Exists(ctx, redisIntegrationKey(ns, short)).Result()
		return err == nil && n == 0
	}, 3*time.Second, 20*time.Millisecond)
	_, err = view.Get(ctx, short)
	require.ErrorIs(t, err, layercache.ErrMiss)
	// Block only after a real ASK round trip, separately from native socket testing.
	hook := newPausedClusterReply(t, c, "eval", physical)
	require.NoError(t, base.InvalidateLocal(ctx, key))
	readDone := make(chan error, 1)
	go func() { _, err := view.Get(ctx, key); readDone <- err }()
	waitFaultSignal(t, ctx, hook.entered)
	require.NoError(t, base.Close())
	require.ErrorIs(t, <-readDone, layercache.ErrClosed)
	hook.resume()
}

// Observe an actual socket Read without replacing data or blocking in a Hook.
type faultObservedConn struct {
	net.Conn
	armed   *atomic.Bool
	claimed *atomic.Bool
	entered chan struct{}
}

func (c *faultObservedConn) Read(p []byte) (int, error) {
	if c.armed.Load() && c.claimed.CompareAndSwap(false, true) {
		close(c.entered)
	}
	return c.Conn.Read(p)
}
func testFaultSocketClose(t *testing.T, ctx context.Context, f *faultFleet, c *redis.ClusterClient, protocol int, batch bool) {
	slots := faultTopology(t, ctx, c)
	node := f.node(t, slots[0].Nodes[1].Addr)
	var heal func()
	direct := faultSocketClose(t, ctx, f, node, protocol, batch, func(direct *redis.Client) {
		require.NoError(t, direct.Ping(ctx).Err())
		heal = f.fault(t, node, "pause")
	})
	heal()
	require.NoError(t, direct.Ping(ctx).Err(), "Close must preserve the application client")
	faultTopology(t, ctx, c)
}

// beforeRead may warm the connection and then pause its server. With nil, the
// paused server also blocks real connection initialization on socket Read.
func faultSocketClose(t *testing.T, ctx context.Context, f *faultFleet, node faultNode, protocol int, batch bool, beforeRead func(*redis.Client)) *redis.Client {
	var armed, claimed atomic.Bool
	entered := make(chan struct{})
	direct := redis.NewClient(&redis.Options{Addr: node.addr, Protocol: protocol, PoolSize: 1, MaxActiveConns: 1, ContextTimeoutEnabled: true, MaxRetries: -1, DialerRetries: 1, ReadTimeout: 300 * time.Millisecond, WriteTimeout: 300 * time.Millisecond, Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := f.dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &faultObservedConn{conn, &armed, &claimed, entered}, nil
	}})
	t.Cleanup(func() { require.NoError(t, direct.Close()) })
	if beforeRead != nil {
		beforeRead(direct)
	}
	store, err := New(direct)
	require.NoError(t, err)
	base := newRedisIntegrationCache(t, store, "fault-socket-"+rand.Text(), layercache.WithRemoteTimeout(30*time.Second))
	armed.Store(true)
	finished := make(chan error, 1)
	go func() {
		var err error
		if batch {
			_, err = layercache.NewTyped[string](base).MGet(ctx, []string{"key"}, nil)
		} else {
			var value string
			err = base.Get(ctx, "key", &value)
		}
		finished <- err
	}()
	waitFaultSignal(t, ctx, entered)
	start := time.Now()
	closed := make(chan error, 1)
	go func() { closed <- base.Close() }()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not drain native socket I/O within the configured network deadline")
	}
	require.ErrorIs(t, <-finished, layercache.ErrClosed, "[fault:socket_close]")
	require.ErrorIs(t, base.Get(ctx, "key", nil), layercache.ErrClosed)
	t.Logf("native socket Close drain=%s; ReadTimeout=300ms, RemoteTimeout=30s", time.Since(start))
	return direct
}

func testFaultFailover(t *testing.T, ctx context.Context, f *faultFleet, c *redis.ClusterClient, protocol int, kind string) {
	slots := faultTopology(t, ctx, c)
	oldPrimary := slots[0].Nodes[0].Addr
	replica := slots[0].Nodes[1].Addr
	node := f.node(t, oldPrimary)
	primary := f.direct(t, oldPrimary, protocol)
	primaryID := slots[0].Nodes[0].ID
	ns := "fault-" + kind + "-" + rand.Text()
	tag := faultTagForPrimary(t, ctx, c, ns, oldPrimary, slots)
	key := tag + ":data"
	physical := redisIntegrationKey(ns, key)
	slot, err := c.ClusterKeySlot(ctx, physical).Result()
	require.NoError(t, err)
	store, err := New(c)
	require.NoError(t, err)
	base := newRedisIntegrationCache(t, store, ns, layercache.WithLoadTimeout(60*time.Second), layercache.WithRemoteTimeout(time.Second), layercache.WithNotFoundTTL(30*time.Second))
	view := layercache.NewTyped[string](base)
	keys := keysOnThreeMasters(t, ctx, c, ns, slots)
	want := map[string]string{}
	for _, k := range append(keys, key) {
		want[k] = "baseline"
		require.NoError(t, view.Set(ctx, k, "baseline", layercache.TTL(time.Minute)))
	}
	healthyPackets := map[string][]byte{}
	for _, k := range keys {
		p := redisIntegrationKey(ns, k)
		s, err := c.ClusterKeySlot(ctx, p).Result()
		require.NoError(t, err)
		if primaryForSlot(slots, s) != oldPrimary {
			value, err := c.Get(ctx, p).Bytes()
			require.NoError(t, err)
			healthyPackets[p] = value
		}
	}
	lease := redisIntegrationKey(ns, tag+":manual-lease")
	_, ok, err := store.AcquireLease(ctx, lease, "owner-a", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	// Hold an old batch origin snapshot across the real topology change.
	missing := []string{tag + ":origin", "missing-other-slot"}
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	defer resume()
	finished := make(chan error, 1)
	go func() {
		_, err := view.MGet(ctx, missing, func(task context.Context, _ []string) (map[string]string, error) {
			close(started)
			select {
			case <-release:
				return map[string]string{missing[0]: "old", missing[1]: "old"}, nil
			case <-task.Done():
				return nil, task.Err()
			}
		})
		finished <- err
	}()
	waitFaultSignal(t, ctx, started)
	// A blocked refresher's expired lease must not authorize a late write.
	a := newRedisIntegrationCache(t, store, ns+":refresh", layercache.WithLoadTimeout(60*time.Second), layercache.WithRefreshInterval(time.Second), layercache.WithRefreshIdleTimeout(time.Minute))
	av := layercache.NewTyped[string](a)
	rk := tag + ":refresh"
	require.NoError(t, av.Set(ctx, rk, "seed", layercache.TTL(time.Minute)))
	refreshStarted, refreshRelease := make(chan struct{}), make(chan struct{})
	var refreshOnce sync.Once
	unblockRefresh := func() { refreshOnce.Do(func() { close(refreshRelease) }) }
	defer unblockRefresh()
	var sourceCalls atomic.Int64
	_, err = av.GetOrLoad(ctx, rk, func(context.Context, string) (string, error) {
		if sourceCalls.Add(1) == 1 {
			close(refreshStarted)
			<-refreshRelease
			return "stale", nil
		}
		return "fresh", nil
	}, layercache.Refresh(true))
	require.NoError(t, err)
	waitFaultSignal(t, ctx, refreshStarted)
	short := tag + ":ttl"
	require.NoError(t, view.Set(ctx, short, "expires", layercache.TTL(600*time.Millisecond)))
	faultReplicationBarrier(t, ctx, primary, redisIntegrationKey(ns, tag+":barrier"))
	var heal func()
	if kind == "crash" {
		f.crash(t, node)
		heal = func() { f.restart(t, node) }
	} else {
		heal = f.fault(t, node, kind)
	}
	defer heal()
	if kind == "crash" {
		for _, s := range slots {
			if s.Nodes[0].Addr == oldPrimary {
				continue
			}
			socketNode := f.node(t, s.Nodes[1].Addr)
			var healSocket func()
			client := faultSocketClose(t, ctx, f, socketNode, protocol, true, func(client *redis.Client) {
				require.NoError(t, client.Ping(ctx).Err())
				healSocket = f.fault(t, socketNode, "pause")
			})
			healSocket()
			require.NoError(t, client.Ping(ctx).Err())
			break
		}
	}
	partialCtx, stopPartial := context.WithTimeout(ctx, 500*time.Millisecond)
	requested := []string{physical}
	for key := range healthyPackets {
		requested = append(requested, key)
	}
	partial, partialErr := store.MGet(partialCtx, requested)
	stopPartial()
	for key, value := range healthyPackets {
		if kind != "crash" {
			require.Equal(t, value, partial[key].Data, "[fault:partial_batch]")
		}
	}
	t.Logf("fault batch confirmed snapshots=%d error=%v", len(partial), partialErr)
	for _, k := range append(keys, key) {
		require.NoError(t, base.InvalidateLocal(ctx, k))
	}
	var originCalls atomic.Int64
	probe, probeErr := view.MGet(ctx, append(keys, key), func(context.Context, []string) (map[string]string, error) { originCalls.Add(1); return nil, nil })
	require.Zero(t, originCalls.Load(), "backend unavailability must not become authoritative absence")
	for _, k := range keys {
		if _, healthy := healthyPackets[redisIntegrationKey(ns, k)]; healthy && kind != "crash" {
			require.Equal(t, want[k], probe[k], "[fault:core_partial_batch]")
		}
	}
	for k, v := range probe {
		require.Equal(t, want[k], v)
	}
	if probeErr != nil {
		require.False(t, base.IsNotFound(probeErr))
		t.Logf("degraded batch result=%d error=%v", len(probe), probeErr)
	}
	// Observe the replica, rather than manually forcing CLUSTER FAILOVER.
	var promoted string
	require.Eventually(t, func() bool {
		current, err := c.ClusterSlots(ctx).Result()
		if err != nil {
			return false
		}
		promoted = primaryForSlot(current, slot)
		return promoted == replica
	}, 25*time.Second, 100*time.Millisecond)
	c.ReloadState(ctx)
	t.Logf("automatic failover slot=%d %s -> %s", slot, oldPrimary, promoted)
	require.Eventually(t, func() bool { got, err := view.Get(ctx, key); return err == nil && got == "baseline" }, 10*time.Second, 50*time.Millisecond)
	_, ok, err = store.AcquireLease(ctx, lease, "owner-b", time.Minute)
	require.NoError(t, err)
	require.False(t, ok, "replicated live lease must not be stolen")
	_, ok, err = store.AcquireLease(ctx, lease, "owner-a", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	if kind == "crash" {
		batch := make(map[string][]byte, len(keys))
		for _, k := range keys {
			p := redisIntegrationKey(ns, k)
			value, err := c.Get(ctx, p).Bytes()
			require.NoError(t, err)
			batch[p] = value
		}
		receipts, err := store.MSet(ctx, batch, time.Minute)
		require.NoError(t, err)
		require.Len(t, receipts, 3, "[fault:crash_batch_write]")
		t.Log("post-promotion cross-primary MSet confirmed 3 receipts")
	}
	require.NoError(t, view.Set(ctx, missing[0], "new"))
	resume()
	require.ErrorIs(t, <-finished, layercache.ErrInvalidated, "[fault:batch_fence]")
	_, err = view.Get(ctx, missing[1])
	require.ErrorIs(t, err, layercache.ErrMiss, "invalidated batch must publish no untouched member")
	n, err := c.Exists(ctx, redisIntegrationKey(ns, missing[1])).Result()
	require.NoError(t, err)
	require.Zero(t, n)
	got, err := view.Get(ctx, missing[0])
	require.NoError(t, err)
	require.Equal(t, "new", got)
	require.Eventually(t, func() bool {
		n, err := c.Exists(ctx, redisIntegrationKey(ns, short)).Result()
		return err == nil && n == 0
	}, 3*time.Second, 20*time.Millisecond)
	_, err = view.Get(ctx, short)
	require.ErrorIs(t, err, layercache.ErrMiss, "expired data must not reappear after promotion")
	b := newRedisIntegrationCache(t, store, ns+":refresh", layercache.WithRefreshInterval(time.Second), layercache.WithRefreshIdleTimeout(time.Minute))
	bv := layercache.NewTyped[string](b)
	_, err = bv.GetOrLoad(ctx, rk, func(context.Context, string) (string, error) { return "fresh", nil }, layercache.Refresh(true))
	require.NoError(t, err)
	require.Eventually(t, func() bool { x, err := bv.Get(ctx, rk); return err == nil && x == "fresh" }, 10*time.Second, 50*time.Millisecond)
	unblockRefresh()
	require.Eventually(t, func() bool { return a.Stats().RefreshErrors > 0 }, 5*time.Second, 20*time.Millisecond, "[fault:expired_refresh] expired refresh must finish as a failure before Close")
	require.Eventually(t, func() bool { x, err := av.GetSkippingLocal(ctx, rk); return err == nil && x == "fresh" }, 5*time.Second, 20*time.Millisecond)
	require.NoError(t, a.Close())
	require.NoError(t, b.Close())
	if kind == "crash" {
		require.NoError(t, view.Set(ctx, key, "updated-while-primary-stopped", layercache.TTL(time.Minute)))
		want[key] = "updated-while-primary-stopped"
	}
	heal()
	faultTopology(t, ctx, c)
	if kind == "crash" {
		id, err := primary.ClusterMyID(ctx).Result()
		require.NoError(t, err)
		require.Equal(t, primaryID, id, "[fault:restart_identity]")
		require.Eventually(t, func() bool {
			info, err := primary.Info(ctx, "replication").Result()
			return err == nil && strings.Contains(info, "role:slave") && strings.Contains(info, "master_link_status:up")
		}, 15*time.Second, 50*time.Millisecond)
	}
	newPrimary := f.direct(t, promoted, protocol)
	faultReplicationBarrier(t, ctx, newPrimary, redisIntegrationKey(ns, tag+":healed-barrier"))
	if kind == "crash" {
		expected, err := c.Get(ctx, physical).Bytes()
		require.NoError(t, err)
		conn := primary.Conn()
		defer conn.Close()
		require.NoError(t, conn.ReadOnly(ctx).Err())
		actual, err := conn.Get(ctx, physical).Bytes()
		require.NoError(t, err)
		require.Equal(t, expected, actual, "[fault:restart_catchup] restarted primary must follow the promoted replica")
		require.NoError(t, conn.ReadWrite(ctx).Err())
		t.Logf("original primary restarted as replica, preserved ID=%s and caught up with writes during its outage", primaryID)
	}
	for k, v := range want {
		require.NoError(t, base.InvalidateLocal(ctx, k))
		got, err := view.Get(ctx, k)
		require.NoError(t, err)
		require.Equal(t, v, got)
	}
	require.NoError(t, base.Close())
	require.NoError(t, c.Ping(ctx).Err())
}

// Hold the replica briefly so the default full-coverage policy has a stable,
// observable CLUSTERDOWN window, then let Redis elect it without forcing failover.
func testFaultClusterDown(t *testing.T, ctx context.Context, f *faultFleet, c *redis.ClusterClient, protocol int, kind string) {
	slots := faultTopology(t, ctx, c)
	oldPrimary, replica := slots[0].Nodes[0].Addr, slots[0].Nodes[1].Addr
	var healthy, healthyReplica string
	for _, s := range slots {
		if s.Nodes[0].Addr != oldPrimary {
			healthy, healthyReplica = s.Nodes[0].Addr, s.Nodes[1].Addr
			break
		}
	}
	primary := f.direct(t, oldPrimary, protocol)
	primaryID, replicaID := slots[0].Nodes[0].ID, slots[0].Nodes[1].ID
	require.NotEmpty(t, primaryID)
	require.NotEmpty(t, replicaID)
	observers := map[string]*redis.Client{}
	for _, s := range slots {
		addr := s.Nodes[0].Addr
		if addr != oldPrimary && observers[addr] == nil {
			observers[addr] = f.direct(t, addr, protocol)
		}
	}
	ns := "fault-down-" + kind + "-" + rand.Text()
	tag := faultTagForPrimary(t, ctx, c, ns, oldPrimary, slots)
	healthyTag := faultTagForPrimary(t, ctx, c, ns, healthy, slots)
	keys := keysOnThreeMasters(t, ctx, c, ns, slots)
	key, short, healthyShort := tag+":data", tag+":ttl", healthyTag+":ttl"
	physical := redisIntegrationKey(ns, key)
	slot, err := c.ClusterKeySlot(ctx, physical).Result()
	require.NoError(t, err)
	store, err := New(c)
	require.NoError(t, err)
	base := newRedisIntegrationCache(t, store, ns, layercache.WithLoadTimeout(60*time.Second), layercache.WithNotFoundTTL(30*time.Second))
	view := layercache.NewTyped[string](base)
	for _, k := range append(keys, key) {
		require.NoError(t, view.Set(ctx, k, "baseline", layercache.TTL(time.Minute)))
	}
	lease := redisIntegrationKey(ns, tag+":lease")
	_, ok, err := store.AcquireLease(ctx, lease, "owner-a", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	// Keep an old batch source result alive across CLUSTERDOWN and local invalidation.
	missing := []string{tag + ":origin", healthyTag + ":origin"}
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	defer resume()
	finished := make(chan error, 1)
	go func() {
		_, err := view.MGet(ctx, missing, func(task context.Context, _ []string) (map[string]string, error) {
			close(started)
			select {
			case <-release:
				return map[string]string{missing[0]: "old", missing[1]: "old"}, nil
			case <-task.Done():
				return nil, task.Err()
			}
		})
		finished <- err
	}()
	waitFaultSignal(t, ctx, started)

	// One existing refresher outlives its lease. A separate job must not query
	// the source when its healthy-shard lease command returns CLUSTERDOWN.
	var leaseErrors, probeCalls, expiredErrors atomic.Int64
	a := newRedisIntegrationCache(t, store, ns+":refresh", layercache.WithLoadTimeout(60*time.Second), layercache.WithRefreshInterval(time.Second), layercache.WithRefreshIdleTimeout(time.Minute), layercache.WithLocalTTL(30*time.Second), layercache.WithErrorHandler(func(err error) {
		if strings.Contains(err.Error(), "CLUSTERDOWN") {
			leaseErrors.Add(1)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			expiredErrors.Add(1)
		}
	}))
	av := layercache.NewTyped[string](a)
	rk := tag + ":refresh"
	require.NoError(t, av.Set(ctx, rk, "seed", layercache.TTL(time.Minute)))
	refreshStarted, refreshRelease := make(chan struct{}), make(chan struct{})
	var refreshOnce sync.Once
	unblockRefresh := func() { refreshOnce.Do(func() { close(refreshRelease) }) }
	defer unblockRefresh()
	var refreshCalls atomic.Int64
	_, err = av.GetOrLoad(ctx, rk, func(context.Context, string) (string, error) {
		if refreshCalls.Add(1) == 1 {
			close(refreshStarted)
			<-refreshRelease
			return "stale", nil
		}
		return "fresh", nil
	}, layercache.Refresh(true))
	require.NoError(t, err)
	waitFaultSignal(t, ctx, refreshStarted)
	probeKey, hotKey := healthyTag+":refresh-probe", healthyTag+":hot"
	require.NoError(t, av.Set(ctx, hotKey, "hot", layercache.TTL(time.Minute)))
	require.NoError(t, av.Set(ctx, probeKey, "seed", layercache.TTL(time.Minute)))
	_, err = av.GetOrLoad(ctx, probeKey, func(context.Context, string) (string, error) { probeCalls.Add(1); return "fresh", nil }, layercache.Refresh(true))
	require.NoError(t, err)
	healthyLease := redisIntegrationKey(ns, healthyTag+":lease")
	_, ok, err = store.AcquireLease(ctx, healthyLease, "owner-a", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, view.Set(ctx, short, "expires", layercache.TTL(600*time.Millisecond)))
	require.NoError(t, view.Set(ctx, healthyShort, "expires", layercache.TTL(600*time.Millisecond)))
	faultReplicationBarrier(t, ctx, primary, redisIntegrationKey(ns, tag+":barrier"))
	var healReplica, healPrimary func()
	if kind == "crash_pair" {
		f.crash(t, f.node(t, oldPrimary), f.node(t, replica))
		healReplica = func() { f.restart(t, f.node(t, replica)) }
		healPrimary = func() { f.restart(t, f.node(t, oldPrimary)) }
	} else {
		healReplica = f.fault(t, f.node(t, replica), "pause")
		healPrimary = f.fault(t, f.node(t, oldPrimary), kind)
	}
	defer healReplica()
	defer healPrimary()
	window := time.Now()
	healthyPhysical := redisIntegrationKey(ns, healthyTag+":probe")
	require.Eventually(t, func() bool {
		for _, observer := range observers {
			if !strings.Contains(fmt.Sprint(observer.Get(ctx, healthyPhysical).Err()), "CLUSTERDOWN") {
				return false
			}
		}
		return true
	}, 10*time.Second, 50*time.Millisecond, "[fault:clusterdown_observed]")
	t.Logf("CLUSTERDOWN observed on both reachable primaries after=%s", time.Since(window))

	requested := []string{redisIntegrationKey(ns, key)}
	for _, k := range keys {
		requested = append(requested, redisIntegrationKey(ns, k))
		require.NoError(t, base.InvalidateLocal(ctx, k))
	}
	require.NoError(t, base.InvalidateLocal(ctx, key))
	packets, err := store.MGet(ctx, requested)
	require.ErrorContains(t, err, "CLUSTERDOWN", "[fault:down_batch]")
	require.Empty(t, packets, "no shard confirmed a snapshot during CLUSTERDOWN")
	var originCalls atomic.Int64
	values, err := view.MGet(ctx, append(keys, key), func(context.Context, []string) (map[string]string, error) { originCalls.Add(1); return nil, nil })
	require.ErrorContains(t, err, "CLUSTERDOWN", "[fault:down_batch]")
	require.False(t, base.IsNotFound(err))
	require.Empty(t, values)
	require.Zero(t, originCalls.Load(), "[fault:down_origin] unavailability cannot authorize source or negative caching")
	// A valid L1 result remains a partial success, even while all L2 reads fail.
	values, err = av.MGet(ctx, []string{hotKey, healthyTag + ":cold"}, nil)
	require.ErrorContains(t, err, "CLUSTERDOWN")
	require.Equal(t, map[string]string{hotKey: "hot"}, values, "[fault:down_l1]")
	writes := map[string][]byte{healthyPhysical: []byte("unconfirmed")}
	for _, k := range keys {
		writes[redisIntegrationKey(ns, k)] = []byte("unconfirmed")
	}
	receipts, err := store.MSet(ctx, writes, time.Minute)
	require.ErrorContains(t, err, "CLUSTERDOWN")
	require.Empty(t, receipts, "[fault:down_receipt]")
	deadline, acquired, err := store.AcquireLease(ctx, healthyLease, "owner-b", time.Minute)
	require.ErrorContains(t, err, "CLUSTERDOWN")
	require.False(t, acquired)
	require.True(t, deadline.IsZero())
	require.Eventually(t, func() bool { return leaseErrors.Load() > 0 }, 2*time.Second, 20*time.Millisecond)
	calls, errorsBefore := probeCalls.Load(), leaseErrors.Load()
	require.Eventually(t, func() bool { return leaseErrors.Load() >= errorsBefore+2 }, 3*time.Second, 20*time.Millisecond)
	require.Equal(t, calls, probeCalls.Load(), "[fault:down_refresh] failed leases must not authorize source calls")

	// Local invalidation remains available without Redis; the entire old batch
	// must be fenced before it can publish any key or emit network writes.
	invalidateStart := time.Now()
	require.NoError(t, base.InvalidateLocal(ctx, missing[0]))
	require.Less(t, time.Since(invalidateStart), time.Second)
	resume()
	require.ErrorIs(t, <-finished, layercache.ErrInvalidated, "[fault:down_fence]")
	_, err = view.Get(ctx, short)
	require.Error(t, err, "[fault:down_ttl] expired L1 must not mask backend failure")
	require.False(t, base.IsNotFound(err))
	_, err = view.Get(ctx, healthyShort)
	require.ErrorContains(t, err, "CLUSTERDOWN", "[fault:down_ttl]")
	var blockedClient *redis.Client
	if kind == "crash_pair" {
		var healSocket func()
		socketNode := f.node(t, healthyReplica)
		blockedClient = faultSocketClose(t, ctx, f, socketNode, protocol, true, func(client *redis.Client) {
			require.NoError(t, client.Ping(ctx).Err())
			healSocket = f.fault(t, socketNode, "pause")
		})
		healSocket()
	} else {
		blockedClient = faultSocketClose(t, ctx, f, f.node(t, replica), protocol, true, nil)
	}
	// Still CLUSTERDOWN after all assertions: none were satisfied by early recovery.
	for _, observer := range observers {
		require.ErrorContains(t, observer.Get(ctx, healthyPhysical).Err(), "CLUSTERDOWN")
	}
	t.Logf("CLUSTERDOWN assertions complete; held replica for=%s lease errors=%d source calls=%d", time.Since(window), leaseErrors.Load(), probeCalls.Load())

	owner := replica
	if kind == "crash_pair" {
		healPrimary()
		owner = oldPrimary
	} else {
		healReplica()
	}
	require.Eventually(t, func() bool {
		current, err := c.ClusterSlots(ctx).Result()
		return err == nil && primaryForSlot(current, slot) == owner
	}, 25*time.Second, 100*time.Millisecond)
	c.ReloadState(ctx)
	require.Eventually(t, func() bool { x, err := view.Get(ctx, key); return err == nil && x == "baseline" }, 10*time.Second, 50*time.Millisecond, "[fault:restart_persistence]")
	if kind == "crash_pair" {
		t.Logf("pair crash recovered original primary from AOF slot=%d owner=%s", slot, owner)
		healReplica()
	} else {
		t.Logf("automatic full-coverage failover slot=%d %s -> %s", slot, oldPrimary, owner)
	}
	for _, k := range keys {
		x, err := view.Get(ctx, k)
		require.NoError(t, err)
		require.Equal(t, "baseline", x, "[fault:down_negative] backend error must not leave a negative cache entry")
	}
	for _, k := range missing {
		_, err := view.Get(ctx, k)
		require.ErrorIs(t, err, layercache.ErrMiss)
		n, err := c.Exists(ctx, redisIntegrationKey(ns, k)).Result()
		require.NoError(t, err)
		require.Zero(t, n, "[fault:down_fence]")
	}
	_, ok, err = store.AcquireLease(ctx, lease, "owner-b", time.Minute)
	require.NoError(t, err)
	require.False(t, ok, "[fault:down_lease] replicated lease must not be stolen after recovery")
	_, ok, err = store.AcquireLease(ctx, healthyLease, "owner-b", time.Minute)
	require.NoError(t, err)
	require.False(t, ok, "failed lease command must not change its owner")
	for _, p := range []string{redisIntegrationKey(ns, short), redisIntegrationKey(ns, healthyShort), healthyPhysical} {
		n, err := c.Exists(ctx, p).Result()
		require.NoError(t, err)
		require.Zero(t, n, "expired data and rejected writes must stay absent")
	}
	_, err = view.Get(ctx, short)
	require.ErrorIs(t, err, layercache.ErrMiss)
	b := newRedisIntegrationCache(t, store, ns+":refresh", layercache.WithRefreshInterval(time.Second), layercache.WithRefreshIdleTimeout(time.Minute))
	bv := layercache.NewTyped[string](b)
	_, err = bv.GetOrLoad(ctx, rk, func(context.Context, string) (string, error) { return "fresh", nil }, layercache.Refresh(true))
	require.NoError(t, err)
	require.Eventually(t, func() bool { x, err := bv.Get(ctx, rk); return err == nil && x == "fresh" }, 5*time.Second, 20*time.Millisecond)
	unblockRefresh()
	require.Eventually(t, func() bool { return expiredErrors.Load() > 0 }, 2*time.Second, 20*time.Millisecond, "[fault:down_expired_refresh]")
	require.Eventually(t, func() bool { x, err := av.GetSkippingLocal(ctx, rk); return err == nil && x == "fresh" }, 2*time.Second, 20*time.Millisecond)
	require.NoError(t, a.Close())
	require.NoError(t, b.Close())
	healPrimary()
	faultTopology(t, ctx, c)
	faultReplicationBarrier(t, ctx, f.direct(t, owner, protocol), redisIntegrationKey(ns, tag+":healed-barrier"))
	if kind == "crash_pair" {
		for addr, expected := range map[string]string{oldPrimary: primaryID, replica: replicaID} {
			id, err := f.direct(t, addr, protocol).ClusterMyID(ctx).Result()
			require.NoError(t, err)
			require.Equal(t, expected, id, "[fault:restart_identity]")
		}
	}
	require.NoError(t, blockedClient.Ping(ctx).Err(), "Close preserves the application client")
	require.NoError(t, base.Close())
}
