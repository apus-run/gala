//go:build integration

package authz

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	gormadapter "github.com/apus-run/gala/components/authz/adapter/gorm"
	redisadapter "github.com/apus-run/gala/components/authz/adapter/redis"
	"github.com/casbin/casbin/v3/model"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type dockerRedisClient interface {
	redis.Cmdable
	redisadapter.WatcherClient
	Close() error
}

func dockerEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		t.Fatalf("%s must be set; use integration/run.py", key)
	}
	return value
}
func dockerRedis(t *testing.T, cluster bool) dockerRedisClient {
	t.Helper()
	var client dockerRedisClient
	if cluster {
		addresses := strings.Split(dockerEnv(t, "AUTHZ_CLUSTER_ADDRS"), ",")
		if len(addresses) != 3 {
			t.Fatal("expected three cluster endpoints")
		}
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs: addresses, DialTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, ContextTimeoutEnabled: true, MaxRetries: -1,
			// Cluster nodes announce their in-container loopback ports. Translate only
			// those ports to Compose's random host mappings; no fixed host ports needed.
			Dialer: func(ctx context.Context, network, address string) (net.Conn, error) {
				_, port, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				switch port {
				case "7000":
					address = addresses[0]
				case "7001":
					address = addresses[1]
				case "7002":
					address = addresses[2]
				}
				return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, address)
			},
		})
	} else {
		client = redis.NewClient(&redis.Options{Addr: dockerEnv(t, "AUTHZ_REDIS_ADDR"), DialTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, ContextTimeoutEnabled: true, MaxRetries: -1})
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	return client
}

func dockerPolicy(t *testing.T, subjects ...string) model.Model {
	t.Helper()
	m, err := model.NewModelFromString(testAllowModel)
	if err != nil {
		t.Fatal(err)
	}
	for _, subject := range subjects {
		m.AddPolicy("p", "p", []string{subject, "/reports", "GET"})
	}
	return m
}
func dockerAdapter(t *testing.T, c redis.Cmdable, key string) *redisadapter.Adapter {
	t.Helper()
	adapter, err := redisadapter.NewAdapter(&redisadapter.Config{Client: c, Key: key})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}
func dockerAwait(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not converge within 8s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func dockerWatchAuthorizer(t *testing.T, c dockerRedisClient, key, channel string) (*Authorizer, *redisadapter.Watcher) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	watcher, err := redisadapter.NewWatcher(ctx, c, channel)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(watcher.Close)
	a := newSyncAuthorizer(t, dockerAdapter(t, c, key), WithWatcher(watcher), WithWatcherErrorHandler(func(err error) { t.Errorf("watcher: %v", err) }))
	return a, watcher
}

