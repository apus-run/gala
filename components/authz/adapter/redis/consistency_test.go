package redisadapter

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	"github.com/casbin/casbin/v3/model"
	"github.com/redis/go-redis/v9"
)

func testRedisAdapter(t *testing.T) (*Adapter, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	a, err := NewAdapter(&Config{Client: client})
	if err != nil {
		t.Fatal(err)
	}
	return a, server, client
}
func policyModel(t *testing.T, subjects ...string) model.Model {
	t.Helper()
	m, err := model.NewModelFromFile("examples/rbac_model.conf")
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range subjects {
		m.AddPolicy("p", "p", []string{sub, "data1", "read"})
	}
	return m
}
func TestSaveEmptyPolicyPropagatesFailure(t *testing.T) {
	a, s, _ := testRedisAdapter(t)
	if err := a.SavePolicy(policyModel(t, "alice")); err != nil {
		t.Fatal(err)
	}
	s.SetError("ERR injected storage failure")
	if err := a.SavePolicy(policyModel(t)); err == nil {
		t.Fatal("failed clear returned success")
	}
	s.SetError("")
	m := policyModel(t)
	if err := a.LoadPolicy(m); err != nil {
		t.Fatal(err)
	}
	if len(m["p"]["p"].Policy) != 1 {
		t.Fatal("previous snapshot lost")
	}
	if err := a.SavePolicy(policyModel(t)); err != nil {
		t.Fatal(err)
	}
	m = policyModel(t)
	if err := a.LoadPolicy(m); err != nil || len(m["p"]["p"].Policy) != 0 {
		t.Fatalf("clear failed: %v", err)
	}
}
func TestSavePolicyReadersSeeWholeSnapshots(t *testing.T) {
	a, _, _ := testRedisAdapter(t)
	old, next := policyModel(t, "alice"), policyModel(t, "bob", "carol", "dave")
	if err := a.SavePolicy(old); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			m := old
			if i%2 == 0 {
				m = next
			}
			if err := a.SavePolicy(m); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for i := 0; i < 100; i++ {
		m := policyModel(t)
		if err := a.LoadPolicy(m); err != nil {
			t.Fatal(err)
		}
		p := m["p"]["p"].Policy
		if !reflect.DeepEqual(p, old["p"]["p"].Policy) && !reflect.DeepEqual(p, next["p"]["p"].Policy) {
			t.Fatalf("partial or mixed snapshot: %v", p)
		}
	}
	wg.Wait()
}

type commandHook struct{ before func(redis.Cmder) }

func (h commandHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h commandHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h commandHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error { h.before(cmd); return next(ctx, cmd) }
}
func TestLoadUsesSingleFullRange(t *testing.T) {
	a, s, c := testRedisAdapter(t)
	if err := a.SavePolicy(policyModel(t, "alice", "bob", "carol")); err != nil {
		t.Fatal(err)
	}
	values, err := c.LRange(context.Background(), a.key, 0, -1).Result()
	if err != nil {
		t.Fatal(err)
	}
	s.Del(a.key)
	c.AddHook(commandHook{before: func(cmd redis.Cmder) {
		if cmd.Name() == "llen" {
			t.Error("LLEN reintroduces split snapshot")
		}
		if cmd.Name() == "lrange" {
			for _, v := range values {
				_, _ = s.RPush(a.key, v)
			}
		}
	}})
	m := policyModel(t)
	if err := a.LoadPolicy(m); err != nil {
		t.Fatal(err)
	}
	if len(m["p"]["p"].Policy) != 3 {
		t.Fatal("truncated snapshot")
	}
}
func TestInjectedClientSurvivesCloseAndTypedNilRejected(t *testing.T) {
	a, _, c := testRedisAdapter(t)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	var client *redis.Client
	if _, err := NewAdapter(&Config{Client: client}); err == nil {
		t.Fatal("typed nil accepted")
	}
}
func TestWatcherSubscriptionReadyAndClientSurvivesClose(t *testing.T) {
	_, _, c := testRedisAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	w, err := NewWatcher(ctx, c, "policies:test")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	got := make(chan string, 1)
	if err := w.SetUpdateCallback(func(s string) { got <- s }); err != nil {
		t.Fatal(err)
	}
	if err := w.Update(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-ctx.Done():
		t.Fatal("ready subscription missed update")
	}
	w.Close()
	w.Close()
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	if err := w.Update(); err == nil {
		t.Fatal("closed watcher published")
	}
}
func TestWatcherCloseWaitsForCallback(t *testing.T) {
	_, _, c := testRedisAdapter(t)
	w, err := NewWatcher(context.Background(), c, "policies:close")
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	_ = w.SetUpdateCallback(func(string) { close(entered); <-release })
	if err := w.Update(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("callback not called")
	}
	done := make(chan struct{})
	go func() { w.Close(); close(done) }()
	select {
	case <-done:
		t.Error("Close did not wait")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close stuck")
	}
}
func TestWatcherStartupFailure(t *testing.T) {
	_, _, c := testRedisAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w, err := NewWatcher(ctx, c, "policies:cancel")
	if w != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("w=%v err=%v", w, err)
	}
}

