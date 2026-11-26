// This file implements the optional per-job leader lease used by Cron.
// See WithLockerProvider and the package documentation for the semantics.
// 本文件实现 Cron 可选的任务级 leader 租约，语义见 WithLockerProvider 与
// 包文档。

package cron

import (
	"context"
	"fmt"
	"sync"
	"time"

	taskcontract "github.com/apus-run/gala/task"
)

const (
	// defaultLockRefreshInterval is how often a leader refreshes its lease
	// when WithLockRefreshInterval is not given.
	// defaultLockRefreshInterval 是未指定 WithLockRefreshInterval 时 leader
	// 续期的默认间隔。
	defaultLockRefreshInterval = 5 * time.Second

	// defaultLockAcquireTimeout bounds every provider/Lock/Refresh/Unlock call
	// when WithLockAcquireTimeout is not given.
	// defaultLockAcquireTimeout 是未指定 WithLockAcquireTimeout 时每次
	// provider/Lock/Refresh/Unlock 调用的默认超时。
	defaultLockAcquireTimeout = 5 * time.Second
)

// LockerProvider resolves the distributed lease to use for a job.
//
// It is called lazily on the first due fire of a job, from the goroutine
// that is about to run it, with a context bounded by the acquire timeout.
// Returning (nil, nil) marks the job local-only: every replica runs it and
// the provider is never called again. An error is not cached: the fire is
// skipped, LockProviderFailed is emitted, and the next due fire retries.
// The returned Locker must belong to this Cron instance (that is, every
// replica needs its own ownership token), because Lock/Refresh/Unlock are
// invoked concurrently by different replicas.
// LockerProvider 解析任务使用的分布式租约。
//
// 它在任务首次到期时惰性调用（由即将运行任务的 goroutine 调用），ctx 受
// acquire 超时限制。返回 (nil, nil) 表示任务仅本地运行：每个副本都会执行它，
// 且不再调用 provider。返回错误不会被缓存：本次触发被跳过并上报
// LockProviderFailed，下次到期时重试。返回的 Locker 必须属于当前 Cron
// 实例（即每个副本都需要自己的所有权令牌），因为不同副本会并发调用
// Lock/Refresh/Unlock。
type LockerProvider func(ctx context.Context, jobID string) (taskcontract.Locker, error)

// LockEvent identifies a distributed-lease event reported to LockEventHandler.
// LockEvent 表示上报给 LockEventHandler 的租约事件。
type LockEvent int

const (
	// LockAcquireFailed means Lock returned an error, so this fire was
	// skipped (the job did not run).
	// LockAcquireFailed 表示 Lock 返回错误，本次触发被跳过（任务没有运行）。
	LockAcquireFailed LockEvent = iota

	// LockProviderFailed means the LockerProvider returned an error, so this
	// fire was skipped. The provider is retried on the next due fire.
	// LockProviderFailed 表示 LockerProvider 返回错误，本次触发被跳过；
	// 下次到期时会再次调用 provider。
	LockProviderFailed

	// LockLost means Refresh failed while this replica was the leader. The
	// replica is demoted and the refresher stops, but the lease is NOT
	// released on purpose: a transient error may have left it healthy, and
	// releasing it could let another replica take over while this one still
	// runs. The next due fire may Lock again with the same Locker.
	// LockLost 表示本副本作为 leader 时 Refresh 失败。副本被降级、续期器停止，
	// 但租约有意不释放：瞬时错误可能并未损坏租约，贸然释放可能让其他副本在
	// 本副本仍在运行时接管。下次到期触发可以用同一个 Locker 重新 Lock。
	LockLost

	// LockReleaseFailed means Unlock returned an error. This includes a
	// "not held" error: this package deliberately does not import dlock, so
	// it cannot classify lease errors and reports every failure.
	// LockReleaseFailed 表示 Unlock 返回错误。这包括"已不持有"错误：本包有意
	// 不导入 dlock，无法区分租约错误，因此所有失败都会上报。
	LockReleaseFailed
)

// String returns the event name | String 返回事件名称。
func (e LockEvent) String() string {
	switch e {
	case LockAcquireFailed:
		return "LockAcquireFailed"
	case LockProviderFailed:
		return "LockProviderFailed"
	case LockLost:
		return "LockLost"
	case LockReleaseFailed:
		return "LockReleaseFailed"
	default:
		return fmt.Sprintf("LockEvent(%d)", int(e))
	}
}

