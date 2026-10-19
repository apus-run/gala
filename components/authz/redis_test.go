package authz

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	redisadapter "github.com/apus-run/gala/components/authz/adapter/redis"
	"github.com/redis/go-redis/v9"
)

func TestNewRedisLoadsSavedPolicy(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	adapter, err := redisadapter.NewAdapter(&redisadapter.Config{Client: client})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.AddPolicy("p", "p", []string{"alice", "/reports", "GET"}); err != nil {
		t.Fatal(err)
	}

	authorizer, err := NewRedis(client, WithModelFilePath(writeModelFile(t, testAllowModel)))
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := authorizer.Authorize("alice", "/reports", "GET")
	if err != nil || !allowed {
		t.Fatalf("allowed=%v error=%v", allowed, err)
	}
}

func TestNewRedisRejectsNilClient(t *testing.T) {
	authorizer, err := NewRedis(nil)
	if authorizer != nil || err == nil {
		t.Fatalf("authorizer=%v error=%v", authorizer, err)
	}
}

func TestNewRedisAcceptsCommandsOnly(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	// Embedding the narrower interface makes accidental UniversalClient
	// requirements a compile error, even though the backing client supports more.
	commands := struct{ redis.Cmdable }{client}
	adapter, err := redisadapter.NewAdapter(&redisadapter.Config{Client: commands})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.AddPolicy("p", "p", []string{"alice", "/reports", "GET"}); err != nil {
		t.Fatal(err)
	}
	a, err := NewRedis(commands, WithModelFilePath(writeModelFile(t, testAllowModel)))
	if err != nil {
		t.Fatal(err)
	}
	assertAlice(t, a, true)
}