func TestPolicyKeepsInteriorEmptyFields(t *testing.T) {
	a, _, _ := testRedisAdapter(t)
	rule := []string{"alice", "", "read"}
	if err := a.AddPolicy("p", "p", rule); err != nil {
		t.Fatal(err)
	}
	m := policyModel(t)
	if err := a.LoadPolicy(m); err != nil {
		t.Fatal(err)
	}
	p := m["p"]["p"].Policy
	if len(p) != 1 || len(p[0]) != 3 || p[0][1] != "" || p[0][2] != "read" {
		t.Fatalf("positional fields corrupted: %v", p)
	}
}

// Reject writes at every fallible staging step; none may alter the live key.
func TestSaveFailurePreservesCompleteSnapshot(t *testing.T) {
	for _, failure := range []string{"first-rpush", "second-rpush", "pexpire", "persist", "rename"} {
		t.Run(failure, func(t *testing.T) {
			a, s, c := testRedisAdapter(t)
			before := policyModel(t, "alice", "deny-role")
			if err := a.SavePolicy(before); err != nil {
				t.Fatal(err)
			}
			pushes := 0
			injected := false
			s.Server().SetPreHook(func(p *server.Peer, cmd string, args ...string) bool {
				cmd = strings.ToLower(cmd)
				if cmd == "rpush" {
					pushes++
				}
				match := cmd == failure || (failure == "first-rpush" && cmd == "rpush" && pushes == 1) || (failure == "second-rpush" && cmd == "rpush" && pushes == 2)
				if match {
					injected = true
					p.WriteError("ERR injected staging failure")
					return true
				}
				return false
			})
			err := a.SavePolicy(policyModel(t, "bob", "carol"))
			s.Server().SetPreHook(nil)
			if err == nil || !injected {
				t.Fatalf("injected=%v err=%v", injected, err)
			}
			after := policyModel(t)
			if err := a.LoadPolicy(after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before["p"]["p"].Policy, after["p"]["p"].Policy) {
				t.Fatalf("failed save changed snapshot: %v", after["p"]["p"].Policy)
			}
			if keys := s.Keys(); len(keys) != 1 || keys[0] != a.key {
				t.Fatalf("staging leak: %v", keys)
			}
			if err := a.SavePolicy(policyModel(t, "bob", "carol")); err != nil {
				t.Fatal(err)
			}
			ttl, err := c.TTL(context.Background(), a.key).Result()
			if err != nil || ttl != -1 {
				t.Fatalf("published snapshot expires: ttl=%v err=%v", ttl, err)
			}
		})
	}
}

func TestSnapshotKeyUsesSameHashTag(t *testing.T) {
	for _, tc := range []struct{ key, tag string }{{"casbin_rules", "casbin_rules"}, {"tenant:{a}:rules", "a"}, {"{{x}:rules", "{x"}} {
		one, err := snapshotKey(tc.key)
		if err != nil {
			t.Fatal(err)
		}
		two, err := snapshotKey(tc.key)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(one, "{"+tc.tag+"}:") || one == two {
			t.Fatalf("keys=%q %q", one, two)
		}
	}
	for _, key := range []string{"a{}b", "a{b", "a}b"} {
		if _, err := snapshotKey(key); err == nil {
			t.Fatalf("ambiguous key accepted: %q", key)
		}
	}
}

// These wrappers intentionally expose no Close, hooks, or generic execution API.
type commandsOnly struct{ redis.Cmdable }
type pubsubOnly struct{ WatcherClient }

func TestAdapterAcceptsCommandsOnlyAndDoesNotOwnThem(t *testing.T) {
	_, _, client := testRedisAdapter(t)
	a, err := NewAdapter(&Config{Client: commandsOnly{client}})
	if err != nil {
		t.Fatal(err)
	}
	want := policyModel(t, "alice", "bob")
	if err := a.SavePolicy(want); err != nil {
		t.Fatal(err)
	}
	got := policyModel(t)
	if err := a.LoadPolicy(got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want["p"]["p"].Policy, got["p"]["p"].Policy) {
		t.Fatal("round trip changed policy")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatal("injected client closed", err)
	}
	var typedNil *commandsOnly
	if _, err := NewAdapter(&Config{Client: typedNil}); err == nil {
		t.Fatal("typed nil accepted")
	}
}

func TestAdapterClosesOwnedClient(t *testing.T) {
	s := miniredis.RunT(t)
	a, err := NewAdapter(&Config{Network: "tcp", Address: s.Addr()})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.client.Ping(context.Background()).Err(); !errors.Is(err, redis.ErrClosed) {
		t.Fatalf("owned client not closed: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatal("second close", err)
	}
}

func TestWatcherAcceptsPublishSubscribeOnly(t *testing.T) {
	_, _, client := testRedisAdapter(t)
	w, err := NewWatcher(context.Background(), pubsubOnly{client}, "capabilities:changed")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	received := make(chan struct{}, 1)
	if err := w.SetUpdateCallback(func(string) { received <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	if err := w.Update(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("no notification")
	}
	w.Close()
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatal("watcher closed client", err)
	}
	var typedNil *pubsubOnly
	if _, err := NewWatcher(context.Background(), typedNil, "test"); err == nil {
		t.Fatal("typed nil accepted")
	}
}
