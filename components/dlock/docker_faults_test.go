//go:build integration

package dlock_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/apus-run/gala/components/dlock"
	"github.com/apus-run/gala/components/dlock/etcd"
	"github.com/apus-run/gala/components/dlock/redis"
)

func awaitFaultState(t *testing.T, description string, timeout time.Duration, probe func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		err := probe()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %v", description, err)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func dockerControl(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// Only disrupt a container created by the dedicated fault suite.
func faultContainer(t *testing.T, service string) string {
	t.Helper()
	project := dockerEndpoint(t, "DLOCK_FAULT_PROJECT")
	id := dockerEndpoint(t, "DLOCK_FAULT_"+strings.ToUpper(service)+"_ID")
	identity := dockerControl(t, "inspect", "--format", `{{index .Config.Labels "com.docker.compose.project"}}/{{index .Config.Labels "com.docker.compose.service"}}`, id)
	if !strings.HasPrefix(project, "gala-dlock-faults-") || identity != project+"/"+service {
		t.Fatalf("unexpected test container %q", identity)
	}
	return id
}

func faultLocker(t *testing.T, c dlock.Client, key string, ttl time.Duration) dlock.Locker {
	t.Helper()
	l, err := c.NewLock(t.Context(), dlock.WithLockName(key), dlock.WithLockTimeout(ttl), dlock.WithOperationTimeout(1500*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if closer, ok := l.(io.Closer); ok {
		t.Cleanup(func() { _ = closer.Close() })
	}
	return l
}

func TestDockerFaultRedisFailover(t *testing.T) {
	ctx := t.Context()
	addrs := map[string]string{
		"primary:6379": dockerEndpoint(t, "DLOCK_FAULT_PRIMARY_ADDR"),
		"replica:6379": dockerEndpoint(t, "DLOCK_FAULT_REPLICA_ADDR"),
	}
	primaryID, replicaID := faultContainer(t, "primary"), faultContainer(t, "replica")
	var observers []*goredis.SentinelClient
	var sentinelAddrs []string
	for i := 1; i <= 3; i++ {
		addr := dockerEndpoint(t, fmt.Sprintf("DLOCK_FAULT_SENTINEL_%d_ADDR", i))
		sentinelAddrs = append(sentinelAddrs, addr)
		s := goredis.NewSentinelClient(&goredis.Options{Addr: addr, MaxRetries: -1})
		t.Cleanup(func() { _ = s.Close() })
		observers = append(observers, s)
	}
	awaitFaultState(t, "Sentinel quorum and replica discovery", 40*time.Second, func() error {
		for _, s := range observers {
			state, err := s.Master(ctx, "leases").Result()
			if err != nil {
				return err
			}
			if state["num-other-sentinels"] != "2" || state["num-slaves"] != "1" {
				return fmt.Errorf("sentinels=%s replicas=%s", state["num-other-sentinels"], state["num-slaves"])
			}
			if err := s.CkQuorum(ctx, "leases").Err(); err != nil {
				return err
			}
		}
		return nil
	})
	direct := func(addr string) *goredis.Client {
		c := goredis.NewClient(&goredis.Options{Addr: addr, MaxRetries: -1})
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	primary, replica := direct(addrs["primary:6379"]), direct(addrs["replica:6379"])
	// Only translate Compose service names to published loopback ports. The
	// failover client discovers the primary through Sentinel on every redial.
	rdb := goredis.NewFailoverClient(&goredis.FailoverOptions{MasterName: "leases", SentinelAddrs: sentinelAddrs,
		ContextTimeoutEnabled: true, MaxRetries: -1, DialTimeout: time.Second,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if mapped, ok := addrs[addr]; ok {
				addr = mapped
			}
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, addr)
		},
	})
	t.Cleanup(func() { _ = rdb.Close() })
	f := redis.NewClient(rdb)
	const ttl = 2 * time.Minute
	syncedKey, lostKey := "synced-"+rand.Text(), "lagged-"+rand.Text()
	synced, lagged := faultLocker(t, f, syncedKey, ttl), faultLocker(t, f, lostKey, ttl)
	if err := synced.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	token, err := primary.Get(ctx, syncedKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	awaitFaultState(t, "control lock visible on replica", 30*time.Second, func() error {
		got, err := replica.Get(ctx, syncedKey).Result()
		if err != nil {
			return err
		}
		if got != token {
			return errors.New("replica value differs")
		}
		return nil
	})
	dockerControl(t, "pause", replicaID)
	paused := true
	t.Cleanup(func() {
		if paused {
			dockerControl(t, "unpause", replicaID)
		}
	})
	// Close the existing replication stream while the replica cannot reconnect.
	// This places the next acknowledged write unambiguously outside the replica's history.
	if n, err := primary.ClientKillByFilter(ctx, "TYPE", "replica").Result(); err != nil || n != 1 {
		t.Fatalf("disconnect replica: %d, %v", n, err)
	}
	start := time.Now()
	if err := lagged.Lock(ctx); err != nil {
		t.Fatalf("primary did not acknowledge the lock: %v", err)
	}
	if err := primary.Get(ctx, lostKey).Err(); err != nil {
		t.Fatal(err)
	}
	dockerControl(t, "kill", primaryID)
	dockerControl(t, "unpause", replicaID)
	paused = false
	awaitFaultState(t, "Sentinels agree on the promoted replica", 45*time.Second, func() error {
		for _, s := range observers {
			node, err := s.GetMasterAddrByName(ctx, "leases").Result()
			if err != nil {
				return err
			}
			if len(node) != 2 || node[0] != "replica" {
				return fmt.Errorf("master=%v", node)
			}
		}
		info, err := replica.Info(ctx, "replication").Result()
		if err != nil {
			return err
		}
		if !strings.Contains(info, "role:master") {
			return errors.New("replica is not writable yet")
		}
		return nil
	})
	awaitFaultState(t, "application reconnected to the new primary", 10*time.Second, func() error { return rdb.Ping(ctx).Err() })
	if err := synced.Refresh(ctx); err != nil {
		t.Fatalf("lost replicated control: %v", err)
	}
	if err := faultLocker(t, f, syncedKey, ttl).Lock(ctx); !errors.Is(err, dlock.ErrLocked) {
		t.Fatalf("replicated lock admitted a contender: %v", err)
	}
	if err := replica.Get(ctx, lostKey).Err(); !errors.Is(err, goredis.Nil) {
		t.Fatalf("expected absent lagged lock: %v", err)
	}
	next := faultLocker(t, f, lostKey, ttl)
	if err := next.Lock(ctx); err != nil {
		t.Fatalf("acquire on promoted primary: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed >= ttl {
		t.Fatalf("TTL elapsed before second acquisition: %s", elapsed)
	}
	if err := lagged.Refresh(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("old Refresh: %v", err)
	}
	if err := lagged.Unlock(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("old Unlock: %v", err)
	}
	if err := next.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("replicated lock excluded B; unreplicated lock admitted B after %s of A's %s TTL", elapsed.Round(time.Millisecond), ttl)
}

func TestDockerFaultEtcdPartition(t *testing.T) {
	ctx := t.Context()
	names := []string{"a", "b", "c"}
	clients := make([]*clientv3.Client, 3)
	addrs := make([]string, 3)
	for i, n := range names {
		addrs[i] = dockerEndpoint(t, "DLOCK_FAULT_"+strings.ToUpper(n)+"_ADDR")
		c, err := clientv3.New(clientv3.Config{Endpoints: []string{addrs[i]}, DialTimeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		clients[i] = c
	}
	statusOf := func(i int) (*clientv3.StatusResponse, error) {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		return clients[i].Status(probe, addrs[i])
	}
	leader := -1
	var original uint64
	awaitFaultState(t, "three members agree on leader", 30*time.Second, func() error {
		ids := map[uint64]bool{}
		for i := range clients {
			s, err := statusOf(i)
			if err != nil {
				return err
			}
			if i == 0 {
				original = s.Leader
			}
			if s.Leader == 0 || s.Leader != original {
				return errors.New("leader election in progress")
			}
			ids[s.Header.MemberId] = true
			if s.Header.MemberId == original {
				leader = i
			}
		}
		if len(ids) != 3 || !ids[original] {
			return errors.New("incomplete cluster")
		}
		return nil
	})
	key := "partition-" + rand.Text()
	const ttl = 16 * time.Second
	old := faultLocker(t, etcd.NewClient(clients[leader]), key, ttl)
	started := time.Now()
	if err := old.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	majority := (leader + 1) % 3
	next := faultLocker(t, etcd.NewClient(clients[majority]), key, time.Minute)
	member := faultContainer(t, names[leader])
	network := dockerEndpoint(t, "DLOCK_FAULT_PROJECT") + "_peers"
	dockerControl(t, "network", "disconnect", network, member)
	heal := func() { dockerControl(t, "network", "connect", "--alias", "peer-"+names[leader], network, member) }
	isolated := true
	t.Cleanup(func() {
		if isolated {
			heal()
		}
	})
	awaitFaultState(t, "majority elects a new leader", 9*time.Second, func() error {
		s, err := statusOf(majority)
		if err != nil {
			return err
		}
		if s.Leader == 0 || s.Leader == original {
			return errors.New("majority still lacks new leader")
		}
		other, err := statusOf((leader + 2) % 3)
		if err != nil {
			return err
		}
		if s.Leader != other.Leader {
			return errors.New("majority has not converged")
		}
		return nil
	})
	// Status is served locally; it proves the minority's client endpoint still
	// responds even though it can no longer commit operations through Raft.
	minorityStatus, err := statusOf(leader)
	if err != nil || minorityStatus.Header.MemberId != original {
		t.Fatalf("minority client endpoint lost: %v, %v", minorityStatus, err)
	}
	if err := next.Lock(ctx); !errors.Is(err, dlock.ErrLocked) {
		t.Fatalf("majority did not preserve existing lease: %v", err)
	}
	// A unique key separates lack of quorum from ordinary lock contention.
	probe := faultLocker(t, etcd.NewClient(clients[leader]), key+"-fresh", ttl)
	for _, attempt := range []struct {
		name string
		call func(context.Context) error
	}{{"Refresh", old.Refresh}, {"Lock", probe.Lock}} {
		start := time.Now()
		err := attempt.call(ctx)
		if !(errors.Is(err, context.DeadlineExceeded) || errors.Is(err, rpctypes.ErrNoLeader) || status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded || errors.Is(err, rpctypes.ErrTimeout)) {
			t.Fatalf("expected quorum/timeout error from minority, got %v", err)
		}
		d := time.Since(start)
		t.Logf("minority %s returned %v after %s", attempt.name, err, d.Round(time.Millisecond))
		if d > 3*time.Second {
			t.Fatalf("minority operation exceeded budget: %s", d)
		}
	}
	awaitFaultState(t, "majority takes over expired lease", 35*time.Second, func() error {
		err := next.Lock(ctx)
		if err != nil && !errors.Is(err, dlock.ErrLocked) {
			t.Fatalf("majority acquire: %v", err)
		}
		return err
	})
	if err := next.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < ttl {
		t.Fatalf("majority took over after %s, before TTL %s", elapsed, ttl)
	}
	committed, err := clients[majority].Get(ctx, key)
	if err != nil || len(committed.Kvs) != 1 {
		t.Fatalf("read successor lease: %v, %v", committed, err)
	}
	heal()
	isolated = false
	awaitFaultState(t, "isolated member sees new ownership", 25*time.Second, func() error {
		probeCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		r, err := clients[leader].Get(probeCtx, key)
		if err != nil {
			return err
		}
		if len(r.Kvs) != 1 || r.Kvs[0].Lease != committed.Kvs[0].Lease || !bytes.Equal(r.Kvs[0].Value, committed.Kvs[0].Value) {
			return errors.New("recovered member has not observed the successor lease")
		}
		return nil
	})
	if err := old.Refresh(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("old Refresh after healing: %v", err)
	}
	if err := old.Unlock(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
		t.Fatalf("old Unlock after healing: %v", err)
	}
	if err := next.Refresh(ctx); err != nil {
		t.Fatalf("successor after healing: %v", err)
	}
	t.Logf("isolated former leader %s; minority could not renew or acquire, majority preserved lease then took over", names[leader])
}