// LockEventHandler receives distributed-lease events.
//
// It is called synchronously by the goroutine that observed the event: a job
// goroutine, the lease refresher, the caller of Stop/Shutdown, or a background
// release started by Remove/RemoveAll. It may therefore run concurrently for
// different jobs and must be fast and concurrency-safe. Do not block in it, and
// do not call back into the same Cron (a handler invoked from the refresher
// would deadlock a Stop that waits for that refresher to exit).
// LockEventHandler 接收分布式租约事件。
//
// 它由观察到事件的 goroutine 同步调用：任务 goroutine、租约续期 goroutine、
// Stop/Shutdown 的调用方，或 Remove/RemoveAll 启动的后台释放。因此不同任务的事件
// 可能并发到达，处理器必须快速且并发安全。不要在其中阻塞，也不要回调同一个 Cron
// （续期 goroutine 里调用的处理器若去 Stop，会等待该续期器退出而死锁）。
type LockEventHandler func(jobID string, event LockEvent, err error)

// LockOption configures the distributed-lease behavior enabled by
// WithLockerProvider.
// LockOption 配置由 WithLockerProvider 启用的分布式租约行为。
type LockOption interface {
	apply(*lockConfig)
}

// LockOptionFunc adapts a plain function to LockOption.
// LockOptionFunc 把普通函数适配为 LockOption。
type LockOptionFunc func(*lockConfig)

func (f LockOptionFunc) apply(c *lockConfig) {
	if f != nil {
		f(c)
	}
}

// lockConfig is the resolved lease configuration stored on a Cron.
// lockConfig 是存储在 Cron 上的最终租约配置。
type lockConfig struct {
	refreshInterval time.Duration
	acquireTimeout  time.Duration
	onEvent         LockEventHandler
}

// WithLockRefreshInterval sets how often a leader refreshes its lease
// (default 5s; a non-positive duration keeps the current value).
//
// It must be comfortably shorter than the backend lease TTL, otherwise the
// lease can expire between refreshes and another replica may take over.
// WithLockRefreshInterval 设置 leader 续期租约的间隔（默认 5s；非正值保持
// 当前值）。
//
// 它必须明显短于后端租约 TTL，否则租约可能在两次续期之间过期，被其他副本接管。
func WithLockRefreshInterval(d time.Duration) LockOption {
	return LockOptionFunc(func(c *lockConfig) {
		if d > 0 {
			c.refreshInterval = d
		}
	})
}

// WithLockAcquireTimeout bounds every provider/Lock/Refresh/Unlock call
// (default 5s; a non-positive duration keeps the current value).
// WithLockAcquireTimeout 限制每次 provider/Lock/Refresh/Unlock 调用的时长
// （默认 5s；非正值保持当前值）。
func WithLockAcquireTimeout(d time.Duration) LockOption {
	return LockOptionFunc(func(c *lockConfig) {
		if d > 0 {
			c.acquireTimeout = d
		}
	})
}

// WithLockEventHandler installs the lease event handler (default: no-op).
// A nil handler keeps the current one.
// WithLockEventHandler 设置租约事件处理器（默认：不处理）。handler 为 nil
// 时保持当前值。
func WithLockEventHandler(h LockEventHandler) LockOption {
	return LockOptionFunc(func(c *lockConfig) {
		if h != nil {
			c.onEvent = h
		}
	})
}

// WithLockerProvider enables per-job distributed leases.
//
// The provider is stored on the Cron; no I/O happens here, nor in New, Add or
// Start. The first due fire of a job resolves its Locker. WithLockerProvider
// (nil) is a no-op, and a nil provider also keeps the current one.
//
// The option is meant for New; the lease options are applied in order.
// WithLockerProvider 启用任务级分布式租约。
//
// provider 仅保存在 Cron 上；本函数、New、Add、Start 都不做 I/O。任务首次
// 到期触发时才解析它的 Locker。WithLockerProvider(nil) 是空操作，nil
// provider 也会保持当前值。
//
// 该选项用于 New；各租约选项按传入顺序应用。
func WithLockerProvider(p LockerProvider, opts ...LockOption) Option {
	return OptionFunc(func(c *Cron) {
		if p == nil {
			return
		}

		cfg := lockConfig{
			refreshInterval: defaultLockRefreshInterval,
			acquireTimeout:  defaultLockAcquireTimeout,
		}
		for _, opt := range opts {
			if opt != nil {
				opt.apply(&cfg)
			}
		}

		c.lockProvider = p
		c.lockCfg = cfg
	})
}