func TestDockerRedisSnapshotsAndWatchers(t *testing.T) {
	for _, cluster := range []bool{false, true} {
		t.Run(map[bool]string{false: "standalone", true: "cluster"}[cluster], func(t *testing.T) {
			c := dockerRedis(t, cluster)
			ctx := context.Background()
			tag := "docker-" + rand.Text()
			key := "authz:{" + tag + "}:rules"
			channel := "authz:" + tag + ":changed"
			adapter := dockerAdapter(t, c, key)
			old, next := dockerPolicy(t, "alice"), dockerPolicy(t, "bob", "carol", "dave")
			if err := adapter.SavePolicy(old); err != nil {
				t.Fatal(err)
			}
			// Readers must never observe a prefix or a transient empty active List.
			failures := make(chan error, 1)
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 50; i++ {
					snapshot := old
					if i%2 == 0 {
						snapshot = next
					}
					if err := adapter.SavePolicy(snapshot); err != nil {
						failures <- err
						return
					}
				}
			}()
			for i := 0; i < 100; i++ {
				m := dockerPolicy(t)
				if err := adapter.LoadPolicy(m); err != nil {
					t.Error(err)
					break
				}
				p := m["p"]["p"].Policy
				if !reflect.DeepEqual(p, old["p"]["p"].Policy) && !reflect.DeepEqual(p, next["p"]["p"].Policy) {
					t.Errorf("partial snapshot: %v", p)
					break
				}
			}
			wg.Wait()
			select {
			case err := <-failures:
				t.Fatal(err)
			default:
			}
			if err := adapter.SavePolicy(old); err != nil {
				t.Fatal(err)
			}
			a, _ := dockerWatchAuthorizer(t, c, key, channel)
			peer, peerWatcher := dockerWatchAuthorizer(t, c, key, channel)
			assertAlice(t, a, true)
			assertAlice(t, peer, true)
			if err := adapter.SavePolicy(dockerPolicy(t)); err != nil {
				t.Fatal(err)
			}
			if err := a.SyncPolicy(); err != nil {
				t.Fatal(err)
			}
			dockerAwait(t, func() bool {
				ok, err := peer.Enforce("alice", "/reports", "GET")
				if err != nil {
					t.Error(err)
				}
				return !ok && err == nil
			})
			// Deliberately miss a notification. Periodic reconciliation must still repair
			// the peer without another publish or shared Authorizer memory.
			peerWatcher.Close()
			if err := adapter.SavePolicy(old); err != nil {
				t.Fatal(err)
			}
			if err := a.SyncPolicy(); err != nil {
				t.Fatal(err)
			}
			assertAlice(t, peer, false)
			reloadCtx, stop := context.WithCancel(ctx)
			done := make(chan error, 1)
			defer func() {
				stop()
				if err := <-done; !errors.Is(err, context.Canceled) {
					t.Error(err)
				}
			}()
			go func() { done <- peer.RunPolicyReload(reloadCtx, 20*time.Millisecond, func(err error) { t.Error(err) }) }()
			dockerAwait(t, func() bool { ok, err := peer.Enforce("alice", "/reports", "GET"); return ok && err == nil })
			stop()
			if err := adapter.Close(); err != nil {
				t.Fatal(err)
			}
			if err := c.Ping(ctx).Err(); err != nil {
				t.Fatal("adapter closed injected client", err)
			}
		})
	}
}

