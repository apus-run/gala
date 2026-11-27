package mongo_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/event"
	driver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/apus-run/gala/components/dlock"
	"github.com/apus-run/gala/components/dlock/mongo"
)

type afterCommandKey struct{}

// TestMongoLeases uses a fresh collection. The command monitor can delay a
// successful reply or interleave another operation after a server-side write.
func TestMongoLeases(t *testing.T) {
	uri := os.Getenv("DLOCK_TEST_MONGODB_URI")
	if uri == "" {
		t.Skip("set DLOCK_TEST_MONGODB_URI to run against MongoDB")
	}
	ctx := t.Context()
	monitor := &event.CommandMonitor{Succeeded: func(ctx context.Context, e *event.CommandSucceededEvent) {
		if f, ok := ctx.Value(afterCommandKey{}).(func(string)); ok {
			f(e.CommandName)
		}
	}}
	client, err := driver.Connect(ctx, options.Client().ApplyURI(uri).SetMonitor(monitor))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	coll := client.Database("gala_dlock_test").Collection(primitive.NewObjectID().Hex())
	t.Cleanup(func() {
		if err := coll.Drop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	factory := mongo.NewClient(client, coll)
	if err := factory.Init(ctx); err != nil {
		t.Fatal(err)
	}
	newLocker := func(t *testing.T, ttl time.Duration) dlock.Locker {
		t.Helper()
		locker, err := factory.NewLock(ctx, dlock.WithLockName(t.Name()), dlock.WithLockTimeout(ttl))
		if err != nil {
			t.Fatal(err)
		}
		return locker
	}
	expire := func(t *testing.T) {
		t.Helper()
		res, err := coll.UpdateOne(ctx, bson.M{"name": t.Name()}, bson.M{"$set": bson.M{"expiredAt": time.Now().Add(-time.Hour)}})
		if err != nil || res.MatchedCount != 1 {
			t.Fatalf("expire: result = %v, error = %v", res, err)
		}
	}

	t.Run("index removal and restoration", func(t *testing.T) {
		locker := newLocker(t, time.Minute)
		if err := locker.Lock(ctx); err != nil {
			t.Fatal(err)
		}
		if err := coll.FindOne(ctx, bson.M{"_id": t.Name(), "name": t.Name()}).Err(); err != nil {
			t.Fatalf("new lock document: %v", err)
		}
		if err := locker.Unlock(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := coll.Indexes().DropOne(ctx, "name_1"); err != nil {
			t.Fatal(err)
		}
		if err := locker.Lock(ctx); err == nil || !strings.Contains(err.Error(), "unique index") {
			t.Fatalf("lock after index removal = %v", err)
		}
		if err := factory.Init(ctx); err != nil {
			t.Fatal(err)
		}
		if err := locker.Lock(ctx); err != nil {
			t.Fatalf("lock after index restoration = %v", err)
		}
	})

	t.Run("legacy ObjectID", func(t *testing.T) {
		oldID := primitive.NewObjectID()
		_, err := coll.InsertOne(ctx, bson.M{"_id": oldID, "name": t.Name(), "token": "old", "expiredAt": time.Now().Add(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		locker := newLocker(t, time.Minute)
		// Drop the secondary index between verification and the upsert. The
		// legacy document still owns this name despite its different _id.
		dropped := false
		dropAfterCheck := context.WithValue(ctx, afterCommandKey{}, func(cmd string) {
			if cmd == "listIndexes" {
				if _, err := coll.Indexes().DropOne(ctx, "name_1"); err != nil {
					t.Fatal(err)
				}
				dropped = true
			}
		})
		if err := locker.Lock(dropAfterCheck); !dropped || !errors.Is(err, dlock.ErrLocked) {
			t.Fatalf("dropped = %v, lock during old lease = %v", dropped, err)
		}
		if err := factory.Init(ctx); err != nil {
			t.Fatal(err)
		}
		expire(t)
		if err := locker.Lock(ctx); err != nil {
			t.Fatal(err)
		}
		if err := coll.FindOne(ctx, bson.M{"_id": oldID, "token": bson.M{"$ne": "old"}}).Err(); err != nil {
			t.Fatalf("legacy document was not taken over in place: %v", err)
		}
	})

	t.Run("expired cleanup after reacquisition", func(t *testing.T) {
		locker := newLocker(t, time.Minute)
		if err := locker.Lock(ctx); err != nil {
			t.Fatal(err)
		}
		expire(t)
		reacquired := false
		afterDelete := context.WithValue(ctx, afterCommandKey{}, func(cmd string) {
			if cmd == "delete" && !reacquired {
				if err := locker.Lock(ctx); err != nil {
					t.Fatal(err)
				}
				reacquired = true
			}
		})
		if err := locker.Unlock(afterDelete); !errors.Is(err, dlock.ErrLockNotHeld) {
			t.Fatalf("expired Unlock = %v", err)
		}
		if err := locker.Refresh(ctx); !reacquired || err != nil {
			t.Fatalf("reacquired = %v, Refresh = %v", reacquired, err)
		}
	})

	for _, operation := range []string{"insert", "takeover", "refresh"} {
		t.Run("slow "+operation, func(t *testing.T) {
			const ttl = 100 * time.Millisecond
			locker := newLocker(t, ttl)
			if operation == "refresh" {
				if err := locker.Lock(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if operation == "takeover" {
				if err := newLocker(t, time.Minute).Lock(ctx); err != nil {
					t.Fatal(err)
				}
				expire(t)
			}

			// A takeover writes the lease in the second update, after the upsert matched.
			updates := 0
			delayed := false
			slowReply := context.WithValue(ctx, afterCommandKey{}, func(cmd string) {
				if cmd == "update" {
					updates++
					if operation != "takeover" || updates == 2 {
						time.Sleep(ttl)
						delayed = true
					}
				}
			})
			op := locker.Lock
			if operation == "refresh" {
				op = locker.Refresh
			}
			if err := op(slowReply); !delayed || !errors.Is(err, dlock.ErrLockNotHeld) {
				t.Fatalf("delayed = %v, error = %v", delayed, err)
			}
		})
	}
}
