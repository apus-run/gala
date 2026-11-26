package etcd

import (
	"context"
	"errors"
	"testing"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/apus-run/gala/components/dlock"
)

type fakeUnlockKV struct {
	clientv3.KV
	resp *clientv3.GetResponse
	err  error
}

func (f fakeUnlockKV) Get(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	return f.resp, f.err
}

type fakeUnlockLease struct {
	clientv3.Lease
	revoked int
	err     error
}

func (f *fakeUnlockLease) Revoke(context.Context, clientv3.LeaseID) (*clientv3.LeaseRevokeResponse, error) {
	f.revoked++
	return &clientv3.LeaseRevokeResponse{}, f.err
}

func newUnlockTestLock(resp *clientv3.GetResponse, kvErr error) (*Lock, *fakeUnlockLease) {
	lease := &fakeUnlockLease{}
	l := &Lock{
		cli:     &clientv3.Client{KV: fakeUnlockKV{resp: resp, err: kvErr}},
		lease:   lease,
		lockKey: "lock",
		token:   "owner@token",
		held:    true,
		leaseID: 1,
	}
	return l, lease
}

func TestUnlockChecksKeyOwnership(t *testing.T) {
	ctx := context.Background()

	t.Run("owned key releases", func(t *testing.T) {
		l, lease := newUnlockTestLock(&clientv3.GetResponse{
			Kvs: []*mvccpb.KeyValue{{Key: []byte("lock"), Value: []byte("owner@token")}},
		}, nil)
		if err := l.Unlock(ctx); err != nil {
			t.Fatalf("Unlock() error = %v", err)
		}
		if lease.revoked != 1 {
			t.Fatalf("Revoke calls = %d, want 1", lease.revoked)
		}
		if l.held {
			t.Fatal("Unlock() left the lock held")
		}
	})

	t.Run("missing key reports not held", func(t *testing.T) {
		l, lease := newUnlockTestLock(&clientv3.GetResponse{}, nil)
		err := l.Unlock(ctx)
		if !errors.Is(err, dlock.ErrLockNotHeld) {
			t.Fatalf("Unlock() error = %v, want ErrLockNotHeld", err)
		}
		if lease.revoked != 1 {
			t.Fatalf("discard Revoke calls = %d, want 1", lease.revoked)
		}
		if l.held {
			t.Fatal("Unlock() left the lock held after losing it")
		}
	})

	t.Run("other token reports not held", func(t *testing.T) {
		l, lease := newUnlockTestLock(&clientv3.GetResponse{
			Kvs: []*mvccpb.KeyValue{{Key: []byte("lock"), Value: []byte("other@token")}},
		}, nil)
		err := l.Unlock(ctx)
		if !errors.Is(err, dlock.ErrLockNotHeld) {
			t.Fatalf("Unlock() error = %v, want ErrLockNotHeld", err)
		}
		if lease.revoked != 1 {
			t.Fatalf("discard Revoke calls = %d, want 1", lease.revoked)
		}
	})

	t.Run("ownership read error keeps the lease", func(t *testing.T) {
		l, lease := newUnlockTestLock(nil, errors.New("etcd unavailable"))
		err := l.Unlock(ctx)
		if err == nil || errors.Is(err, dlock.ErrLockNotHeld) {
			t.Fatalf("Unlock() error = %v, want a transient error", err)
		}
		if lease.revoked != 0 {
			t.Fatalf("Revoke calls = %d, want 0", lease.revoked)
		}
		if !l.held {
			t.Fatal("Unlock() dropped the lease after a transient read error")
		}
	})
}
