// Package etcd provides a distributed lock built on an etcd lease and key |
// etcd 提供基于 etcd 租约与键的分布式锁.
//
// # Notes | 说明
//
// Lock grants a lease of LockTimeout seconds (rounded up) and creates the key
// with a transaction that requires the key to be absent, so it fails with
// [dlock.ErrLocked] while another holder owns it. Unlock revokes the lease and
// Refresh extends it with KeepAliveOnce; both operate on this Locker's lease
// ID only, so they can never touch another holder's key. When the holder dies
// the lease expires and the key disappears on its own. No background renewal
// is started: long-running work must call Refresh before the lease expires.
//
// Close releases the lease client created by NewLock and stops its background
// goroutines. It does not revoke a held lease, so call Unlock before Close.
// When the Locker is obtained through dlock.Client, use an io.Closer
// assertion to reach Close; later Lock/Refresh/Unlock calls return
// [dlock.ErrClosed].
// Lock 创建时长向上取整到秒的租约，并用“key 不存在”事务条件写入；他人持有时
// 返回 [dlock.ErrLocked]。Unlock 撤销本 Locker 的租约，Refresh 用
// KeepAliveOnce 续期；两者只操作自己的 lease ID，不会碰他人的 key。持有者
// 死亡后租约到期，key 自行消失。不会启动后台续期：长任务必须在租约到期前
// 调用 Refresh。
//
// Close 释放 NewLock 创建的 lease client 并停止其后台 goroutine。它不会撤销
// 仍持有的租约，所以先 Unlock 再 Close。通过 dlock.Client 获取 Locker 时，
// 用 io.Closer 断言拿到 Close；Close 之后的 Lock/Refresh/Unlock 返回
// [dlock.ErrClosed]。
//
// # Example | 示例
//
//	locker, err := etcd.NewLock(ctx, etcdCli, dlock.WithLockName("export"))
//	if err != nil {
//		return err
//	}
//	if err := locker.Lock(ctx); err != nil { /* held elsewhere */ }
//	defer locker.Close() // stops the lease client's goroutines
//	defer locker.Unlock(ctx)
package etcd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/apus-run/gala/components/dlock"
	"github.com/apus-run/gala/components/dlock/internal/cleanup"
	"github.com/apus-run/gala/components/dlock/internal/opctx"
	"github.com/apus-run/gala/components/dlock/internal/token"
)

// Lock provides a distributed locking mechanism using etcd | Lock 提供基于 etcd 的分布式锁机制。
type Lock struct {
	cli              *clientv3.Client
	lease            clientv3.Lease
	lockKey          string
	lockTimeout      time.Duration
	operationTimeout time.Duration
	token            string

	mu      sync.Mutex
	leaseID clientv3.LeaseID
	held    bool
	closed  bool
}

// Ensure Lock implements the Locker interface | 确保 Lock 实现 Locker 接口。
var _ dlock.Locker = (*Lock)(nil)

// Ensure Lock implements io.Closer | 确保 Lock 实现 io.Closer。
var _ io.Closer = (*Lock)(nil)

// NewLock initializes a new Lock instance | NewLock 初始化一个新的 Lock 实例。
func NewLock(_ context.Context, cli *clientv3.Client, opts ...dlock.Option) (*Lock, error) {
	if cli == nil {
		return nil, fmt.Errorf("dlock/etcd: nil etcd client")
	}

	o := dlock.ApplyOptions(opts...)
	if err := o.Validate(); err != nil {
		return nil, err
	}

	tok, err := token.New(o.OwnerID)
	if err != nil {
		return nil, fmt.Errorf("dlock/etcd: generate owner token: %w", err)
	}

	return &Lock{
		cli:              cli,
		lease:            clientv3.NewLease(cli),
		lockKey:          o.LockName,
		lockTimeout:      o.LockTimeout,
		operationTimeout: o.OperationTimeout,
		token:            tok,
	}, nil
}

// Lock acquires the distributed lock | Lock 获取分布式锁。
func (l *Lock) Lock(ctx context.Context) error {
	ctx, cancel := opctx.WithTimeout(ctx, l.operationTimeout)
	defer cancel()

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return fmt.Errorf("%w: lock %q", dlock.ErrClosed, l.lockKey)
	}

	// A second Lock on the same Locker is idempotent while its lease lives and
	// the key still carries this Locker's token.
	// 同一 Locker 在租约有效且 key 仍属本 Locker 时重复 Lock 是幂等的。
	if l.held {
		if _, err := l.lease.KeepAliveOnce(ctx, l.leaseID); err == nil {
			owned, err := l.ownsKey(ctx)
			if err != nil {
				return fmt.Errorf("dlock/etcd: verify lock %q: %w", l.lockKey, err)
			}
			if owned {
				return nil
			}
		} else if !errors.Is(err, rpctypes.ErrLeaseNotFound) {
			return fmt.Errorf("dlock/etcd: refresh lease for lock %q: %w", l.lockKey, err)
		}
		// The lease is gone or no longer protects the key: clean it up before
		// acquiring a new one.
		// 租约已消失或不再保护 key：获取新租约前先清理。
		l.discardLease(ctx, l.leaseID)
		l.held = false
		l.leaseID = 0
	}

	// etcd lease TTL is an integer number of seconds; round up so a sub-second
	// timeout does not become a zero TTL.
	// etcd 租约 TTL 为整数秒；向上取整，避免亚秒超时变成 0。
	ttl := max(1, int64(math.Ceil(l.lockTimeout.Seconds())))

	grant, err := l.lease.Grant(ctx, ttl)
	if err != nil {
		return fmt.Errorf("dlock/etcd: grant lease for lock %q: %w", l.lockKey, err)
	}

	resp, err := l.cli.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(l.lockKey), "=", 0)).
		Then(clientv3.OpPut(l.lockKey, l.token, clientv3.WithLease(grant.ID))).
		Commit()
	if err != nil {
		l.discardLease(ctx, grant.ID)
		return fmt.Errorf("dlock/etcd: acquire lock %q: %w", l.lockKey, err)
	}
	if !resp.Succeeded {
		l.discardLease(ctx, grant.ID)
		if holder := l.holder(ctx); holder != "" {
			return fmt.Errorf("%w: lock %q held by %q", dlock.ErrLocked, l.lockKey, holder)
		}
		return fmt.Errorf("%w: lock %q", dlock.ErrLocked, l.lockKey)
	}

	l.leaseID = grant.ID
	l.held = true
	return nil
}