// jobLease is the lease state of a single job id.
//
// It is keyed by job id, not attached to the job struct, so replacing a job
// with Add preserves leadership.
// jobLease 是单个 job id 的租约状态。
//
// 它以 job id 为键、不挂在 job 结构上，因此用 Add 替换任务不会丢失 leadership。
type jobLease struct {
	// opMu serializes acquire and release (including their I/O) for this job.
	// It is never held by the refresher, so release can cancel the refresher
	// and wait for it while holding opMu.
	// opMu 串行化该任务的获取与释放（包括其中的 I/O）。续期器从不持有它，
	// 因此 release 可以持着 opMu 取消续期器并等待其退出。
	opMu sync.Mutex

	// mu guards the fields below; it is never held during I/O.
	// mu 保护以下字段；I/O 期间不持有。
	mu sync.Mutex

	resolved  bool                // the provider returned a Locker | provider 已成功返回 Locker。
	localOnly bool                // provider returned (nil, nil) | provider 返回了 (nil, nil)。
	removed   bool                // Remove/RemoveAll dropped this state | Remove/RemoveAll 已丢弃该状态。
	locker    taskcontract.Locker // cached Locker | 缓存的 Locker。

	leader  bool // this replica owns the lease | 本副本持有租约。
	mayHold bool // the lease MAY still be held in the backend | 后端租约"可能"仍被持有。

	// leaderGen increments each time this replica wins a Lock and becomes
	// leader. A release snapshots it: if it changed by the time the release
	// actually runs, a newer leadership was acquired (eg. after a concurrent
	// Start), and the stale release must not cancel its refresher nor Unlock
	// its lease.
	// leaderGen 在每次本副本 Lock 成功并成为 leader 时递增。释放会快照它：若释放
	// 真正执行时它已变化，说明已有更新的 leadership（如在并发 Start 之后获取），
	// 旧的释放不得取消其续期器，也不得 Unlock 其租约。
	leaderGen uint64

	refreshCancel context.CancelFunc // stops the refresher | 停止续期器。
	refreshDone   chan struct{}      // closed when the refresher exits | 续期器退出时关闭。
}

// leaseFor returns the lease state a fire of j may use, creating it if needed.
//
// It reports false when the fire must be skipped: the Cron is stopped, j was
// removed or replaced after the fire was dispatched, or the state is a
// tombstone left by Remove/RemoveAll. Such a fire must not run, and it must
// not resurrect a lease state for a job id that was already forgotten:
// Remove has returned (and may have been followed by Add on another
// goroutine), so a second Lock with the same cached Locker could silently
// steal a lease from a newer state.
//
// A tombstone is settled when its release finishes: an id re-added meanwhile
// keeps the state (same opMu) and is reopened, a still-removed id forgets it
// and the next Add creates a fresh state. Either way the stale release has
// completed or is ordered by opMu before a new Lock, so the stale Unlock can
// never delete a lease a newer lifecycle just acquired.
// leaseFor 返回 j 的本次触发可使用的租约状态（必要时创建）。
//
// 以下情况返回 false（本次触发必须跳过）：Cron 已停止、j 在触发被分发之后被
// 移除或替换、状态是 Remove/RemoveAll 留下的墓碑。这类触发不能运行，也不能复活
// 已遗忘 job id 的租约状态：Remove 已经返回（之后可能在另一 goroutine 里被 Add），
// 用同一缓存 Locker 再次 Lock 会悄悄抢走新状态的租约。
//
// 释放结束时墓碑会被结算：期间被重新添加的 id 保留状态（同一个 opMu）并重新
// 开放；仍被移除的 id 则遗忘状态，下次 Add 会创建新状态。无论哪种情况，旧释放
// 都已完成或被 opMu 排在新 Lock 之前，因此旧 Unlock 绝不会删除新生命周期刚获取的
// 租约。
func (c *Cron) leaseFor(j *job, epoch uint64) (*jobLease, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stopped || c.epoch != epoch || c.jobs[j.id] != j {
		return nil, false
	}

	if c.leases == nil {
		c.leases = map[string]*jobLease{}
	}

	l := c.leases[j.id]
	if l == nil {
		l = &jobLease{}
		c.leases[j.id] = l
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.removed {
		return l, false
	}

	return l, true
}

// finishRemoval settles a tombstoned lease after its release finished.
//
// It runs under the state's opMu, so the stale Unlock already happened (or was
// unnecessary) and no new acquire can interleave. A job re-added while the
// release was in flight keeps its state (with the same opMu) and is reopened
// for the next fire; a still-removed job forgets the state entirely.
//
// It never deletes a state whose lease may still be held: a failed Unlock keeps
// the state, its tombstone, and its cached Locker (with the replica's ownership
// token) so a later Stop/Shutdown release can retry instead of forgetting a
// lease that the backend may still consider ours.
// finishRemoval 在释放完成后处理墓碑租约。
//
// 它在状态的 opMu 下运行，因此旧的 Unlock 已经发生（或本就无需释放），不会有新的
// 获取插入。释放期间被重新添加的任务保留其状态（同一个 opMu）并重新开放给下次
// 触发；仍被移除的任务则彻底遗忘该状态。
//
// 它绝不会删除租约"可能仍被持有"的状态：Unlock 失败会保留状态、墓碑以及缓存的
// Locker（含本副本的所有权令牌），让之后的 Stop/Shutdown 释放可以重试，而不是
// 遗忘一个后端可能仍认为属于我们的租约。
func (c *Cron) finishRemoval(jobID string, l *jobLease) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.leases[jobID] != l {
		return
	}

	l.mu.Lock()
	mayHold := l.mayHold
	l.mu.Unlock()
	if mayHold {
		return
	}

	if c.jobs[jobID] != nil {
		l.mu.Lock()
		l.removed = false
		l.mu.Unlock()
		return
	}
	delete(c.leases, jobID)
}

