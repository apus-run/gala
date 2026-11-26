// Package consul provides a distributed lock built on a Consul session and
// KV entry | consul 提供基于 Consul session 与 KV 条目的分布式锁.
//
// # Notes | 说明
//
// Lock creates a session whose TTL equals LockTimeout (behavior: release) and
// claims the key with KV.Acquire, which the server rejects while another
// session holds it, so Lock fails with [dlock.ErrLocked] instead of stealing
// the lock. Unlock releases the key and destroys the session; Refresh renews
// that same session and then confirms the key is still bound to it. Both only
// ever touch this Locker's session, so an old holder cannot release or renew
// the next holder's lock. When the holder dies the session expires, Consul
// releases the key after its lock-delay, and a nil entry from Renew (Consul's
// 404 answer) is reported as [dlock.ErrLockNotHeld]. Consul requires a session
// TTL of at least 10s. No background renewal is started: long-running work
// must call Refresh before the session expires. Unlock ignores caller
// cancellation, so a canceled request or job still releases the key.
// Lock 创建 TTL 等于 LockTimeout 的 session（behavior: release），并用
// KV.Acquire 认领 key：他人持有时 Consul 会拒绝，因此返回 [dlock.ErrLocked]
// 而不是抢占。Unlock 释放 key 并销毁 session；Refresh 续期同一个 session 并
// 确认 key 仍绑定在它上面。两者只操作本 Locker 的 session，旧持有者无法释放或
// 续期新持有者的锁。持有者死亡后 session 过期，Consul 在 lock-delay 之后释放
// key；Renew 返回 nil entry（Consul 对 404 的回答）时按 [dlock.ErrLockNotHeld]
// 处理。Consul 要求 session TTL 至少 10s。不会启动后台续期：长任务必须在
// session 过期前调用 Refresh。Unlock 忽略调用方取消，请求/任务取消后仍会释放 key。
//
// # Example | 示例
//
//	locker, err := consul.NewLock(ctx, apiClient, dlock.WithLockName("export"))
//	if err != nil {
//		return err
//	}
//	if err := locker.Lock(ctx); err != nil { /* held elsewhere */ }
//	defer locker.Unlock(ctx)
package consul

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/apus-run/gala/components/dlock"
	"github.com/apus-run/gala/components/dlock/internal/cleanup"
	"github.com/apus-run/gala/components/dlock/internal/opctx"
	"github.com/apus-run/gala/components/dlock/internal/token"
)

// Consul session TTL limits | Consul session TTL 限制。
const (
	minSessionTTL = 10 * time.Second
	maxSessionTTL = 24 * time.Hour
)

// Lock is a structure that implements distributed locking using Consul | Lock 是基于 Consul 实现分布式锁的结构体。
type Lock struct {
	client           *api.Client
	lockKey          string
	lockTimeout      time.Duration
	operationTimeout time.Duration
	token            string

	mu        sync.Mutex
	sessionID string
	held      bool
}

// Ensure Lock implements the Locker interface | 确保 Lock 实现 Locker 接口。
var _ dlock.Locker = (*Lock)(nil)

// NewLock creates a new Lock instance | NewLock 创建新的 Lock 实例。
func NewLock(_ context.Context, client *api.Client, opts ...dlock.Option) (*Lock, error) {
	if client == nil {
		return nil, fmt.Errorf("dlock/consul: nil consul client")
	}

	o := dlock.ApplyOptions(opts...)
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if o.LockTimeout < minSessionTTL {
		return nil, fmt.Errorf("dlock/consul: lock timeout %s is below the Consul minimum of %s", o.LockTimeout, minSessionTTL)
	}
	if o.LockTimeout > maxSessionTTL {
		return nil, fmt.Errorf("dlock/consul: lock timeout %s is above the Consul maximum of %s", o.LockTimeout, maxSessionTTL)
	}

	tok, err := token.New(o.OwnerID)
	if err != nil {
		return nil, fmt.Errorf("dlock/consul: generate owner token: %w", err)
	}

	return &Lock{
		client:           client,
		lockKey:          o.LockName,
		lockTimeout:      o.LockTimeout,
		operationTimeout: o.OperationTimeout,
		token:            tok,
	}, nil
}

