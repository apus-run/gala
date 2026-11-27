package etcd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.etcd.io/etcd/api/v3/mvccpb"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/apus-run/gala/components/dlock"
)

type acquireTxn struct {
	succeeded bool
	err       error
}

func (tx *acquireTxn) If(...clientv3.Cmp) clientv3.Txn { return tx }

func (tx *acquireTxn) Then(...clientv3.Op) clientv3.Txn { return tx }

func (tx *acquireTxn) Else(...clientv3.Op) clientv3.Txn { return tx }

func (tx *acquireTxn) Commit() (*clientv3.TxnResponse, error) {
	return &clientv3.TxnResponse{Succeeded: tx.succeeded}, tx.err
}

type acquireKV struct {
	fakeUnlockKV
	tx *acquireTxn
}

func (k acquireKV) Txn(context.Context) clientv3.Txn { return k.tx }

type acquireLease struct {
	fakeUnlockLease
	grantErr, renewErr error
	ttl                int64
}

func (l *acquireLease) Grant(_ context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
	l.ttl = ttl
	return &clientv3.LeaseGrantResponse{ID: 2}, l.grantErr
}

func (l *acquireLease) KeepAliveOnce(context.Context, clientv3.LeaseID) (*clientv3.LeaseKeepAliveResponse, error) {
	return &clientv3.LeaseKeepAliveResponse{TTL: 2}, l.renewErr
}

func TestEtcdLeaseTransitions(t *testing.T) {
	unavailable := errors.New("unavailable")
	for _, tc := range []struct {
		name                                          string
		method                                        func(*Lock, context.Context) error
		held, owned, won                              bool
		getErr, renewErr, grantErr, txnErr, revokeErr error
		want                                          error
		wantHeld                                      bool
		wantRevokes                                   int
	}{
		{name: "grant failure", method: (*Lock).Lock, grantErr: unavailable, want: unavailable},
		{name: "commit failure", method: (*Lock).Lock, txnErr: unavailable, want: unavailable, wantRevokes: 1},
		{name: "held by peer", method: (*Lock).Lock, want: dlock.ErrLocked, wantRevokes: 1},
		{name: "unknown holder", method: (*Lock).Lock, getErr: unavailable, want: dlock.ErrLocked, wantRevokes: 1},
		{name: "acquired", method: (*Lock).Lock, won: true, wantHeld: true},
		{name: "retry while owned", method: (*Lock).Lock, held: true, owned: true, wantHeld: true},
		{name: "reacquire expired lease", method: (*Lock).Lock, held: true, renewErr: rpctypes.ErrLeaseNotFound, won: true, wantHeld: true, wantRevokes: 1},
		{name: "retry read fails", method: (*Lock).Lock, held: true, getErr: unavailable, want: unavailable, wantHeld: true},
		{name: "retry renew fails", method: (*Lock).Lock, held: true, renewErr: unavailable, want: unavailable, wantHeld: true},
		{name: "refresh without lease", method: (*Lock).Refresh, want: dlock.ErrLockNotHeld},
		{name: "refresh expired", method: (*Lock).Refresh, held: true, renewErr: rpctypes.ErrLeaseNotFound, want: dlock.ErrLockNotHeld},
		{name: "refresh unavailable", method: (*Lock).Refresh, held: true, renewErr: unavailable, want: unavailable, wantHeld: true},
		{name: "refresh read fails", method: (*Lock).Refresh, held: true, getErr: unavailable, want: unavailable, wantHeld: true},
		{name: "refresh succeeds", method: (*Lock).Refresh, held: true, owned: true, wantHeld: true},
		{name: "release without lease", method: (*Lock).Unlock, want: dlock.ErrLockNotHeld},
		{name: "lease gone during release", method: (*Lock).Unlock, held: true, owned: true, revokeErr: rpctypes.ErrLeaseNotFound, want: dlock.ErrLockNotHeld, wantRevokes: 1},
		{name: "release can retry", method: (*Lock).Unlock, held: true, owned: true, revokeErr: unavailable, want: unavailable, wantHeld: true, wantRevokes: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := "peer@token"
			if tc.owned {
				value = "owner@token"
			}
			reply := &clientv3.GetResponse{Kvs: []*mvccpb.KeyValue{{Value: []byte(value), Lease: 1}}}
			l, _ := newUnlockTestLock(reply, tc.getErr)
			l.cli.KV = acquireKV{fakeUnlockKV: fakeUnlockKV{resp: reply, err: tc.getErr}, tx: &acquireTxn{tc.won, tc.txnErr}}
			lease := &acquireLease{grantErr: tc.grantErr, renewErr: tc.renewErr, fakeUnlockLease: fakeUnlockLease{err: tc.revokeErr}}
			l.lease, l.held, l.lockTimeout = lease, tc.held, 1500*time.Millisecond
			err := tc.method(l, t.Context())
			if !errors.Is(err, tc.want) || l.held != tc.wantHeld || lease.revoked != tc.wantRevokes {
				t.Fatalf("error=%v held=%v revoked=%d", err, l.held, lease.revoked)
			}
			if tc.won && (lease.ttl != 2 || l.leaseID != 2) {
				t.Fatalf("TTL=%d ID=%d", lease.ttl, l.leaseID)
			}
			if tc.name == "held by peer" && !strings.Contains(err.Error(), "peer") {
				t.Fatal(err)
			}
		})
	}
}

func TestEtcdFactoryAndBusyLocker(t *testing.T) {
	c, err := clientv3.New(clientv3.Config{Endpoints: []string{"127.0.0.1:1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f := NewClient(c)
	if l, err := f.NewLock(t.Context(), dlock.WithLockName("")); l != nil || err == nil {
		t.Fatalf("invalid lock %v %v", l, err)
	}
	locker, err := f.NewLock(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	l := locker.(*Lock)
	defer l.Close()
	if err := l.mu.Acquire(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	defer l.mu.Release(1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := l.Lock(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
