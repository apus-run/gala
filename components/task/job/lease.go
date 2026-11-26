// Lease support: an optional single-run guard built on taskcontract.Locker.
// 租约支持：基于 taskcontract.Locker 的可选单次执行保护。

package job

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	taskcontract "github.com/apus-run/gala/components/task"
)

const (
	// defaultLockRefreshInterval is how often a held lease is refreshed.
	// defaultLockRefreshInterval 是持有租约时的默认续期间隔。
	defaultLockRefreshInterval = 5 * time.Second

	// defaultLockAcquireTimeout bounds the provider call, Lock, each Refresh
	// and Unlock.
	// defaultLockAcquireTimeout 限制 provider 调用、Lock、每次 Refresh 与 Unlock。
	defaultLockAcquireTimeout = 5 * time.Second
)

// lockConfig holds the lease timings configured through [WithLockerProvider].
// lockConfig 保存通过 [WithLockerProvider] 配置的租约时序。
type lockConfig struct {
	refreshInterval time.Duration
	acquireTimeout  time.Duration
}

// LockOption configures the lease timings of [WithLockerProvider].
// LockOption 配置 [WithLockerProvider] 的租约时序。
type LockOption func(*lockConfig)

// LockerProvider resolves the lease of one Run.
//
// It is called for every Run, from the goroutine that is about to execute the
// job, with a context bounded by the acquire timeout. The returned Locker must
// carry its own ownership token, so return a fresh Locker per call (dlock's
// Client.NewLock does). Returning one cached Locker for concurrent Runs makes
// Lock an idempotent extend for the same owner: every Run would then "hold"
// the lease and the first Unlock would release it for the still-running
// others. The provider also decides the lock key, usually one key per job name.
//
// Returning (nil, nil) runs that Run without a lease (local-only). Returning
// an error fails that Run without executing the job; nothing is cached, so a
// later Run calls the provider again.
// LockerProvider 解析一次 Run 使用的租约。
//
// 每次 Run 都会调用它（由即将执行任务的 goroutine 调用），ctx 受获取超时限制。
// 返回的 Locker 必须携带自己的所有权令牌，因此每次调用都应返回新的 Locker
// （dlock 的 Client.NewLock 即如此）。若并发 Run 复用同一个缓存的 Locker，Lock
// 对同一持有者会变成幂等续期：所有 Run 都会"持有"租约，而第一个 Unlock 会替仍在
// 运行的其它 Run 释放租约。锁键也由 provider 决定，通常每个任务名一个键。
//
// 返回 (nil, nil) 表示本次 Run 不加租约（纯本地执行）。返回错误则本次 Run 失败、
// 任务不执行；错误不会被缓存，之后的 Run 会再次调用 provider。
type LockerProvider func(ctx context.Context, jobName string) (taskcontract.Locker, error)

// WithLockerProvider enables the single-run lease: every Run asks the provider
// for a Locker, acquires it before invoking the job and releases it afterwards,
// so across replicas only the holder runs the job.
//
// Calling it with a nil provider is a no-op (the current configuration is
// kept). The lease interface comes from github.com/apus-run/gala/components/task, so any
// dlock backend can be adapted without this module depending on
// components/dlock. The option is meant for NewRegistry; it performs no I/O.
// WithLockerProvider 启用单次执行租约：每次 Run 向 provider 索取 Locker，调用任务
// 前获取、结束后释放，因此多副本中只有持有者会运行任务。
//
// provider 为 nil 时是空操作（保持当前配置）。租约接口来自
// github.com/apus-run/gala/components/task，任何 dlock 后端都可适配，而本模块不依赖
// components/dlock。该选项用于 NewRegistry，不做任何 I/O。
func WithLockerProvider(p LockerProvider, opts ...LockOption) Option {
	return func(c *config) {
		if p == nil {
			return
		}
		c.lockProvider = p
		for _, opt := range opts {
			if opt != nil {
				opt(&c.lock)
			}
		}
	}
}

// WithLockRefreshInterval sets how often a running job's lease is refreshed
// (default 5s). A non-positive duration keeps the default.
//
// Keep it well below the backend's lock TTL: a lease that expires between two
// refreshes lets another replica take over.
// WithLockRefreshInterval 设置运行中任务租约的续期间隔（默认 5s）。非正数保留
// 默认值。
//
// 应明显小于后端的锁 TTL：租约在两次续期间隔内过期会让其他副本接管。
func WithLockRefreshInterval(d time.Duration) LockOption {
	return func(c *lockConfig) {
		if d > 0 {
			c.refreshInterval = d
		}
	}
}