// emitLockEvent invokes the configured handler (if any).
// emitLockEvent 调用已配置的事件处理器（若有）。
func (c *Cron) emitLockEvent(jobID string, event LockEvent, err error) {
	if c.lockCfg.onEvent != nil {
		c.lockCfg.onEvent(jobID, event, err)
	}
}

// ensureLeader reports whether the current fire of j may run.
//
// It is called from the job goroutine, never from runDue/dispatch, so lease
// I/O never blocks scheduling. Returning true means the job may run: this
// replica is the leader, the job is local-only, or no lease is in use.
// A false result means the fire must be skipped; lease conflicts are reported
// to the event handler, while a job removed/replaced or a Cron stopped since
// dispatch is dropped silently (it is not a lease failure).
//
// The Cron mutex is never held during provider/Lock I/O.
// ensureLeader 报告 j 的本次触发是否可以运行。
//
// 它由任务 goroutine 调用，绝不在 runDue/dispatch 中调用，因此租约 I/O 不会
// 阻塞调度。返回 true 表示任务可以运行：本副本是 leader、任务是纯本地的，
// 或未启用租约。返回 false 表示必须跳过本次触发；租约竞争会上报事件处理器，
// 而分发后任务被移除/替换或 Cron 已停止则静默丢弃（不算租约失败）。
//
// provider/Lock I/O 期间绝不持有 Cron 锁。
func (c *Cron) ensureLeader(j *job, epoch uint64) bool {
	jobID := j.id

	l, ok := c.leaseFor(j, epoch)
	if !ok {
		return false
	}

	// Fast path: a leader (or a local-only job) runs without any lock I/O.
	// 快速路径：leader（或纯本地任务）无需任何锁 I/O。
	l.mu.Lock()
	if l.localOnly || l.leader {
		l.mu.Unlock()
		return true
	}
	l.mu.Unlock()

	l.opMu.Lock()

	var (
		event LockEvent
		evErr error
		emit  bool
	)
	defer func() {
		// Report after releasing opMu so a slow handler cannot block other
		// fires and a handler calling back into this lease cannot deadlock.
		// 先释放 opMu 再上报，慢处理器不会阻塞其它触发，回调本租约也不会死锁。
		l.opMu.Unlock()
		if emit {
			c.emitLockEvent(jobID, event, evErr)
		}
	}()

	l.mu.Lock()
	if l.removed {
		l.mu.Unlock()
		return false
	}
	if l.localOnly || l.leader {
		l.mu.Unlock()
		return true
	}
	locker, resolved := l.locker, l.resolved
	l.mu.Unlock()

	// Another goroutine may have dropped the job or stopped the Cron while
	// this fire waited for opMu (Remove/RemoveAll/Stop, or Add replacing it).
	// Do not acquire a lease for a job the Cron no longer coordinates.
	// 本次触发等待 opMu 期间，其它 goroutine 可能已丢弃该任务或停止了 Cron
	// （Remove/RemoveAll/Stop，或被 Add 替换）。不要为 Cron 已不再协调的任务
	// 获取租约。
	c.mu.RLock()
	live := !c.stopped && c.epoch == epoch && c.jobs[jobID] == j
	c.mu.RUnlock()
	if !live {
		return false
	}

	if !resolved {
		ctx, cancel := context.WithTimeout(context.Background(), c.lockCfg.acquireTimeout)
		got, err := c.lockProvider(ctx, jobID)
		cancel()
		if err != nil {
			// Provider errors are not cached: the next due fire retries.
			// provider 错误不缓存：下次到期触发会重试。
			event, evErr, emit = LockProviderFailed, err, true
			return false
		}

		if got == nil {
			// Install the local-only verdict under the Cron read lock, like
			// leadership below: Remove/RemoveAll and Stop set removed, stopped
			// and epoch under its write lock, so the fence cannot land between
			// the check and the installation.
			// 与下面的 leadership 一样，在 Cron 读锁下记录"纯本地"结论：Remove/
			// RemoveAll 与 Stop 在其写锁下设置 removed、stopped 与 epoch，因此闸门
			// 不可能落在检查与安装之间。
			c.mu.RLock()
			fenced := c.stopped || c.epoch != epoch
			if !fenced {
				l.mu.Lock()
				fenced = l.removed
				if !fenced {
					l.localOnly = true
				}
				l.mu.Unlock()
			}
			c.mu.RUnlock()
			if fenced {
				return false
			}

			// The provider may have been slow enough for Add to replace the job:
			// mirror the Locked path's stillCurrent check so the replaced
			// version's fire does not run. The cached local-only verdict stays on
			// the id's state for the new version.
			// provider 可能慢到足以让 Add 替换任务：与 Locked 路径一样做
			// stillCurrent 检查，使被替换版本的触发不运行。缓存的"纯本地"结论
			// 保留在 id 的状态上，供新版本使用。
			c.mu.RLock()
			stillCurrent := c.jobs[jobID] == j
			c.mu.RUnlock()

			return stillCurrent
		}

		l.mu.Lock()
		l.locker = got
		l.resolved = true
		l.mu.Unlock()
		locker = got
	}

	// The provider may itself have been slow: if the gate closed while it was
	// resolving the Locker, do not Lock at all. The cached Locker stays in the
	// state for a fire of the new epoch.
	// provider 本身也可能很慢：若解析 Locker 期间闸门已关闭，就不要再 Lock。
	// 已缓存的 Locker 留给新 epoch 的触发使用。
	if c.gateFenced(l, epoch) {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.lockCfg.acquireTimeout)
	err := locker.Lock(ctx)
	cancel()
	if err != nil {
		event, evErr, emit = LockAcquireFailed, err, true
		return false
	}

	// The gate may have been fenced while the provider or Lock was in flight:
	// Remove/RemoveAll tombstoned this state, or Stop fenced the Cron. The
	// lease we just acquired must not be kept: release it here and skip the
	// run. Any release already queued behind opMu then finds mayHold false.
	//
	// Re-check the fence and install leadership in one critical section under
	// the Cron read lock: Remove/RemoveAll and Stop take its write lock and set
	// removed, stopped and epoch under it, so no fence can land between the
	// check and the installation. Otherwise the queued release snapshots the
	// older leader generation, skips the newer leadership, and a Remove/Stop
	// leaves a lease with its refresher alive for a tombstoned or stopped
	// state.
	// 在 provider 或 Lock 进行期间闸门可能已被隔离：Remove/RemoveAll 写了墓碑，
	// 或 Stop 隔离了 Cron。刚获取的租约不能保留：在这里释放并跳过运行。排在 opMu
	// 之后的释放随后会看到 mayHold 为 false。
	//
	// 复查闸门与安装 leadership 在 Cron 读锁下的同一个临界区完成：
	// Remove/RemoveAll 与 Stop 需要其写锁并在锁内设置 removed、stopped 与 epoch，
	// 因此闸门不可能落在检查与安装之间。否则排队中的释放会快照旧的 leader 代数、
	// 跳过新的 leadership，使 Remove/Stop 给墓碑或已停止状态留下活着的租约与续期器。
	done := make(chan struct{})
	rctx, rcancel := context.WithCancel(context.Background())

	c.mu.RLock()
	fenced := c.stopped || c.epoch != epoch
	if !fenced {
		l.mu.Lock()
		fenced = l.removed
		if !fenced {
			l.leader = true
			l.leaderGen++
			l.mayHold = true
			l.refreshCancel = rcancel
			l.refreshDone = done
		}
		l.mu.Unlock()
	}
	c.mu.RUnlock()

	if fenced {
		rcancel()
		uctx, ucancel := context.WithTimeout(context.Background(), c.lockCfg.acquireTimeout)
		uerr := locker.Unlock(uctx)
		ucancel()
		if uerr != nil {
			l.mu.Lock()
			l.mayHold = true
			l.mu.Unlock()
			event, evErr, emit = LockReleaseFailed, uerr, true
		}
		return false
	}

	go c.refresher(l, jobID, locker, rctx, rcancel, done)

	// A fire of a job version that Add replaced while this fire was acquiring
	// is dropped, but the lease belongs to the job id: keep leadership and its
	// refresher so the next fire of the new version runs without another Lock.
	// 本次触发获取租约期间被 Add 替换的旧版本触发会被丢弃，但租约属于 job id：
	// 保留 leadership 与续期器，新版本的下次触发无需再次 Lock。
	c.mu.RLock()
	stillCurrent := c.jobs[jobID] == j
	c.mu.RUnlock()

	return stillCurrent
}