// Unlock releases the distributed lock | Unlock 释放分布式锁。
func (l *Lock) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return fmt.Errorf("%w: lock %q", dlock.ErrClosed, l.lockKey)
	}
	if !l.held {
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockKey)
	}

	// Release is cleanup: a canceled caller context must not leave the lease
	// behind.
	// 释放属于清理：调用方 ctx 取消不应让租约留下。
	cleanupCtx, cancel := cleanup.Context(ctx)
	defer cancel()

	_, err := l.lease.Revoke(cleanupCtx, l.leaseID)
	if err != nil {
		if errors.Is(err, rpctypes.ErrLeaseNotFound) {
			l.held = false
			l.leaseID = 0
			return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockKey)
		}
		// Keep the lease state so a transient failure can be retried.
		// 暂时性错误保留租约状态，允许调用方重试。
		return fmt.Errorf("dlock/etcd: release lock %q: %w", l.lockKey, err)
	}

	l.held = false
	l.leaseID = 0
	return nil
}

// Refresh refreshes the lease for the distributed lock | Refresh 为分布式锁的租约续期。
func (l *Lock) Refresh(ctx context.Context) error {
	ctx, cancel := opctx.WithTimeout(ctx, l.operationTimeout)
	defer cancel()

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return fmt.Errorf("%w: lock %q", dlock.ErrClosed, l.lockKey)
	}
	if !l.held {
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockKey)
	}

	_, err := l.lease.KeepAliveOnce(ctx, l.leaseID)
	if err != nil {
		if errors.Is(err, rpctypes.ErrLeaseNotFound) {
			l.held = false
			l.leaseID = 0
			return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockKey)
		}
		return fmt.Errorf("dlock/etcd: refresh lock %q: %w", l.lockKey, err)
	}

	// A live lease does not prove the key still exists or still belongs to
	// this Locker: an old binary or an operator can delete it.
	// 租约存活不代表 key 仍存在且属于本 Locker：旧版本或运维操作可能删掉它。
	owned, err := l.ownsKey(ctx)
	if err != nil {
		return fmt.Errorf("dlock/etcd: verify lock %q: %w", l.lockKey, err)
	}
	if !owned {
		// The lease may still be alive without a key; revoke it instead of
		// leaving it until its TTL.
		// 租约可能还活着但没有 key：撤销它，而不是留到 TTL。
		l.discardLease(ctx, l.leaseID)
		l.held = false
		l.leaseID = 0
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockKey)
	}
	return nil
}

// Close releases the lease client created by NewLock. It is safe to call more
// than once. Close does not revoke a held lease: call Unlock first, or let the
// lease expire.
// Close 释放 NewLock 创建的 lease client，可重复调用。Close 不会撤销仍持有的
// 租约：先调用 Unlock，或等待租约自然过期。
func (l *Lock) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	l.mu.Unlock()

	return l.lease.Close()
}

// ownsKey reports whether the lock key still stores this Locker's token.
// ownsKey 判断锁 key 是否仍保存本 Locker 的令牌。
func (l *Lock) ownsKey(ctx context.Context) (bool, error) {
	resp, err := l.cli.Get(ctx, l.lockKey)
	if err != nil {
		return false, err
	}
	return len(resp.Kvs) == 1 && string(resp.Kvs[0].Value) == l.token, nil
}

// discardLease revokes a lease that was granted but never used, without
// letting a cancelled caller context prevent the cleanup.
// discardLease 撤销已申请但未使用的租约；调用方 ctx 已取消也要完成清理。
func (l *Lock) discardLease(ctx context.Context, id clientv3.LeaseID) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	_, _ = l.lease.Revoke(ctx, id)
}

// holder reports the diagnostic owner of the current lock, best effort.
// holder 尽力报告当前锁的持有者，仅用于错误信息。
func (l *Lock) holder(ctx context.Context) string {
	resp, err := l.cli.Get(ctx, l.lockKey)
	if err != nil || len(resp.Kvs) != 1 {
		return ""
	}
	return token.Owner(string(resp.Kvs[0].Value))
}