// Lock attempts to acquire the distributed lock | Lock 尝试获取分布式锁。
func (l *Lock) Lock(ctx context.Context) error {
	ctx, cancel := opctx.WithTimeout(ctx, l.operationTimeout)
	defer cancel()

	l.mu.Lock()
	defer l.mu.Unlock()

	// A second Lock on the same Locker is idempotent while its session lives
	// and the key is still bound to it.
	// 同一 Locker 在 session 有效且 key 仍绑定时重复 Lock 是幂等的。
	if l.held {
		entry, _, renewErr := l.client.Session().Renew(l.sessionID, writeOpts(ctx))
		owned, ownErr := l.ownsLock(ctx)
		if ownErr != nil {
			return fmt.Errorf("dlock/consul: verify lock %q: %w", l.lockKey, ownErr)
		}
		switch {
		case renewErr == nil && entry != nil && owned:
			return nil
		case renewErr != nil && owned:
			// The session still owns the key; report the transient failure
			// instead of dropping a healthy lock.
			// session 仍持有 key：报告暂时性错误，不要丢弃健康的锁。
			return fmt.Errorf("dlock/consul: refresh lock %q: %w", l.lockKey, renewErr)
		}
		// Session is gone, the key moved, or Consul answered 404 with a nil
		// entry: clean up the old session and re-acquire.
		// session 消失、key 易主，或 Consul 对 404 返回 nil entry：清理旧 session
		// 后继续走重新获取。
		l.destroySession(l.sessionID)
		l.held = false
		l.sessionID = ""
	}

	session := &api.SessionEntry{
		TTL:      l.lockTimeout.String(),
		Behavior: api.SessionBehaviorRelease,
	}
	sessionID, _, err := l.client.Session().Create(session, writeOpts(ctx))
	if err != nil {
		return fmt.Errorf("dlock/consul: create session for lock %q: %w", l.lockKey, err)
	}

	acquired, _, err := l.client.KV().Acquire(&api.KVPair{
		Key:     l.lockKey,
		Value:   []byte(l.token),
		Session: sessionID,
	}, writeOpts(ctx))
	if err != nil {
		l.destroySession(sessionID)
		return fmt.Errorf("dlock/consul: acquire lock %q: %w", l.lockKey, err)
	}
	if !acquired {
		l.destroySession(sessionID)
		if holder := l.holder(ctx); holder != "" {
			return fmt.Errorf("%w: lock %q held by %q", dlock.ErrLocked, l.lockKey, holder)
		}
		return fmt.Errorf("%w: lock %q", dlock.ErrLocked, l.lockKey)
	}

	l.sessionID = sessionID
	l.held = true
	return nil
}

// Unlock releases the distributed lock | Unlock 释放分布式锁。
func (l *Lock) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.held {
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockKey)
	}

	// Release is cleanup: a canceled caller context must not leave the key
	// bound to a session that will not be renewed.
	// 释放属于清理：调用方 ctx 取消不应让 key 继续绑定在一个不再续期的 session 上。
	cleanupCtx, cancel := cleanup.Context(ctx)
	defer cancel()

	released, _, err := l.client.KV().Release(&api.KVPair{
		Key:     l.lockKey,
		Session: l.sessionID,
	}, writeOpts(cleanupCtx))
	if err != nil {
		if owned, ownErr := l.ownsLock(cleanupCtx); ownErr == nil && !owned {
			// The key is no longer ours. Destroy our still-live session too,
			// otherwise it lingers until the session TTL with nothing able to
			// clean it up.
			// key 已不属于我们。仍然要销毁还活着的 session，否则它会留到 session
			// TTL 到期，且之后没有任何途径清理。
			l.destroySession(l.sessionID)
			l.held = false
			l.sessionID = ""
			return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockKey)
		}
		return fmt.Errorf("dlock/consul: release lock %q: %w", l.lockKey, err)
	}

	// The session is destroyed even when the entry was already gone, so no
	// orphan session keeps renewing. A destroy failure only leaves an empty
	// session that expires after its TTL; the key is already released, so it
	// must not turn a successful unlock into an error.
	// 即使 KV 条目已不存在也销毁 session，避免孤儿 session 继续续期。销毁失败
	// 只会留下一个空 session 等待 TTL 到期；key 已经释放，不应把成功的解锁
	// 变成错误。
	_, _ = l.client.Session().Destroy(l.sessionID, writeOpts(cleanupCtx))

	l.held = false
	l.sessionID = ""
	if !released {
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockKey)
	}
	return nil
}