// gateFenced reports whether the lease gate has been closed for this fire
// since it passed the initial checks: Remove/RemoveAll tombstoned the state,
// or Stop fenced the whole Cron. It is evaluated while the state's opMu is
// held, right before the Lock is attempted. The state installed after a
// successful Lock is re-checked atomically with its installation in
// ensureLeader, so Remove/Stop cannot land between that check and the
// leadership.
// gateFenced 报告本次触发通过初始检查之后闸门是否已关闭：Remove/RemoveAll 写了
// 墓碑，或 Stop 隔离了整个 Cron。它在持有状态 opMu、即将尝试 Lock 时求值。
// Lock 成功之后安装的状态会在 ensureLeader 中与安装一起原子复查，因此
// Remove/Stop 不可能落在该检查与 leadership 之间。
func (c *Cron) gateFenced(l *jobLease, epoch uint64) bool {
	l.mu.Lock()
	removed := l.removed
	l.mu.Unlock()
	if removed {
		return true
	}

	c.mu.RLock()
	fenced := c.stopped || c.epoch != epoch
	c.mu.RUnlock()

	return fenced
}

// refresher extends the lease every refreshInterval until it is stopped or a
// Refresh call fails.
// refresher 每隔 refreshInterval 续期租约，直到被停止或某次 Refresh 失败。
func (c *Cron) refresher(l *jobLease, jobID string, locker taskcontract.Locker, ctx context.Context, cancel context.CancelFunc, done chan struct{}) {
	defer cancel()

	ticker := time.NewTicker(c.lockCfg.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			close(done)
			return
		case <-ticker.C:
		}

		// Derive the refresh from the refresher context so Stop/Shutdown/release
		// abort a slow backend immediately instead of waiting for the timeout.
		// 续期派生自续期器 context，Stop/Shutdown/release 会立即中止慢后端，
		// 而不是等超时。
		rctx, rcancel := context.WithTimeout(ctx, c.lockCfg.acquireTimeout)
		err := locker.Refresh(rctx)
		rcancel()
		if err != nil {
			if ctx.Err() != nil {
				// Aborted by Stop/Shutdown/release: not a lost lease.
				// 由 Stop/Shutdown/release 中止：不是丢锁。
				close(done)
				return
			}

			l.mu.Lock()
			l.leader = false
			// mayHold stays true on purpose: the lease may still be healthy,
			// and releasing it could hand the job to another replica.
			// mayHold 有意保持 true：租约可能仍然健康，释放它可能把任务交给
			// 另一个副本。
			l.mu.Unlock()

			// Close done before invoking the handler: the handler may block or
			// call back into Cron, and release must not wait on it.
			// 先关闭 done 再调用处理器：处理器可能阻塞或回调 Cron，
			// release 不应等它。
			close(done)
			c.emitLockEvent(jobID, LockLost, err)
			return
		}
	}
}

