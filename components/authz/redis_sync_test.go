package authz

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redisadapter "github.com/apus-run/gala/components/authz/adapter/redis"
	"github.com/redis/go-redis/v9"
)

func TestRedisWatchersPropagateCommittedRevocation(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	adapter, err := redisadapter.NewAdapter(&redisadapter.Config{Client: client})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.AddPolicy("p", "p", []string{"alice", "/reports", "GET"}); err != nil {
		t.Fatal(err)
	}
	var instances []*Authorizer
	for range 2 {
		watcher, err := redisadapter.NewWatcher(context.Background(), client, "policy:test")
		if err != nil {
			t.Fatal(err)
		}
		defer watcher.Close()
		// Separate adapters avoid sharing per-enforcer filtering state.
		a, err := NewRedis(client, WithModelFilePath(writeModelFile(t, testAllowModel)), WithWatcher(watcher), WithWatcherErrorHandler(func(err error) { t.Error(err) }))
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, a)
		assertAlice(t, a, true)
	}
	if err := adapter.RemovePolicy("p", "p", []string{"alice", "/reports", "GET"}); err != nil {
		t.Fatal(err)
	}
	if err := instances[0].SyncPolicy(); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	for {
		allowed, err := instances[1].Enforce("alice", "/reports", "GET")
		if err != nil {
			t.Fatal(err)
		}
		if !allowed {
			break
		}
		select {
		case <-deadline:
			t.Fatal("peer did not revoke")
		case <-time.After(time.Millisecond):
		}
	}
}