func TestDockerClusterPublishesInEveryMasterSlotRange(t *testing.T) {
	c := dockerRedis(t, true)
	ctx := context.Background()
	slots, err := c.ClusterSlots(ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 3 {
		t.Fatalf("expected three primary slot ranges, got %v", slots)
	}
	covered := map[int]bool{}
	for i := 0; i < 500 && len(covered) < len(slots); i++ {
		key := fmt.Sprintf("authz:{range-%d}:rules", i)
		slot, err := c.ClusterKeySlot(ctx, key).Result()
		if err != nil {
			t.Fatal(err)
		}
		for rangeIndex, slotRange := range slots {
			if slot >= int64(slotRange.Start) && slot <= int64(slotRange.End) && !covered[rangeIndex] {
				adapter := dockerAdapter(t, c, key)
				if err := adapter.SavePolicy(dockerPolicy(t, "alice")); err != nil {
					t.Fatalf("slot %d: %v", slot, err)
				}
				m := dockerPolicy(t)
				if err := adapter.LoadPolicy(m); err != nil || len(m["p"]["p"].Policy) != 1 {
					t.Fatalf("slot %d load: %v", slot, err)
				}
				if err := adapter.SavePolicy(dockerPolicy(t)); err != nil {
					t.Fatal(err)
				}
				covered[rangeIndex] = true
				t.Logf("published and cleared snapshot in slots %d-%d (key slot %d)", slotRange.Start, slotRange.End, slot)
			}
		}
	}
	if len(covered) != len(slots) {
		t.Fatalf("covered %d of %d slot ranges", len(covered), len(slots))
	}
}

func TestDockerRedisACLFailuresPreserveDeny(t *testing.T) {
	admin := dockerRedis(t, false)
	ctx := context.Background()
	const denyModel = `[request_definition]
r = sub, obj, act
[policy_definition]
p = sub, obj, act, eft
[policy_effect]
e = some(where (p.eft == allow)) && !some(where (p.eft == deny))
[matchers]
m = r.sub == p.sub && r.obj == p.obj && r.act == p.act
`
	for _, denied := range []string{"rpush", "pexpire", "persist", "rename", "del"} {
		t.Run(denied, func(t *testing.T) {
			id := rand.Text()
			key := "authz:{acl-" + id + "}:rules"
			user := "authz_" + id
			writer := dockerAdapter(t, admin, key)
			m, err := model.NewModelFromString(denyModel)
			if err != nil {
				t.Fatal(err)
			}
			m.AddPolicy("p", "p", []string{"alice", "/reports", "GET", "allow"})
			m.AddPolicy("p", "p", []string{"alice", "/reports", "GET", "deny"})
			if err := writer.SavePolicy(m); err != nil {
				t.Fatal(err)
			}
			a, err := New(WithModelFilePath(writeModelFile(t, denyModel)), WithPolicyAdapter(dockerAdapter(t, admin, key)), WithMaxPolicyAge(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if err := admin.ACLSetUser(ctx, user, "on", ">integration-only", "~*", "&*", "+@all", "-"+denied).Err(); err != nil {
				t.Fatal(err)
			}
			defer admin.ACLDelUser(ctx, user)
			restricted := redis.NewClient(&redis.Options{Addr: dockerEnv(t, "AUTHZ_REDIS_ADDR"), Username: user, Password: "integration-only", ContextTimeoutEnabled: true, MaxRetries: -1, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second})
			defer restricted.Close()
			replacement := m
			if denied == "del" {
				replacement, _ = model.NewModelFromString(denyModel)
			}
			err = dockerAdapter(t, restricted, key).SavePolicy(replacement)
			if err == nil {
				t.Fatal("ACL-restricted write succeeded")
			}
			message := strings.ToUpper(err.Error())
			if !strings.Contains(message, "NOPERM") && !strings.Contains(message, "ACL FAILURE") {
				t.Fatalf("expected ACL failure, got %v", err)
			}
			if err := a.LoadPolicy(); err != nil {
				t.Fatal(err)
			}
			assertAlice(t, a, false)
			n, err := admin.LLen(ctx, key).Result()
			if err != nil || n != 2 {
				t.Fatalf("ACL failure changed active policy: len=%d err=%v", n, err)
			}
		})
	}
}

func TestDockerRedisWatcherReconnect(t *testing.T) {
	c := dockerRedis(t, false)
	ctx := context.Background()
	id := rand.Text()
	key := "authz:{reconnect-" + id + "}:rules"
	channel := "reconnect:" + id
	adapter := dockerAdapter(t, c, key)
	if err := adapter.SavePolicy(dockerPolicy(t, "alice")); err != nil {
		t.Fatal(err)
	}
	a, _ := dockerWatchAuthorizer(t, c, key, channel)
	peer, _ := dockerWatchAuthorizer(t, c, key, channel)
	killed, err := c.ClientKillByFilter(ctx, "TYPE", "pubsub").Result()
	if err != nil || killed < 2 {
		t.Fatalf("kill subscribers: n=%d err=%v", killed, err)
	}
	dockerAwait(t, func() bool { n, err := c.PubSubNumSub(ctx, channel).Result(); return err == nil && n[channel] == 2 })
	if err := adapter.SavePolicy(dockerPolicy(t)); err != nil {
		t.Fatal(err)
	}
	if err := a.SyncPolicy(); err != nil {
		t.Fatal(err)
	}
	dockerAwait(t, func() bool { ok, err := peer.Enforce("alice", "/reports", "GET"); return !ok && err == nil })
}

func TestDockerGORMCommittedPolicyAndTimeout(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			var dialector gorm.Dialector
			if driver == "mysql" {
				dialector = mysql.Open(dockerEnv(t, "AUTHZ_MYSQL_DSN"))
			} else {
				dialector = postgres.Open(dockerEnv(t, "AUTHZ_POSTGRES_DSN"))
			}
			db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			defer sqlDB.Close()
			sqlDB.SetMaxOpenConns(5)
			modelPath := writeModelFile(t, testAllowModel)
			// These are fresh, disposable databases owned by this Compose run.
			if db.Migrator().HasTable(&gormadapter.CasbinRule{}) {
				t.Fatal("expected fresh database")
			}
			a, err := NewAuthz(db, WithModelFilePath(modelPath))
			if a != nil || err == nil || db.Migrator().HasTable(&gormadapter.CasbinRule{}) {
				t.Fatalf("constructor migrated schema or accepted missing table: a=%v err=%v", a, err)
			}
			if err := db.AutoMigrate(&gormadapter.CasbinRule{}); err != nil {
				t.Fatal(err)
			}
			rules := []gormadapter.CasbinRule{{Ptype: "p", V0: "reader", V1: "/reports", V2: "GET"}, {Ptype: "g", V0: "alice", V1: "reader"}}
			if err := db.Create(&rules).Error; err != nil {
				t.Fatal(err)
			}
			redisClient := dockerRedis(t, false)
			channel := "db:" + driver + ":" + rand.Text()
			var instances []*Authorizer
			var watchers []*redisadapter.Watcher
			for range 2 {
				w, err := redisadapter.NewWatcher(context.Background(), redisClient, channel)
				if err != nil {
					t.Fatal(err)
				}
				defer w.Close()
				watchers = append(watchers, w)
				// Caller query conditions must not leak into the adapter's clean session.
				instance, err := NewAuthz(db.Where("v0 = ?", "must-not-filter"), WithModelFilePath(modelPath), WithWatcher(w), WithWatcherErrorHandler(func(err error) { t.Error(err) }), WithPolicyLoadTimeout(200*time.Millisecond))
				if err != nil {
					t.Fatal(err)
				}
				instances = append(instances, instance)
				assertAlice(t, instance, true)
				roles, err := instance.GetRolesForUser("alice")
				if err != nil || len(roles) != 1 || roles[0] != "reader" {
					t.Fatalf("roles=%v err=%v", roles, err)
				}
			}
			rollback := errors.New("rollback")
			err = db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Where("ptype = ? AND v0 = ?", "g", "alice").Delete(&gormadapter.CasbinRule{}).Error; err != nil {
					return err
				}
				if err := instances[0].LoadPolicy(); err != nil {
					return err
				}
				assertAlice(t, instances[0], true)
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatal(err)
			}
			if err := instances[0].LoadPolicy(); err != nil {
				t.Fatal(err)
			}
			assertAlice(t, instances[0], true)
			if err := db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Where("ptype = ? AND v0 = ?", "g", "alice").Delete(&gormadapter.CasbinRule{}).Error; err != nil {
					return err
				}
				return tx.Create(&gormadapter.CasbinRule{Ptype: "g", V0: "bob", V1: "reader"}).Error
			}); err != nil {
				t.Fatal(err)
			}
			if err := instances[0].SyncPolicy(); err != nil {
				t.Fatal(err)
			}
			dockerAwait(t, func() bool { ok, err := instances[1].Enforce("bob", "/reports", "GET"); return ok && err == nil })
			for _, instance := range instances {
				assertAlice(t, instance, false)
			}
			// Stop asynchronous callbacks before deliberately blocking reads.
			for _, w := range watchers {
				w.Close()
			}
			conn, err := sqlDB.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			var tx *sql.Tx
			if driver == "postgres" {
				tx, err = conn.BeginTx(context.Background(), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if _, err := tx.ExecContext(context.Background(), "LOCK TABLE casbin_rule IN ACCESS EXCLUSIVE MODE"); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := conn.ExecContext(context.Background(), "LOCK TABLES casbin_rule WRITE"); err != nil {
					t.Fatal(err)
				}
				defer conn.ExecContext(context.Background(), "UNLOCK TABLES")
			}
			started := time.Now()
			err = instances[0].LoadPolicy()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("locked read should time out: %v", err)
			}
			if elapsed := time.Since(started); elapsed > 3*time.Second {
				t.Fatalf("read timeout not bounded: %v", elapsed)
			}
			if ok, err := instances[0].Enforce("bob", "/reports", "GET"); err != nil || !ok {
				t.Fatalf("failed read lost previous policy: allowed=%v err=%v", ok, err)
			}
			if driver == "postgres" {
				if err := tx.Rollback(); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := conn.ExecContext(context.Background(), "UNLOCK TABLES"); err != nil {
					t.Fatal(err)
				}
			}
			if err := instances[0].LoadPolicy(); err != nil {
				t.Fatal("next load did not get fresh timeout", err)
			}
			if err := sqlDB.Ping(); err != nil {
				t.Fatal("authorizer closed injected database", err)
			}
			dockerCheckGORMRollback(t, db)
		})
	}
}