// releaseAllLeases releases every lease held by this Cron, using one overall
// acquire-timeout budget shared by all releases and running them concurrently,
// so N leases do not cost N timeouts. The states stay reusable: after a Stop,
// ensureLeader refuses while the Cron is stopped, and after a Start (or a
// Shutdown that let the in-flight jobs drain) a later fire may become leader
// again. It never holds the Cron mutex during lease I/O.
// releaseAllLeases 释放本 Cron 持有的所有租约，所有释放共享一份 acquire 超时预算
// 并并发执行，因此 N 个租约不会花费 N 份超时。状态保持可复用：Stop 之后
// ensureLeader 在 Cron 停止期间拒绝获取；Start 之后（或 Shutdown 等在途任务
// 排空后）新的触发可再次成为 leader。租约 I/O 期间不持有 Cron 锁。
func (c *Cron) releaseAllLeases() {
	refs := c.leaseSnapshot()
	if len(refs) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.lockCfg.acquireTimeout)
	defer cancel()

	c.releaseRefs(ctx, refs, false)
}

// releaseRemoved releases a tombstoned snapshot in the background, with one
// overall acquire-timeout budget shared by all releases. Remove and RemoveAll
// call it after writing the tombstones, so their caller never blocks on lease
// I/O.
// releaseRemoved 在后台用一份 acquire 超时预算释放墓碑快照。Remove 与 RemoveAll
// 在写墓碑后调用它，因此调用方绝不会因租约 I/O 而阻塞。
func (c *Cron) releaseRemoved(refs []leaseRef) {
	ctx, cancel := context.WithTimeout(context.Background(), c.lockCfg.acquireTimeout)
	defer cancel()

	c.releaseRefs(ctx, refs, true)
}

