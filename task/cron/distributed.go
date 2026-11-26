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
// goroutine, the lease refresher, or the caller of Stop/Shutdown/Remove. It
// may therefore run concurrently for different jobs and must be fast and
// concurrency-safe. Do not block in it, and do not call back into the same
// Cron (a handler invoked from the refresher would deadlock a Stop that waits
// for that refresher to exit).
// LockEventHandler 接收分布式租约事件。
//
// 它由观察到事件的 goroutine 同步调用：任务 goroutine、租约续期 goroutine，
// 或 Stop/Shutdown/Remove 的调用方。因此不同任务的事件可能并发到达，处理器
// 必须快速且并发安全。不要在其中阻塞，也不要回调同一个 Cron（续期 goroutine
// 里调用的处理器若去 Stop，会等待该续期器退出而死锁）。
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
// finishRemoval 在释放完成后处理墓碑租约。
//
// 它在状态的 opMu 下运行，因此旧的 Unlock 已经发生（或本就无需释放），不会有新的
// 获取插入。释放期间被重新添加的任务保留其状态（同一个 opMu）并重新开放给下次
// 触发；仍被移除的任务则彻底遗忘该状态。
func (c *Cron) finishRemoval(jobID string, l *jobLease) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.leases[jobID] != l {
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
			if c.gateFenced(l, epoch) {
				return false
			}
			l.mu.Lock()
			l.localOnly = true
			l.mu.Unlock()
			return true
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
	// 在 provider 或 Lock 进行期间闸门可能已被隔离：Remove/RemoveAll 写了墓碑，
	// 或 Stop 隔离了 Cron。刚获取的租约不能保留：在这里释放并跳过运行。排在 opMu
	// 之后的释放随后会看到 mayHold 为 false。
	if c.gateFenced(l, epoch) {
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

	done := make(chan struct{})
	rctx, rcancel := context.WithCancel(context.Background())

	l.mu.Lock()
	l.leader = true
	l.mayHold = true
	l.refreshCancel = rcancel
	l.refreshDone = done
	l.mu.Unlock()

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
// or Stop fenced the whole Cron. It is evaluated after provider/Lock I/O,
// while the state's opMu is held, so a fenced fire can release before any
// replacement can acquire.
// gateFenced 报告本次触发通过初始检查之后闸门是否已关闭：Remove/RemoveAll 写了
// 墓碑，或 Stop 隔离了整个 Cron。它在 provider/Lock I/O 之后、持有状态 opMu 时
// 求值，因此被隔离的触发会在任何替代者获取之前完成释放。
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

// releaseAllLeases releases every lease held by this Cron. The states stay
// reusable: after a Stop, ensureLeader refuses while the Cron is stopped, and
// after a Start (or a Shutdown that let the in-flight jobs drain) a later fire
// may become leader again. It never holds the Cron mutex during lease I/O.
// releaseAllLeases 释放本 Cron 持有的所有租约。状态保持可复用：Stop 之后
// ensureLeader 在 Cron 停止期间拒绝获取；Start 之后（或 Shutdown 等在途任务
// 排空后）新的触发可再次成为 leader。租约 I/O 期间不持有 Cron 锁。
func (c *Cron) releaseAllLeases() {
	c.mu.RLock()
	refs := make([]leaseRef, 0, len(c.leases))
	for id, l := range c.leases {
		refs = append(refs, leaseRef{id: id, lease: l})
	}
	c.mu.RUnlock()

	for _, ref := range refs {
		c.releaseLease(ref.id, ref.lease, false)
	}
}

// leaseRef names a lease in a snapshot taken from Cron.leases.
// leaseRef 是取自 Cron.leases 快照中的一项。
type leaseRef struct {
	id    string
	lease *jobLease
}

// releaseLease stops the refresher and, when the lease may still be held,
// releases it.
//
// Order: mark not leader and tombstone (when markRemoved), cancel the
// refresher, wait for it to exit, then Unlock with a timeout. Everything
// happens under the state's opMu, so a concurrent ensureLeader either sees the
// tombstone before it can Lock or runs its Lock after the Unlock, never between
// them. A Refresher error deliberately leaves mayHold true, so a later release
// still attempts Unlock; a successful Unlock clears it.
// releaseLease 停止续期器，并在租约"可能仍被持有"时释放它。
//
// 顺序：标记非 leader 并写墓碑（markRemoved 时）、取消续期器、等待其退出、再带
// 超时 Unlock。整个过程在状态的 opMu 下进行，因此并发的 ensureLeader 要么在看到
// 墓碑后放弃 Lock，要么在 Unlock 之后才 Lock，绝不会落在两者之间。续期错误有意
// 保持 mayHold 为 true，因此之后的释放仍会尝试 Unlock；Unlock 成功才清除它。
func (c *Cron) releaseLease(jobID string, l *jobLease, markRemoved bool) {
	l.opMu.Lock()

	var (
		event LockEvent
		evErr error
		emit  bool
	)
	defer func() {
		// Settlement needs opMu: only after this Unlock may a re-added job
		// acquire again, so its Lock cannot race with the stale release.
		// 结算需要 opMu：只有 Unlock 之后被重新添加的任务才能再次获取，其 Lock
		// 不会与这次旧释放竞争。
		if markRemoved {
			c.finishRemoval(jobID, l)
		}
		l.opMu.Unlock()
		if emit {
			c.emitLockEvent(jobID, event, evErr)
		}
	}()

	l.mu.Lock()
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

	ctx, rcancel := context.WithTimeout(context.Background(), c.lockCfg.acquireTimeout)
	err := locker.Unlock(ctx)
	rcancel()
	if err != nil {
		// The lease may still be held: keep mayHold so a later release retries.
		// 租约可能仍被持有：保留 mayHold，让之后的释放重试。
		event, evErr, emit = LockReleaseFailed, err, true
		return
	}

	l.mu.Lock()
	l.mayHold = false
	l.mu.Unlock()
}

// releaseRefs releases a snapshot of leases taken while the Cron lock was
// held, marking each state as removed.
// releaseRefs 释放持 Cron 锁时取得的租约快照，并把每个状态标记为已移除。
func (c *Cron) releaseRefs(refs []leaseRef) {
	for _, ref := range refs {
		c.releaseLease(ref.id, ref.lease, true)
	}
}
