package mongo_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"

	"github.com/apus-run/gala/components/dlock"
	"github.com/apus-run/gala/components/dlock/mongo"
)

func TestMongoProtocol(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("document and index responses", func(mt *mtest.T) {
		ctx := mt.Context()
		f := mongo.NewClient(mt.Client, mt.Coll)
		if l, err := f.NewLock(ctx, dlock.WithLockName("")); l != nil || err == nil {
			mt.Fatal("accepted empty name")
		}
		lock, err := f.NewLock(ctx)
		if err != nil {
			mt.Fatal(err)
		}
		ns := mt.DB.Name() + "." + mt.Coll.Name()
		cursor := func(docs ...bson.D) bson.D { return mtest.CreateCursorResponse(0, ns, mtest.FirstBatch, docs...) }
		index := cursor(bson.D{{Key: "key", Value: bson.D{{Key: "name", Value: 1}}}, {Key: "unique", Value: true}})
		write := func(n int) bson.D { return mtest.CreateSuccessResponse(bson.E{Key: "n", Value: n}) }
		insert := append(write(1), bson.E{Key: "upserted", Value: bson.A{bson.D{{Key: "index", Value: 0}, {Key: "_id", Value: "lock"}}}})
		fail := mtest.CreateCommandErrorResponse(mtest.CommandError{Code: 13, Message: "denied"})
		dup := mtest.CreateWriteErrorsResponse(mtest.WriteError{Code: 11000, Message: "duplicate"})
		peer := cursor(bson.D{{Key: "ownerID", Value: "worker"}})
		for _, tc := range []struct {
			name    string
			op      func(context.Context) error
			replies []bson.D
			want    error
			message string
		}{
			{"init", f.Init, []bson.D{write(1)}, nil, ""},
			{"index creation denied", f.Init, []bson.D{fail}, nil, "denied"},
			{"index query denied", lock.Lock, []bson.D{fail}, nil, "denied"},
			{"index cursor fails", lock.Lock, []bson.D{mtest.CreateCursorResponse(17, ns, mtest.FirstBatch, bson.D{{Key: "name", Value: "_id_"}}), fail, write(1)}, nil, "decode indexes"},
			{"missing index", lock.Lock, []bson.D{cursor()}, nil, "unique index"},
			{"new document", lock.Lock, []bson.D{index, insert}, nil, ""},
			{"upsert denied", lock.Lock, []bson.D{index, fail}, nil, "denied"},
			{"takeover denied", lock.Lock, []bson.D{index, write(1), fail}, nil, "denied"},
			{"duplicate during upsert", lock.Lock, []bson.D{index, dup, write(1)}, nil, ""},
			{"occupied", lock.Lock, []bson.D{index, write(1), write(0), write(1), write(0), peer}, dlock.ErrLocked, "worker"},
			{"holder read denied", lock.Lock, []bson.D{index, write(1), write(0), write(1), write(0), fail}, dlock.ErrLocked, ""},
			{"release", lock.Unlock, []bson.D{write(1)}, nil, ""},
			{"release denied", lock.Unlock, []bson.D{fail}, nil, "denied"},
			{"expired release", lock.Unlock, []bson.D{write(0), write(1)}, dlock.ErrLockNotHeld, ""},
			{"renew", lock.Refresh, []bson.D{write(1)}, nil, ""},
			{"renew denied", lock.Refresh, []bson.D{fail}, nil, "denied"},
			{"lost before renew", lock.Refresh, []bson.D{write(0)}, dlock.ErrLockNotHeld, ""},
		} {
			mt.ClearMockResponses()
			mt.AddMockResponses(tc.replies...)
			err := tc.op(ctx)
			if tc.want != nil && !errors.Is(err, tc.want) {
				mt.Fatalf("%s: want %v, got %v", tc.name, tc.want, err)
			}
			if tc.message != "" && (err == nil || !strings.Contains(err.Error(), tc.message)) {
				mt.Fatalf("%s: want %q, got %v", tc.name, tc.message, err)
			}
			if tc.want == nil && tc.message == "" && err != nil {
				mt.Fatalf("%s: %v", tc.name, err)
			}
		}
	})
}