// leaseSnapshot snapshots Cron.leases together with each state's leader
// generation, so a release can detect leadership acquired after the snapshot.
// leaseSnapshot 快照 Cron.leases 及每个状态的 leader 代数，使释放能发现快照之后
// 才获取的 leadership。
func (c *Cron) leaseSnapshot() []leaseRef {
	c.mu.RLock()
	defer c.mu.RUnlock()

	refs := make([]leaseRef, 0, len(c.leases))
	for id, l := range c.leases {
		l.mu.Lock()
		gen := l.leaderGen
		l.mu.Unlock()
		refs = append(refs, leaseRef{id: id, lease: l, gen: gen})
	}

	return refs
}

// leaseRef names a lease and the leader generation observed in a snapshot
// taken from Cron.leases.
// leaseRef 是取自 Cron.leases 快照中的一项：租约及其被观察到的 leader 代数。
type leaseRef struct {
	id    string
	lease *jobLease
	gen   uint64
}

// releaseRefs releases a snapshot of leases concurrently, one goroutine per
// state, and waits for all of them. Every Unlock shares ctx, so the batch has a
// single overall budget instead of one timeout per lease. Once ctx expires no
// more releases are started: the untouched states (and their tombstones) are
// left for a later Stop/Shutdown retry.
// releaseRefs 并发释放租约快照（每个状态一个 goroutine）并等待全部完成。所有
// Unlock 共享 ctx，因此整批只有一份总预算，而不是每个租约一份超时。ctx 到期后
// 不再启动新的释放：未处理的状态（及其墓碑）留给之后的 Stop/Shutdown 重试。
func (c *Cron) releaseRefs(ctx context.Context, refs []leaseRef, markRemoved bool) {
	var wg sync.WaitGroup

	for _, ref := range refs {
		if ctx.Err() != nil {
			// Budget exhausted: do not start more releases.
			// 预算已耗尽：不再启动新的释放。
			break
		}

		wg.Add(1)
		go func(ref leaseRef) {
			defer wg.Done()
			c.releaseLease(ctx, ref, markRemoved)
		}(ref)
	}

	wg.Wait()
}

