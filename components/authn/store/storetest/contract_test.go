package storetest

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/authn/store"
	"github.com/apus-run/gala/components/authn/store/memory"
	redistore "github.com/apus-run/gala/components/authn/store/redis"
)

// sleepClock makes the memory backend wait in real time.
type sleepClock struct{}

func (sleepClock) Advance(d time.Duration) { time.Sleep(d) }

// serverClock advances miniredis's clock; real sleeps would not expire its keys.
type serverClock struct{ *miniredis.Miniredis }

func (c serverClock) Advance(d time.Duration) { c.FastForward(d) }

func TestMemoryStoreContract(t *testing.T) {
	SessionStorer(t, Options{
		New: func(*testing.T) (store.SessionStorer, Clock) {
			return memory.NewStore(), sleepClock{}
		},
	})
}

func TestRedisStoreContract(t *testing.T) {
	SessionStorer(t, Options{
		New: func(t *testing.T) (store.SessionStorer, Clock) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			return redistore.NewStore(client, "contract:"), serverClock{server}
		},
	})
}