// Refresh refreshes the lock's expiration time | Refresh 刷新锁的过期时间。
func (l *Lock) Refresh(ctx context.Context) error {
	ctx, cancel := opctx.WithTimeout(ctx, l.operationTimeout)
	defer cancel()

	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.held {
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockKey)
	}

	entry, _, renewErr := l.client.Session().Renew(l.sessionID, writeOpts(ctx))
	if renewErr != nil {
		owned, ownErr := l.ownsLock(ctx)
		if ownErr == nil && !owned {
			l.lose()
			return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockKey)
		}
		return fmt.Errorf("dlock/consul: refresh lock %q: %w", l.lockKey, renewErr)
	}
	if entry == nil {
		// Consul answers 404 with a nil entry and a nil error once the session
		// is gone, so a nil entry is a lost lock, not a successful renewal.
		// session 消失后 Consul 返回 404：entry 为 nil 且 err 为 nil，这是丢锁
		// 而不是续期成功。
		l.lose()
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockKey)
	}

	// The session outlived the key (deleted out of band), so ownership is gone
	// even though renewal succeeded.
	// session 活得比 key 久（key 被带外删除）：即使续期成功也已失去所有权。
	owned, ownErr := l.ownsLock(ctx)
	if ownErr != nil {
		return fmt.Errorf("dlock/consul: verify lock %q: %w", l.lockKey, ownErr)
	}
	if !owned {
		l.lose()
		return fmt.Errorf("%w: lock %q", dlock.ErrLockNotHeld, l.lockKey)
	}
	return nil
}

// lose clears the local state and destroys a session that no longer protects
// the key, so a lost lock does not strand a Consul session for its full TTL.
// lose 清理本地状态并销毁已不再保护 key 的 session，避免丢锁后把 session 留到 TTL。
func (l *Lock) lose() {
	if l.sessionID != "" {
		l.destroySession(l.sessionID)
	}
	l.held = false
	l.sessionID = ""
}

// destroySession cleans up a session that failed to acquire the key or no
// longer protects it. It always finishes even when the caller context is
// cancelled, bounded by a 5s timeout.
// destroySession 清理抢占失败或已不再保护 key 的 session；调用方 ctx 取消也要
// 完成，最多等待 5s。
func (l *Lock) destroySession(sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = l.client.Session().Destroy(sessionID, writeOpts(ctx))
}

// ownsLock reports whether the KV entry is still bound to this Locker's
// session. A read error is returned so callers can report an uncertain state
// instead of silently claiming ownership.
// ownsLock 判断 KV 条目是否仍绑定本 Locker 的 session。读取失败会返回错误，
// 让调用方报告"状态未知"，而不是默默声称仍持有锁。
func (l *Lock) ownsLock(ctx context.Context) (bool, error) {
	pair, _, err := l.client.KV().Get(l.lockKey, readOpts(ctx))
	if err != nil {
		return false, err
	}
	return pair != nil && pair.Session == l.sessionID, nil
}

// holder reports the diagnostic owner of the current lock, best effort.
// holder 尽力报告当前锁的持有者，仅用于错误信息。
func (l *Lock) holder(ctx context.Context) string {
	pair, _, err := l.client.KV().Get(l.lockKey, readOpts(ctx))
	if err != nil || pair == nil {
		return ""
	}
	return token.Owner(string(pair.Value))
}

// writeOpts carries the caller's cancellation into Consul API calls.
// writeOpts 把调用方的取消信号传递给 Consul API 请求。
func writeOpts(ctx context.Context) *api.WriteOptions {
	return (&api.WriteOptions{}).WithContext(ctx)
}

// readOpts carries the caller's cancellation into Consul API reads. Consul's
// default HTTP client has no timeout, so a contextless read can hang.
// readOpts 把调用方的取消信号传递给 Consul API 读请求。Consul 默认 HTTP 客户端
// 没有超时，不传上下文可能永久挂起。
func readOpts(ctx context.Context) *api.QueryOptions {
	return (&api.QueryOptions{}).WithContext(ctx)
}