// WithLockAcquireTimeout sets the timeout for the provider call and for
// acquiring the lease (default 5s). The same bound is used for each refresh
// and for the final release, so a hung backend cannot block Run forever. A
// non-positive duration keeps the default.
// WithLockAcquireTimeout 设置 provider 调用与获取租约的超时（默认 5s）。每次续期
// 与最后的释放也使用同一上限，因此卡住的后端不会永久阻塞 Run。非正数保留默认值。
func WithLockAcquireTimeout(d time.Duration) LockOption {
	return func(c *lockConfig) {
		if d > 0 {
			c.acquireTimeout = d
		}
	}
}

// runWithLease executes fn under a lease: resolve the Locker, acquire, refresh
// while running, then unlock and join all errors.
// runWithLease 在租约下执行 fn：解析 Locker、获取、运行期间续期、最后解锁并 join
// 所有错误。
func (r *Registry) runWithLease(ctx context.Context, inv Invocation, fn Func, provider LockerProvider, lc lockConfig) error {
	// The provider and Lock each get their own bounded context, so a slow
	// provider cannot consume the acquisition budget as well.
	// provider 与 Lock 各自使用独立的限时 context，慢 provider 不会占用获取租约的
	// 时间预算。
	resolveCtx, cancelResolve := context.WithTimeout(ctx, lc.acquireTimeout)
	locker, err := provider(resolveCtx, inv.Name)
	cancelResolve()
	if err != nil {
		return fmt.Errorf("job: resolve lease for %q: %w", inv.Name, err)
	}
	if locker == nil {
		// (nil, nil) marks this run local-only: no lease, no refresh.
		// (nil, nil) 表示本次运行纯本地：不获取、不续期。
		return callFunc(ctx, inv, fn)
	}

	acquireCtx, cancelAcquire := context.WithTimeout(ctx, lc.acquireTimeout)
	lockErr := locker.Lock(acquireCtx)
	cancelAcquire()
	if lockErr != nil {
		return fmt.Errorf("job: acquire lease for %q: %w", inv.Name, lockErr)
	}

	// The job sees a derived context so a failed refresh can stop it, while the
	// caller's cancellation still propagates.
	// 任务看到派生的 context：续期失败可以停止它，同时调用方的取消仍会传播。
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	// Refreshing and unlocking outlive a canceled run ctx on purpose: the lease
	// must be released even when the caller disconnects. refreshCtx additionally
	// lets Run stop the refresher and abort a refresh still in flight.
	// 续期与解锁刻意不随运行 ctx 取消：即使调用方断开，也必须释放租约。
	// refreshCtx 还让 Run 能停止续期 goroutine，并中止仍在途的续期调用。
	base := context.WithoutCancel(ctx)
	refreshCtx, stopRefresh := context.WithCancel(base)

	var (
		refreshErr error
		refresher  sync.WaitGroup
	)
	refresher.Add(1)
	go func() {
		defer refresher.Done()

		ticker := time.NewTicker(lc.refreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-refreshCtx.Done():
				return
			case <-ticker.C:
				// Derive the refresh from refreshCtx, not base, so that stopping
				// the refresher also aborts a call still in flight. Derived from
				// base, a slow Refresh would keep Run (via refresher.Wait) blocked
				// for up to the acquire timeout after the job has already returned.
				// 续期派生自 refreshCtx 而非 base，因此停止续期时也能中止仍在途的
				// 调用。若派生自 base，慢速 Refresh 会让 Run（经 refresher.Wait）在
				// 任务已返回后最多阻塞一个获取超时。
				rctx, cancel := context.WithTimeout(refreshCtx, lc.acquireTimeout)
				err := locker.Refresh(rctx)
				cancel()
				if err != nil {
					// A canceled refreshCtx means Run is stopping: the abort is
					// intentional, so it is neither recorded nor treated as a lease
					// loss (which would cancel the job context pointlessly).
					// refreshCtx 已取消说明 Run 正在收尾：这是有意中止，既不记录为
					// 错误，也不当作失去租约（那会无意义地取消任务 ctx）。
					if refreshCtx.Err() != nil {
						return
					}
					// Record the failure and stop the job; the main goroutine
					// reads refreshErr only after refresher.Wait, so this write
					// is synchronized by the WaitGroup.
					// 记录失败并停止任务；主 goroutine 只在 refresher.Wait
					// 之后读取 refreshErr，由 WaitGroup 保证同步。
					refreshErr = err
					cancelRun()
					return
				}
			}
		}
	}()

	jobErr := callFunc(runCtx, inv, fn)

	// Stop refreshing before unlocking: a refresh racing with the release could
	// otherwise renew a lease this Run no longer owns.
	// 先停止续期再解锁：否则与释放竞争的续期可能续订已不属于本次 Run 的租约。
	stopRefresh()
	refresher.Wait()

	unlockCtx, cancelUnlock := context.WithTimeout(base, lc.acquireTimeout)
	unlockErr := locker.Unlock(unlockCtx)
	cancelUnlock()

	return errors.Join(jobErr, refreshErr, unlockErr)
}
