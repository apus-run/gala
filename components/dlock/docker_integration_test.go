//go:build integration

package dlock_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	goredis "github.com/redis/go-redis/v9"
	clientv3 "go.etcd.io/etcd/client/v3"
	driver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/apus-run/gala/components/dlock"
	"github.com/apus-run/gala/components/dlock/consul"
	"github.com/apus-run/gala/components/dlock/etcd"
	dlockgorm "github.com/apus-run/gala/components/dlock/gorm"
	"github.com/apus-run/gala/components/dlock/mongo"
	"github.com/apus-run/gala/components/dlock/redis"
)

func dockerEndpoint(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("missing %s; run integration/run.sh", name)
	}
	return value
}

func dockerClient(t *testing.T, backend string) dlock.Client {
	t.Helper()
	switch backend {
	case "redis":
		c := goredis.NewClient(&goredis.Options{Addr: dockerEndpoint(t, "DLOCK_REDIS_ADDR"), ContextTimeoutEnabled: true})
		t.Cleanup(func() { _ = c.Close() })
		return redis.NewClient(c)
	case "etcd":
		c, err := clientv3.New(clientv3.Config{Endpoints: []string{dockerEndpoint(t, "DLOCK_ETCD_ADDR")}, DialTimeout: 3 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return etcd.NewClient(c)
	case "consul":
		c, err := api.NewClient(&api.Config{Address: dockerEndpoint(t, "DLOCK_CONSUL_ADDR")})
		if err != nil {
			t.Fatal(err)
		}
		return consul.NewClient(c)
	case "mongo":
		return dockerMongoClient(t, rand.Text(), true)
	case "mysql", "postgres":
		var dialector gorm.Dialector
		if backend == "mysql" {
			dialector = mysql.Open(fmt.Sprintf("dlock:dlock@tcp(%s)/dlock?parseTime=true", dockerEndpoint(t, "DLOCK_MYSQL_ADDR")))
		} else {
			dialector = postgres.Open("postgres://dlock:dlock@" + dockerEndpoint(t, "DLOCK_POSTGRES_ADDR") + "/dlock?sslmode=disable")
		}
		db, err := gorm.Open(dialector)
		if err != nil {
			t.Fatal(err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		return dlockgorm.NewClient(db)
	default:
		t.Fatalf("unknown backend %q", backend)
		return nil
	}
}

func dockerMongoClient(t *testing.T, collection string, dropOnCleanup bool) dlock.Client {
	t.Helper()
	c, err := driver.Connect(t.Context(), options.Client().ApplyURI(dockerEndpoint(t, "DLOCK_TEST_MONGODB_URI")))
	if err != nil {
		t.Fatal(err)
	}
	coll := c.Database("gala_dlock_test").Collection(collection)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if dropOnCleanup {
			if err := coll.Drop(ctx); err != nil {
				t.Error(err)
			}
		}
		if err := c.Disconnect(ctx); err != nil {
			t.Error(err)
		}
	})
	return mongo.NewClient(c, coll)
}

func TestDockerLeases(t *testing.T) {
	for _, backend := range []string{"redis", "mysql", "postgres", "mongo", "etcd", "consul"} {
		t.Run(backend, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			client := dockerClient(t, backend)
			if init, ok := client.(dlock.Initializer); ok {
				if err := init.Init(ctx); err != nil {
					t.Fatal(err)
				}
			}
			key := "gala-dlock-" + rand.Text()
			newLocker := func(ttl time.Duration) dlock.Locker {
				l, err := client.NewLock(ctx, dlock.WithLockName(key), dlock.WithLockTimeout(ttl), dlock.WithOwnerID("same-owner"))
				if err != nil {
					t.Fatal(err)
				}
				if closer, ok := l.(io.Closer); ok {
					t.Cleanup(func() { _ = closer.Close() })
				}
				return l
			}
			ttl := time.Second
			if backend == "consul" {
				ttl = 10 * time.Second
			}
			old, next := newLocker(ttl), newLocker(time.Minute)
			if err := old.Lock(ctx); err != nil {
				t.Fatal(err)
			}
			if err := old.Lock(ctx); err != nil {
				t.Fatalf("idempotent Lock: %v", err)
			}
			if err := old.Refresh(ctx); err != nil {
				t.Fatal(err)
			}
			if err := next.Lock(ctx); !errors.Is(err, dlock.ErrLocked) {
				t.Fatalf("early takeover: %v", err)
			}
			// Allow for backend TTL granularity and Consul's session lock-delay.
			deadline := time.Now().Add(75 * time.Second)
			for {
				err := next.Lock(ctx)
				if err == nil {
					break
				}
				if !errors.Is(err, dlock.ErrLocked) || time.Now().After(deadline) {
					t.Fatalf("takeover: %v", err)
				}
				time.Sleep(300 * time.Millisecond)
			}
			if err := old.Refresh(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
				t.Fatalf("stale Refresh: %v", err)
			}
			if err := old.Unlock(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
				t.Fatalf("stale Unlock: %v", err)
			}
			if err := next.Refresh(ctx); err != nil {
				t.Fatalf("successor lost lease: %v", err)
			}
			cancelCtx, cancel := context.WithCancel(ctx)
			cancel()
			if err := next.Unlock(cancelCtx); err != nil {
				t.Fatalf("canceled release: %v", err)
			}
			if err := next.Unlock(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
				t.Fatalf("double release: %v", err)
			}
			contenders := make([]dlock.Locker, 6)
			for i := range contenders {
				contenders[i] = newLocker(time.Minute)
			}
			assertExclusiveWinner(t, contenders)

		})
	}
}

func assertExclusiveWinner(t *testing.T, contenders []dlock.Locker) {
	t.Helper()
	start := make(chan struct{})
	type result struct {
		locker dlock.Locker
		err    error
	}
	results := make(chan result, len(contenders))
	for _, locker := range contenders {
		go func() {
			<-start
			results <- result{locker, locker.Lock(t.Context())}
		}()
	}
	close(start)
	var winners []dlock.Locker
	for range contenders {
		r := <-results
		if r.err == nil {
			winners = append(winners, r.locker)
		} else if !errors.Is(r.err, dlock.ErrLocked) {
			t.Errorf("concurrent acquire: %v", r.err)
		}
	}
	for _, winner := range winners {
		if err := winner.Unlock(t.Context()); err != nil {
			t.Error(err)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("%d simultaneous acquisitions succeeded, want 1", len(winners))
	}
}

func TestDockerLeasesEtcdRebind(t *testing.T) {
	ctx := t.Context()
	cli, err := clientv3.New(clientv3.Config{Endpoints: []string{dockerEndpoint(t, "DLOCK_ETCD_ADDR")}, DialTimeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	for _, op := range []string{"Refresh", "Unlock"} {
		t.Run(op, func(t *testing.T) {
			key := "gala-dlock-" + rand.Text()
			locker, err := etcd.NewLock(ctx, cli, dlock.WithLockName(key))
			if err != nil {
				t.Fatal(err)
			}
			defer locker.Close()
			if err := locker.Lock(ctx); err != nil {
				t.Fatal(err)
			}
			response, err := cli.Get(ctx, key)
			if err != nil || len(response.Kvs) != 1 {
				t.Fatalf("read held key: %v %v", response, err)
			}
			other, err := cli.Grant(ctx, 30)
			if err != nil {
				t.Fatal(err)
			}
			defer cli.Revoke(ctx, other.ID)
			if _, err := cli.Put(ctx, key, string(response.Kvs[0].Value), clientv3.WithLease(other.ID)); err != nil {
				t.Fatal(err)
			}
			call := locker.Refresh
			if op == "Unlock" {
				call = locker.Unlock
			}
			if err := call(ctx); !errors.Is(err, dlock.ErrLockNotHeld) {
				t.Fatalf("%s after rebind: %v", op, err)
			}
			response, err = cli.Get(ctx, key)
			if err != nil || len(response.Kvs) != 1 || response.Kvs[0].Lease != int64(other.ID) {
				t.Fatalf("rebound key changed: %v %v", response, err)
			}
		})
	}
}
