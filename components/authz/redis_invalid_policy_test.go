package authz

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redisadapter "github.com/apus-run/gala/components/authz/adapter/redis"
	"github.com/redis/go-redis/v9"
)

func TestRedisMalformedPolicyCallbackKeepsLastSnapshot(t *testing.T) {
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
	watcher := &testWatcher{}
	var reported error
	a := newSyncAuthorizer(t, adapter, WithWatcher(watcher), WithWatcherErrorHandler(func(err error) { reported = err }))
	before := a.lastLoad.Load()
	if err := client.RPush(context.Background(), "casbin_rules", "null").Err(); err != nil {
		t.Fatal(err)
	}
	watcher.callback("changed")
	if reported == nil {
		t.Fatal("malformed policy error was not reported")
	}
	if a.lastLoad.Load() != before {
		t.Fatal("failed load renewed freshness")
	}
	assertAlice(t, a, true)
	if err := client.LRem(context.Background(), "casbin_rules", 0, "null").Err(); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadPolicy(); err != nil {
		t.Fatal(err)
	}
	assertAlice(t, a, true)
}