func TestDockerRedisReadFailureExpiresAndRecovers(t *testing.T) {
	admin := dockerRedis(t, false)
	ctx := context.Background()
	id := rand.Text()
	key := "authz:{expiry-" + id + "}:rules"
	user := "expiry_" + id
	writer := dockerAdapter(t, admin, key)
	if err := writer.SavePolicy(dockerPolicy(t, "alice")); err != nil {
		t.Fatal(err)
	}
	if err := admin.ACLSetUser(ctx, user, "on", ">integration-only", "~*", "&*", "+@all").Err(); err != nil {
		t.Fatal(err)
	}
	defer admin.ACLDelUser(ctx, user)
	client := redis.NewClient(&redis.Options{Addr: dockerEnv(t, "AUTHZ_REDIS_ADDR"), Username: user, Password: "integration-only", ContextTimeoutEnabled: true, MaxRetries: -1, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second})
	defer client.Close()
	a := newSyncAuthorizer(t, dockerAdapter(t, client, key), WithMaxPolicyAge(300*time.Millisecond))
	if err := admin.ACLSetUser(ctx, user, "-lrange").Err(); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadPolicy(); err == nil {
		t.Fatal("expected denied policy read")
	}
	assertAlice(t, a, true)
	dockerAwait(t, func() bool {
		allowed, err := a.Enforce("alice", "/reports", "GET")
		return !allowed && errors.Is(err, ErrPolicyStale)
	})
	if roles, err := a.GetRolesForUser("alice"); roles != nil || !errors.Is(err, ErrPolicyStale) {
		t.Fatalf("stale roles=%v err=%v", roles, err)
	}
	if err := writer.SavePolicy(dockerPolicy(t)); err != nil {
		t.Fatal(err)
	}
	if err := admin.ACLSetUser(ctx, user, "+lrange").Err(); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadPolicy(); err != nil {
		t.Fatal(err)
	}
	assertAlice(t, a, false)
}