// releaseLease stops the refresher and, when the lease may still be held,
// releases it.
//
// Order: check the leader generation, tombstone (when markRemoved), mark not
// leader, cancel the refresher, wait for it to exit, then Unlock with the
// batch deadline. Everything happens under the state's opMu, so a concurrent
// ensureLeader either sees the tombstone before it can Lock or runs its Lock
// after the Unlock, never between them.
//
// When leaderGen changed since the snapshot, a newer Lock already owns the
// lease (eg. a fire that became leader after a concurrent Start): the stale
// release must not cancel its refresher, Unlock it, or re-apply the removal
// tombstone (the state was reopened and re-acquired since this removal was
// settled), so it skips all three.
//
// A failed Unlock keeps mayHold and the tombstone in place for a later retry
// and reports LockReleaseFailed. A successful Unlock clears mayHold and settles
// a tombstoned state: it is deleted when the job is still removed, or reopened
// (removed=false) when the id was re-added meanwhile. finishRemoval re-checks
// mayHold, so a failed release never forgets the state or its cached Locker.
// releaseLease 停止续期器，并在租约"可能仍被持有"时释放它。
//
// 顺序：检查 leader 代数、写墓碑（markRemoved 时）、标记非 leader、取消续期器、
// 等待其退出，再用整批共享的截止时间 Unlock。整个过程在状态的 opMu 下进行，因此
// 并发的 ensureLeader 要么在看到墓碑后放弃 Lock，要么在 Unlock 之后才 Lock，
// 绝不会落在两者之间。
//
// 若 leaderGen 与快照不同，说明更新的 Lock 已持有租约（如在并发 Start 后成为
// leader 的触发）：旧的释放不得取消其续期器、不得 Unlock 它，也不得重新写移除
// 墓碑（该状态自这次移除结算后已被重新开放并重新获取），因此三者都跳过。
//
// Unlock 失败会保留 mayHold 与墓碑供之后重试，并上报 LockReleaseFailed。Unlock
// 成功则清除 mayHold 并结算墓碑状态：任务仍被移除时删除，期间被重新添加时重新
// 开放（removed=false）。finishRemoval 会再次检查 mayHold，因此失败的释放绝不
// 遗忘状态或其缓存的 Locker。
func (c *Cron) releaseLease(ctx context.Context, ref leaseRef, markRemoved bool) {
	jobID, l := ref.id, ref.lease

	l.opMu.Lock()

	var (
		event LockEvent
		evErr error
		emit  bool
	)
	defer func() {
		// Settlement needs opMu: only after this Unlock may a re-added job
		// acquire again, so its Lock cannot race with the stale release. A
		// failed Unlock (mayHold still true) is never settled, so the state and
		// its cached Locker stay for a later Stop/Shutdown retry.
		// 结算需要 opMu：只有 Unlock 之后被重新添加的任务才能再次获取，其 Lock
		// 不会与这次旧释放竞争。Unlock 失败（mayHold 仍为 true）绝不结算，状态与
		// 其缓存的 Locker 保留给之后的 Stop/Shutdown 重试。
		l.mu.Lock()
		settle := l.removed && !l.mayHold
		l.mu.Unlock()
		if settle {
			c.finishRemoval(jobID, l)
		}
		l.opMu.Unlock()
		if emit {
			c.emitLockEvent(jobID, event, evErr)
		}
	}()

	l.mu.Lock()
	if l.leaderGen != ref.gen {
		// A newer leadership owns the lease: leave its refresher and lease
		// untouched. Do not (re)apply the tombstone either: the state was
		// reopened and re-acquired after this release snapshot, so a tombstone
		// now would fence the re-added id while the newer refresher keeps its
		// lease. The earlier release of the same removal already settled it.
		// 更新的 leadership 持有租约：不碰其续期器与租约。也不要（重新）写墓碑：
		// 该状态在此释放快照之后已被重新开放并重新获取，此时写墓碑会隔离重新添加
		// 的 id，而新的续期器仍在续期其租约。同一次移除的更早释放已经结算过它。
		l.mu.Unlock()
		return
	}
	if markRemoved {
		l.removed = true
	}
	l.leader = false
	cancel := l.refreshCancel
	done := l.refreshDone
	locker := l.locker
	mayHold := l.mayHold
	l.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}

	// Only attempt Unlock when the lease may still be held; a recovered
	// local-only job never has one.
	// 只有租约"可能仍被持有"时才尝试 Unlock；纯本地任务从来没有租约。
	if !mayHold || locker == nil {
		return
	}

	if err := locker.Unlock(ctx); err != nil {
		// The lease may still be held: keep mayHold (and a tombstone) so a
		// later release retries.
		// 租约可能仍被持有：保留 mayHold（与墓碑），让之后的释放重试。
		event, evErr, emit = LockReleaseFailed, err, true
		return
	}

	l.mu.Lock()
	l.mayHold = false
	l.mu.Unlock()
}