func TestDockerRedisLiteralFilteringAndMalformedPolicy(t *testing.T) {
	for _, cluster := range []bool{false, true} {
		t.Run(map[bool]string{false: "standalone", true: "cluster"}[cluster], func(t *testing.T) {
			client := dockerRedis(t, cluster)
			for _, value := range []string{`alice"`, `alice\`, `alice&bob`, `<alice>`, `用户🙂`} {
				t.Run(value, func(t *testing.T) {
					key := "authz:{filter-" + rand.Text() + "}:rules"
					adapter := dockerAdapter(t, client, key)
					target, other := []string{value, "/reports", "GET"}, []string{value + "-other", "/reports", "GET"}
					for _, rule := range [][]string{target, other} {
						if err := adapter.AddPolicy("p", "p", rule); err != nil {
							t.Fatal(err)
						}
					}
					filtered := dockerPolicy(t)
					if err := adapter.LoadFilteredPolicy(filtered, redisadapter.Filter{V0: []string{value}}); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(filtered["p"]["p"].Policy, [][]string{target}) {
						t.Fatalf("literal filter mismatch: %v", filtered["p"]["p"].Policy)
					}
					a := newSyncAuthorizer(t, dockerAdapter(t, client, key))
					if _, err := adapter.UpdateFilteredPolicies("p", "p", [][]string{{value, "/reports", "POST"}}, 0, value); err != nil {
						t.Fatal(err)
					}
					if err := a.LoadPolicy(); err != nil {
						t.Fatal(err)
					}
					if ok, err := a.Enforce(value, "/reports", "GET"); err != nil || ok {
						t.Fatalf("old grant survived update: %v %v", ok, err)
					}
					if ok, err := a.Enforce(value, "/reports", "POST"); err != nil || !ok {
						t.Fatalf("new grant missing: %v %v", ok, err)
					}
					if err := adapter.RemoveFilteredPolicy("p", "p", 0, value); err != nil {
						t.Fatal(err)
					}
					if err := a.LoadPolicy(); err != nil {
						t.Fatal(err)
					}
					if ok, err := a.Enforce(value, "/reports", "POST"); err != nil || ok {
						t.Fatalf("revoked grant survived reload: %v %v", ok, err)
					}
					if ok, err := a.Enforce(other[0], "/reports", "GET"); err != nil || !ok {
						t.Fatalf("unrelated grant lost: %v %v", ok, err)
					}
					if err := client.RPush(context.Background(), key, "null").Err(); err != nil {
						t.Fatal(err)
					}
					before := a.lastLoad.Load()
					if err := a.LoadPolicy(); err == nil {
						t.Fatal("malformed row accepted")
					}
					if a.lastLoad.Load() != before {
						t.Fatal("malformed row renewed policy age")
					}
					if ok, err := a.Enforce(other[0], "/reports", "GET"); err != nil || !ok {
						t.Fatalf("malformed reload lost old snapshot: %v %v", ok, err)
					}
				})
			}
		})
	}
}

func dockerCheckGORMRollback(t *testing.T, db *gorm.DB) {
	t.Helper()
	adapter, err := gormadapter.NewAdapterByDB(db)
	if err != nil {
		t.Fatal(err)
	}
	var before []gormadapter.CasbinRule
	if err := db.Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	// CHECK works on both actual MySQL 8.4 and PostgreSQL 16. Its failure occurs
	// after SavePolicy has deleted old rows, exercising transaction rollback.
	if err := db.Exec("ALTER TABLE casbin_rule ADD CONSTRAINT reject_test_policy CHECK (v0 <> '__reject__')").Error; err != nil {
		t.Fatal(err)
	}
	if err := adapter.SavePolicy(dockerPolicy(t, "__reject__")); err == nil {
		t.Fatal("expected check constraint failure")
	}
	var after []gormadapter.CasbinRule
	if err := db.Order("id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("failed save changed policy: before=%v after=%v", before, after)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := adapter.SavePolicyCtx(ctx, dockerPolicy(t, "carol")); err == nil {
		t.Fatal("cancelled save succeeded")
	}
	after = nil
	if err := db.Order("id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("cancelled save changed policy")
	}
	if err := adapter.SavePolicy(dockerPolicy(t, "carol")); err != nil {
		t.Fatal("retry failed", err)
	}
	loaded := dockerPolicy(t)
	if err := adapter.LoadPolicy(loaded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded["p"]["p"].Policy, [][]string{{"carol", "/reports", "GET"}}) {
		t.Fatalf("replacement failed: %v", loaded["p"]["p"].Policy)
	}
	t.Log("constraint failure and canceled save retained exact old rows; retry replaced the snapshot")
}
