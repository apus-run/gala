package cron

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	taskcontract "github.com/apus-run/gala/task"
)

const fakeJobID = "job"

var (
	errFakeLocked   = errors.New("fake: lease held by another owner")
	errFakeNotHeld  = errors.New("fake: lease not held")
	errFakeProvider = errors.New("fake: provider failed")
)

// fakeBackend is an in-memory lease store shared by several Cron instances.
// It records every call so tests can assert what the cron did.
// fakeBackend 是多个 Cron 实例共享的内存租约存储，记录所有调用以便断言。
type fakeBackend struct {
	mu        sync.Mutex
	owner     map[string]string // jobID -> owner token | 任务 -> 持有者令牌
	lockCalls int
	refreshes int
	unlocks   int
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{owner: map[string]string{}}
}

func (b *fakeBackend) ownerOf(jobID string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.owner[jobID]
}

func (b *fakeBackend) counts() (locks, refreshes, unlocks int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lockCalls, b.refreshes, b.unlocks
}

func (b *fakeBackend) refreshCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.refreshes
}

// fakeLocker is one replica's Locker for one job id.
// fakeLocker 是一个副本针对一个任务 id 的 Locker。
type fakeLocker struct {
	backend     *fakeBackend
	owner       string
	jobID       string
	failRefresh atomic.Bool

	// blockRefresh makes Refresh wait for its context and return ctx.Err(),
	// modeling a slow backend that must be cancellable by Stop/Shutdown.
	// blockRefresh 让 Refresh 等待其 context 并返回 ctx.Err()，模拟必须能被
	// Stop/Shutdown 取消的慢后端。
	blockRefresh   atomic.Bool
	refreshEntered chan struct{}
}

func newFakeLocker(b *fakeBackend, owner, jobID string) *fakeLocker {
	return &fakeLocker{backend: b, owner: owner, jobID: jobID}
}

func (f *fakeLocker) Lock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	f.backend.mu.Lock()
	defer f.backend.mu.Unlock()

	f.backend.lockCalls++
	if cur, held := f.backend.owner[f.jobID]; held && cur != f.owner {
		return errFakeLocked
	}
	f.backend.owner[f.jobID] = f.owner
	return nil
}

func (f *fakeLocker) Unlock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	f.backend.mu.Lock()
	defer f.backend.mu.Unlock()

	f.backend.unlocks++
	if cur, held := f.backend.owner[f.jobID]; !held || cur != f.owner {
		return errFakeNotHeld
	}
	delete(f.backend.owner, f.jobID)
	return nil
}

func (f *fakeLocker) Refresh(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if f.blockRefresh.Load() {
		if f.refreshEntered != nil {
			select {
			case f.refreshEntered <- struct{}{}:
			default:
			}
		}
		<-ctx.Done()
		return ctx.Err()
	}

	f.backend.mu.Lock()
	defer f.backend.mu.Unlock()

	f.backend.refreshes++
	if cur, held := f.backend.owner[f.jobID]; !held || cur != f.owner {
		return errFakeNotHeld
	}
	if f.failRefresh.Load() {
		// Model the lease expiring while the refresh itself fails.
		// 模拟续期失败的同时租约过期。
		delete(f.backend.owner, f.jobID)
		return errors.New("fake: refresh failed")
	}
	return nil
}

// fixedProvider always returns the same Locker (single-job tests).
// fixedProvider 总是返回同一个 Locker（单任务测试）。
func fixedProvider(l *fakeLocker) LockerProvider {
	return func(context.Context, string) (taskcontract.Locker, error) {
		return l, nil
	}
}

// fakeProviderSet creates one fake Locker per job id for a replica.
// fakeProviderSet 为每个副本按任务 id 创建独立的 fake Locker。
type fakeProviderSet struct {
	backend *fakeBackend
	owner   string

	mu      sync.Mutex
	lockers map[string]*fakeLocker
}

func newFakeProviderSet(b *fakeBackend, owner string) *fakeProviderSet {
	return &fakeProviderSet{backend: b, owner: owner, lockers: map[string]*fakeLocker{}}
}

func (p *fakeProviderSet) provider() LockerProvider {
	return func(_ context.Context, jobID string) (taskcontract.Locker, error) {
		p.mu.Lock()
		defer p.mu.Unlock()

		l := p.lockers[jobID]
		if l == nil {
			l = newFakeLocker(p.backend, p.owner, jobID)
			p.lockers[jobID] = l
		}
		return l, nil
	}
}

func (p *fakeProviderSet) locker(jobID string) *fakeLocker {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lockers[jobID]
}

// recordedLockEvent is one handler invocation | recordedLockEvent 是一次处理器调用。
type recordedLockEvent struct {
	jobID string
	event LockEvent
	err   error
}

type lockEventRecorder struct {
	ch chan recordedLockEvent
}

func newLockEventRecorder() *lockEventRecorder {
	return &lockEventRecorder{ch: make(chan recordedLockEvent, 32)}
}

func (r *lockEventRecorder) handler() LockEventHandler {
	return func(jobID string, event LockEvent, err error) {
		r.ch <- recordedLockEvent{jobID: jobID, event: event, err: err}
	}
}

func (r *lockEventRecorder) wait(t *testing.T, want LockEvent) recordedLockEvent {
	t.Helper()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case rec := <-r.ch:
			if rec.event == want {
				return rec
			}
		case <-deadline:
			t.Fatalf("timed out waiting for lock event %s", want)
			return recordedLockEvent{}
		}
	}
}

func (r *lockEventRecorder) none(t *testing.T) {
	t.Helper()

	select {
	case rec := <-r.ch:
		t.Fatalf("unexpected lock event %s for %q: %v", rec.event, rec.jobID, rec.err)
	case <-time.After(50 * time.Millisecond):
	}
}

// waitFor polls cond until it is true or a bounded deadline expires.
// waitFor 轮询 cond 直到为真或到达有界截止时间。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()

	for !cond() {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		case <-tick.C:
		}
	}
}

// waitRun waits for a job run and returns the fake lease owner it saw.
// waitRun 等待一次任务运行并返回它看到的 fake 租约持有者。
func waitRun(t *testing.T, runs <-chan string) string {
	t.Helper()

	select {
	case owner := <-runs:
		return owner
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the job to run")
		return ""
	}
}

func expectNoRun(t *testing.T, runs <-chan string) {
	t.Helper()

	select {
	case owner := <-runs:
		t.Fatalf("unexpected job run (lease owner %q)", owner)
	case <-time.After(50 * time.Millisecond):
	}
}

// leaseOf returns the lease state of a job (nil after Remove).
// leaseOf 返回任务的租约状态（Remove 之后为 nil）。
func leaseOf(c *Cron, jobID string) *jobLease {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.leases[jobID]
}

// refresherStopped reports whether the lease's refresher goroutine has exited.
// refresherStopped 报告租约的续期 goroutine 是否已退出。
func (l *jobLease) refresherStopped() bool {
	if l == nil {
		return true
	}

	l.mu.Lock()
	done := l.refreshDone
	l.mu.Unlock()

	if done == nil {
		return true
	}

	select {
	case <-done:
		return true
	default:
		return false
	}
}

// TestDistributedOnlyLeaderRuns covers two replicas firing the same job for
// the same due minute: exactly one executes.
// TestDistributedOnlyLeaderRuns 覆盖两个副本在同一到期分钟触发同一任务：
// 恰好一个执行。
func TestDistributedOnlyLeaderRuns(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	runs := make(chan string, 4)

	lockerA := newFakeLocker(backend, "A", fakeJobID)
	lockerB := newFakeLocker(backend, "B", fakeJobID)
	events := newLockEventRecorder()

	c1 := New(WithLockerProvider(fixedProvider(lockerA)))
	c2 := New(WithLockerProvider(fixedProvider(lockerB), WithLockEventHandler(events.handler())))

	job := func() { runs <- backend.ownerOf(fakeJobID) }
	c1.MustAdd(fakeJobID, "* * * * *", job)
	c2.MustAdd(fakeJobID, "* * * * *", job)

	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	c1.runDue(base)
	if owner := waitRun(t, runs); owner != "A" {
		t.Fatalf("first run: lease owner = %q, want A", owner)
	}

	c2.runDue(base) // same due minute on the follower | follower 在同一到期分钟触发
	rec := events.wait(t, LockAcquireFailed)
	if rec.jobID != fakeJobID || !errors.Is(rec.err, errFakeLocked) {
		t.Fatalf("unexpected acquire failure event: %+v", rec)
	}

	expectNoRun(t, runs)
	if owner := backend.ownerOf(fakeJobID); owner != "A" {
		t.Fatalf("lease owner = %q, want A", owner)
	}

	if err := c1.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c2.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if owner := backend.ownerOf(fakeJobID); owner != "" {
		t.Fatalf("lease not released after Shutdown: owner %q", owner)
	}
}

// TestDistributedStopReleasesAndFollowerTakesOver covers leader Stop()
// releasing the lease and the other replica taking over on the next fire.
// TestDistributedStopReleasesAndFollowerTakesOver 覆盖 leader 调用 Stop() 释放
// 租约、另一副本在下次触发时接管。
func TestDistributedStopReleasesAndFollowerTakesOver(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	runs := make(chan string, 4)

	lockerA := newFakeLocker(backend, "A", fakeJobID)
	lockerB := newFakeLocker(backend, "B", fakeJobID)

	c1 := New(WithLockerProvider(fixedProvider(lockerA)))
	c2 := New(WithLockerProvider(fixedProvider(lockerB)))

	job := func() { runs <- backend.ownerOf(fakeJobID) }
	c1.MustAdd(fakeJobID, "* * * * *", job)
	c2.MustAdd(fakeJobID, "* * * * *", job)

	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	c1.runDue(base)
	if owner := waitRun(t, runs); owner != "A" {
		t.Fatalf("first run: lease owner = %q, want A", owner)
	}

	c1.Stop()
	if owner := backend.ownerOf(fakeJobID); owner != "" {
		t.Fatalf("Stop must release the lease, owner = %q", owner)
	}
	if !leaseOf(c1, fakeJobID).refresherStopped() {
		t.Fatal("Stop left a refresher goroutine running")
	}

	c2.runDue(base.Add(time.Minute))
	if owner := waitRun(t, runs); owner != "B" {
		t.Fatalf("takeover run: lease owner = %q, want B", owner)
	}
	if owner := backend.ownerOf(fakeJobID); owner != "B" {
		t.Fatalf("lease owner after takeover = %q, want B", owner)
	}

	if err := c2.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if owner := backend.ownerOf(fakeJobID); owner != "" {
		t.Fatalf("lease not released: owner %q", owner)
	}
}

// TestDistributedRefreshFailureDemotesAndReacquires covers Refresh failure:
// demote, emit LockLost, do not Unlock, and allow a later fire to re-acquire.
// TestDistributedRefreshFailureDemotesAndReacquires 覆盖 Refresh 失败：降级、
// 上报 LockLost、不 Unlock，之后到期触发可重新获取。
func TestDistributedRefreshFailureDemotesAndReacquires(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	locker := newFakeLocker(backend, "A", fakeJobID)
	events := newLockEventRecorder()

	c := New(WithLockerProvider(
		fixedProvider(locker),
		WithLockRefreshInterval(5*time.Millisecond),
		WithLockEventHandler(events.handler()),
	))

	var runs atomic.Int32
	c.MustAdd(fakeJobID, "* * * * *", func() { runs.Add(1) })

	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	c.runDue(base)
	waitFor(t, "the first run", func() bool { return runs.Load() == 1 })
	waitFor(t, "the refresher to run", func() bool { return backend.refreshCount() >= 1 })

	locker.failRefresh.Store(true)
	rec := events.wait(t, LockLost)
	if rec.jobID != fakeJobID || rec.err == nil {
		t.Fatalf("unexpected LockLost event: %+v", rec)
	}
	if owner := backend.ownerOf(fakeJobID); owner != "" {
		t.Fatalf("the fake lease should have expired, owner = %q", owner)
	}
	if _, _, unlocks := backend.counts(); unlocks != 0 {
		t.Fatalf("a Refresh error must not trigger Unlock, got %d unlock calls", unlocks)
	}
	if !leaseOf(c, fakeJobID).refresherStopped() {
		t.Fatal("the refresher is still running after LockLost")
	}

	locker.failRefresh.Store(false)
	c.runDue(base.Add(time.Minute))
	waitFor(t, "the re-acquire run", func() bool { return runs.Load() == 2 })
	if owner := backend.ownerOf(fakeJobID); owner != "A" {
		t.Fatalf("lease owner after re-acquire = %q, want A", owner)
	}

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if owner := backend.ownerOf(fakeJobID); owner != "" {
		t.Fatalf("lease not released: owner %q", owner)
	}
}

// TestDistributedReleaseFailureEvent covers a failed release: after LockLost
// leaves mayHold true, Stop attempts Unlock and reports the "not held" error.
// TestDistributedReleaseFailureEvent 覆盖释放失败：LockLost 保留 mayHold 为
// true 后，Stop 尝试 Unlock 并上报"已不持有"错误。
func TestDistributedReleaseFailureEvent(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	locker := newFakeLocker(backend, "A", fakeJobID)
	events := newLockEventRecorder()

	c := New(WithLockerProvider(
		fixedProvider(locker),
		WithLockRefreshInterval(5*time.Millisecond),
		WithLockEventHandler(events.handler()),
	))

	var runs atomic.Int32
	c.MustAdd(fakeJobID, "* * * * *", func() { runs.Add(1) })

	c.runDue(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))
	waitFor(t, "the first run", func() bool { return runs.Load() == 1 })
	waitFor(t, "the refresher to run", func() bool { return backend.refreshCount() >= 1 })

	locker.failRefresh.Store(true)
	events.wait(t, LockLost)

	// Stop still attempts Unlock because the lease may be held; the fake has
	// already expired it, so the failure is reported.
	// 租约可能仍被持有，Stop 仍会尝试 Unlock；fake 已让它过期，因此上报失败。
	c.Stop()

	rec := events.wait(t, LockReleaseFailed)
	if rec.jobID != fakeJobID || !errors.Is(rec.err, errFakeNotHeld) {
		t.Fatalf("unexpected release failure event: %+v", rec)
	}
	if !leaseOf(c, fakeJobID).refresherStopped() {
		t.Fatal("Stop left a refresher goroutine running")
	}
}

// TestDistributedNilProviderRunsLocally covers a provider returning (nil, nil):
// the job runs locally and the provider is never called again.
// TestDistributedNilProviderRunsLocally 覆盖 provider 返回 (nil, nil)：任务
// 本地运行，且 provider 不再被调用。
func TestDistributedNilProviderRunsLocally(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	provider := func(context.Context, string) (taskcontract.Locker, error) {
		calls.Add(1)
		return nil, nil
	}
	events := newLockEventRecorder()

	c := New(WithLockerProvider(provider, WithLockEventHandler(events.handler())))

	var runs atomic.Int32
	c.MustAdd(fakeJobID, "* * * * *", func() { runs.Add(1) })

	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	c.runDue(base)
	waitFor(t, "the first local run", func() bool { return runs.Load() == 1 })

	c.runDue(base.Add(time.Minute))
	waitFor(t, "the second local run", func() bool { return runs.Load() == 2 })

	if got := calls.Load(); got != 1 {
		t.Fatalf("provider called %d times, want 1 (local-only is cached)", got)
	}
	events.none(t)

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestDistributedProviderErrorSkipsAndRetries covers provider error handling:
// skip the run, emit LockProviderFailed, retry on the next due fire.
// TestDistributedProviderErrorSkipsAndRetries 覆盖 provider 错误：跳过运行、
// 上报 LockProviderFailed、下次到期重试。
func TestDistributedProviderErrorSkipsAndRetries(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	locker := newFakeLocker(backend, "A", fakeJobID)
	events := newLockEventRecorder()

	var calls atomic.Int32
	provider := func(context.Context, string) (taskcontract.Locker, error) {
		if calls.Add(1) == 1 {
			return nil, errFakeProvider
		}
		return locker, nil
	}

	c := New(WithLockerProvider(provider, WithLockEventHandler(events.handler())))

	var runs atomic.Int32
	c.MustAdd(fakeJobID, "* * * * *", func() { runs.Add(1) })

	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	c.runDue(base)
	rec := events.wait(t, LockProviderFailed)
	if rec.jobID != fakeJobID || !errors.Is(rec.err, errFakeProvider) {
		t.Fatalf("unexpected provider failure event: %+v", rec)
	}
	if runs.Load() != 0 {
		t.Fatal("the job ran although the provider failed")
	}

	// The provider error is not cached: the next due fire retries.
	// provider 错误不缓存：下次到期触发重试。
	c.runDue(base.Add(time.Minute))
	waitFor(t, "the retried run", func() bool { return runs.Load() == 1 })
	if got := calls.Load(); got != 2 {
		t.Fatalf("provider called %d times, want 2", got)
	}

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if owner := backend.ownerOf(fakeJobID); owner != "" {
		t.Fatalf("lease not released: owner %q", owner)
	}
}

// TestDistributedShutdownWaitsForInflightThenReleases covers Shutdown waiting
// for a running job while the lease is still held, then releasing it.
// TestDistributedShutdownWaitsForInflightThenReleases 覆盖 Shutdown 在任务运行
// 期间仍持有租约地等待，随后释放租约。
func TestDistributedShutdownWaitsForInflightThenReleases(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	locker := newFakeLocker(backend, "A", fakeJobID)

	c := New(WithLockerProvider(fixedProvider(locker)))

	started := make(chan struct{})
	release := make(chan struct{})
	c.MustAdd(fakeJobID, "* * * * *", func() {
		close(started)
		<-release
	})

	c.runDue(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the job did not start")
	}
	if owner := backend.ownerOf(fakeJobID); owner != "A" {
		t.Fatalf("lease owner while running = %q, want A", owner)
	}

	done := make(chan error, 1)
	go func() { done <- c.Shutdown(context.Background()) }()

	select {
	case err := <-done:
		t.Fatalf("Shutdown returned before the job finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if owner := backend.ownerOf(fakeJobID); owner != "A" {
		t.Fatalf("Shutdown released the lease while the job is still running: owner %q", owner)
	}

	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown did not return after the job finished")
	}

	if owner := backend.ownerOf(fakeJobID); owner != "" {
		t.Fatalf("Shutdown did not release the lease: owner %q", owner)
	}
	if !leaseOf(c, fakeJobID).refresherStopped() {
		t.Fatal("Shutdown left a refresher goroutine running")
	}
}

// TestDistributedLeaderRefreshesWhileRunning covers a long-running leader
// refreshing its lease at least once before it finishes.
// TestDistributedLeaderRefreshesWhileRunning 覆盖长任务作为 leader 在结束前至少
// 续期一次。
func TestDistributedLeaderRefreshesWhileRunning(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	locker := newFakeLocker(backend, "A", fakeJobID)

	c := New(WithLockerProvider(fixedProvider(locker), WithLockRefreshInterval(5*time.Millisecond)))

	started := make(chan struct{})
	release := make(chan struct{})
	c.MustAdd(fakeJobID, "* * * * *", func() {
		close(started)
		<-release
	})

	c.runDue(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the job did not start")
	}

	waitFor(t, "the leader to refresh twice", func() bool { return backend.refreshCount() >= 2 })
	if owner := backend.ownerOf(fakeJobID); owner != "A" {
		t.Fatalf("lease owner while running = %q, want A", owner)
	}

	close(release)
	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if owner := backend.ownerOf(fakeJobID); owner != "" {
		t.Fatalf("lease not released: owner %q", owner)
	}
}

// TestDistributedShutdownCtxExpiryKeepsLease covers Shutdown returning
// ctx.Err() without releasing while the job is still running; a later
// Shutdown releases the lease.
// TestDistributedShutdownCtxExpiryKeepsLease 覆盖任务仍在运行时 Shutdown 返回
// ctx.Err() 且不释放租约；之后的 Shutdown 会释放。
func TestDistributedShutdownCtxExpiryKeepsLease(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	locker := newFakeLocker(backend, "A", fakeJobID)

	c := New(WithLockerProvider(fixedProvider(locker), WithLockRefreshInterval(5*time.Millisecond)))

	started := make(chan struct{})
	release := make(chan struct{})
	c.MustAdd(fakeJobID, "* * * * *", func() {
		close(started)
		<-release
	})

	c.runDue(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the job did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := c.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v, want context.DeadlineExceeded", err)
	}

	if owner := backend.ownerOf(fakeJobID); owner != "A" {
		t.Fatalf("Shutdown released the lease although ctx expired: owner %q", owner)
	}

	// The background job must keep its refresher running.
	// 后台任务必须继续运行续期器。
	before := backend.refreshCount()
	waitFor(t, "the background refresher to keep going", func() bool {
		return backend.refreshCount() > before
	})

	close(release)
	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if owner := backend.ownerOf(fakeJobID); owner != "" {
		t.Fatalf("the later Shutdown did not release the lease: owner %q", owner)
	}
	if !leaseOf(c, fakeJobID).refresherStopped() {
		t.Fatal("the refresher is still running after Shutdown")
	}
}

// TestDistributedRemoveReleasesLease covers Remove releasing the lease and
// forgetting its state.
// TestDistributedRemoveReleasesLease 覆盖 Remove 释放租约并遗忘其状态。
func TestDistributedRemoveReleasesLease(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	locker := newFakeLocker(backend, "A", fakeJobID)

	c := New(WithLockerProvider(fixedProvider(locker)))

	var runs atomic.Int32
	c.MustAdd(fakeJobID, "* * * * *", func() { runs.Add(1) })

	c.runDue(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))
	waitFor(t, "the run", func() bool { return runs.Load() == 1 })

	l := leaseOf(c, fakeJobID)
	if l == nil {
		t.Fatal("expected lease state")
	}

	c.Remove(fakeJobID)

	if owner := backend.ownerOf(fakeJobID); owner != "" {
		t.Fatalf("Remove did not release the lease: owner %q", owner)
	}
	if !l.refresherStopped() {
		t.Fatal("Remove left a refresher goroutine running")
	}
	if leaseOf(c, fakeJobID) != nil {
		t.Fatal("Remove did not forget the lease state")
	}
	if c.Total() != 0 {
		t.Fatalf("job not removed, Total = %d", c.Total())
	}
}

// TestDistributedRemoveAllReleasesLeases covers RemoveAll releasing every
// lease it held.
// TestDistributedRemoveAllReleasesLeases 覆盖 RemoveAll 释放它持有的所有租约。
func TestDistributedRemoveAllReleasesLeases(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	provider := newFakeProviderSet(backend, "A")

	c := New(WithLockerProvider(provider.provider()))

	runs := make(chan string, 4)
	job := func() { runs <- "ran" }
	c.MustAdd("one", "* * * * *", job)
	c.MustAdd("two", "* * * * *", job)

	c.runDue(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))

	waitRun(t, runs)
	waitRun(t, runs)

	l1 := leaseOf(c, "one")
	l2 := leaseOf(c, "two")
	if l1 == nil || l2 == nil {
		t.Fatal("expected lease state for both jobs")
	}
	if owner := backend.ownerOf("one"); owner != "A" {
		t.Fatalf("job one lease owner = %q, want A", owner)
	}
	if owner := backend.ownerOf("two"); owner != "A" {
		t.Fatalf("job two lease owner = %q, want A", owner)
	}

	c.RemoveAll()

	for _, jobID := range []string{"one", "two"} {
		if owner := backend.ownerOf(jobID); owner != "" {
			t.Fatalf("RemoveAll did not release %q: owner %q", jobID, owner)
		}
	}
	if !l1.refresherStopped() || !l2.refresherStopped() {
		t.Fatal("RemoveAll left a refresher goroutine running")
	}
	if c.Total() != 0 || len(c.leases) != 0 {
		t.Fatalf("RemoveAll left state: jobs=%d leases=%d", c.Total(), len(c.leases))
	}
}

// TestDistributedAddPreservesLeadership covers Add replacing a job without
// losing its lease state: the next fire runs without another Lock.
// TestDistributedAddPreservesLeadership 覆盖 Add 替换任务但不丢失租约状态：
// 下次触发无需再次 Lock。
func TestDistributedAddPreservesLeadership(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	locker := newFakeLocker(backend, "A", fakeJobID)

	c := New(WithLockerProvider(fixedProvider(locker)))

	var runs atomic.Int32
	c.MustAdd(fakeJobID, "* * * * *", func() { runs.Add(1) })

	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	c.runDue(base)
	waitFor(t, "the first run", func() bool { return runs.Load() == 1 })

	// Replace the job: the lease state is keyed by id and must survive.
	// 替换任务：租约状态按 id 保存，必须保留。
	c.MustAdd(fakeJobID, "* * * * *", func() { runs.Add(1) })

	c.runDue(base.Add(time.Minute))
	waitFor(t, "the run after replacement", func() bool { return runs.Load() == 2 })

	if locks, _, _ := backend.counts(); locks != 1 {
		t.Fatalf("Add lost leadership: %d Lock calls, want 1", locks)
	}

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestDistributedLockOptions covers option defaults, non-positive values and
// the timeout context passed to the provider.
// TestDistributedLockOptions 覆盖选项默认值、非正值以及传给 provider 的超时 ctx。
func TestDistributedLockOptions(t *testing.T) {
	t.Parallel()

	noop := func(context.Context, string) (taskcontract.Locker, error) { return nil, nil }

	// nil provider is a no-op | nil provider 是空操作
	c := New(WithLockerProvider(nil, WithLockRefreshInterval(time.Second)))
	if c.lockProvider != nil {
		t.Fatal("WithLockerProvider(nil) must be a no-op")
	}

	// non-positive durations keep the defaults | 非正值保持默认
	c = New(WithLockerProvider(noop, WithLockRefreshInterval(0), WithLockAcquireTimeout(-time.Second)))
	if c.lockCfg.refreshInterval != defaultLockRefreshInterval ||
		c.lockCfg.acquireTimeout != defaultLockAcquireTimeout {
		t.Fatalf("defaults not kept: %+v", c.lockCfg)
	}

	// custom values are applied | 自定义值生效
	c = New(WithLockerProvider(noop, WithLockRefreshInterval(2*time.Second), WithLockAcquireTimeout(3*time.Second)))
	if c.lockCfg.refreshInterval != 2*time.Second || c.lockCfg.acquireTimeout != 3*time.Second {
		t.Fatalf("options not applied: %+v", c.lockCfg)
	}

	// the provider call gets a deadline context | provider 调用带有截止时间的 ctx
	var hasDeadline atomic.Bool
	provider := func(ctx context.Context, _ string) (taskcontract.Locker, error) {
		if _, ok := ctx.Deadline(); ok {
			hasDeadline.Store(true)
		}
		return nil, nil
	}
	c = New(WithLockerProvider(provider, WithLockAcquireTimeout(50*time.Millisecond)))
	c.MustAdd(fakeJobID, "* * * * *", func() {})
	c.runDue(time.Now())

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !hasDeadline.Load() {
		t.Fatal("the provider was called without a deadline context")
	}
	if c.lockCfg.refreshInterval != defaultLockRefreshInterval {
		t.Fatalf("refresh interval = %v, want default", c.lockCfg.refreshInterval)
	}
}

// TestDistributedConcurrentUse hammers lease acquisition, release, removal and
// refresher restarting; meant to be run with -race.
// TestDistributedConcurrentUse 并发压测租约获取、释放、移除与续期器重启；
// 应在 -race 下运行。
func TestDistributedConcurrentUse(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	provider := newFakeProviderSet(backend, "A")

	c := New(WithLockerProvider(
		provider.provider(),
		WithLockRefreshInterval(time.Millisecond),
		WithLockAcquireTimeout(50*time.Millisecond),
	))
	c.MustAdd("one", "* * * * *", func() {})
	c.MustAdd("two", "* * * * *", func() {})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	run := func(f func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
					f(i)
				}
			}
		}()
	}

	run(func(i int) { c.runDue(time.Date(2026, 1, 1, 0, i%60, 0, 0, time.UTC)) })
	run(func(i int) {
		if i%5 == 0 {
			c.Stop()
		} else {
			c.Start()
		}
	})
	run(func(i int) {
		if i%3 == 0 {
			c.MustAdd("two", "* * * * *", func() {})
		}
	})
	run(func(i int) {
		if i%7 == 0 {
			c.RemoveAll()
			c.MustAdd("one", "* * * * *", func() {})
			c.MustAdd("two", "* * * * *", func() {})
		}
	})

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if owner := backend.ownerOf("one"); owner != "" {
		t.Fatalf("lease not released: owner %q", owner)
	}
}

// gatedProvider blocks the first provider call until release is closed, so a
// test can interleave Remove/Stop with a fire that is inside the lease gate.
// gatedProvider 阻塞第一次 provider 调用直到 release 关闭，使测试能把
// Remove/Stop 插入正在租约闸门中的触发。
type gatedProvider struct {
	mu      sync.Mutex
	calls   int
	entered chan string
	release chan struct{}
	locker  *fakeLocker
}

func newGatedProvider(l *fakeLocker) *gatedProvider {
	return &gatedProvider{entered: make(chan string, 4), release: make(chan struct{}), locker: l}
}

func (p *gatedProvider) provider() LockerProvider {
	return func(_ context.Context, jobID string) (taskcontract.Locker, error) {
		p.mu.Lock()
		p.calls++
		first := p.calls == 1
		p.mu.Unlock()

		if first {
			p.entered <- jobID
			<-p.release
		}
		return p.locker, nil
	}
}

// TestDistributedRemoveSkipsLateFire covers a fire that is already inside the
// lease gate when Remove runs: it must not resurrect the lease state, Lock a
// second time, or run the job after Remove returned.
// TestDistributedRemoveSkipsLateFire 覆盖 Remove 时已处于租约闸门中的触发：它不得
// 复活租约状态、不能再次 Lock，也不能在 Remove 返回后运行任务。
func TestDistributedRemoveSkipsLateFire(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	locker := newFakeLocker(backend, "A", fakeJobID)
	gated := newGatedProvider(locker)

	c := New(WithLockerProvider(gated.provider()))

	var runs atomic.Int32
	c.MustAdd(fakeJobID, "* * * * *", func() { runs.Add(1) })

	c.runDue(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))

	select {
	case <-gated.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the fire never reached the lease gate")
	}

	// Remove tombstones the state, then its release waits for the fire's opMu.
	// Once the tombstone is visible, let the fire finish the gate: it must not
	// acquire, and must skip the run instead of resurrecting the forgotten
	// state (a Lock that was already in flight is released by the post-Lock
	// fence check).
	// Remove 先写墓碑，再由释放等待触发的 opMu。墓碑可见后放行触发完成闸门：
	// 它不得获取租约，并跳过运行，而不是复活已遗忘的状态（若 Lock 已经在途，
	// 由 Lock 之后的闸门检查释放）。
	removed := make(chan struct{})
	go func() {
		c.Remove(fakeJobID)
		close(removed)
	}()
	waitFor(t, "the Remove tombstone", func() bool {
		l := leaseOf(c, fakeJobID)
		if l == nil {
			return false
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.removed
	})
	close(gated.release)
	<-removed

	time.Sleep(50 * time.Millisecond)

	if got := runs.Load(); got != 0 {
		t.Fatalf("a fire dispatched before Remove ran the job %d times", got)
	}
	locks, _, unlocks := backend.counts()
	if locks != 0 || unlocks != 0 {
		t.Fatalf("stale fire touched the lease: %d locks, %d unlocks; want 0, 0", locks, unlocks)
	}
	if owner := backend.ownerOf(fakeJobID); owner != "" {
		t.Fatalf("stale fire kept the lease: owner %q", owner)
	}
	if l := leaseOf(c, fakeJobID); l != nil {
		t.Fatal("Remove left a resurrected lease state")
	}

	// Re-adding the id starts a fresh lifecycle and may acquire again.
	// 重新添加该 id 会开始新生命周期，可以再次获取租约。
	c.MustAdd(fakeJobID, "* * * * *", func() { runs.Add(1) })
	c.runDue(time.Date(2026, 6, 1, 10, 1, 0, 0, time.UTC))
	waitFor(t, "the run after re-adding", func() bool { return runs.Load() == 1 })
	if owner := backend.ownerOf(fakeJobID); owner != "A" {
		t.Fatalf("lease owner after re-adding = %q, want A", owner)
	}

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestDistributedStopSkipsLateFire covers a fire dispatched before Stop that
// has not reached the lease gate yet: it must not acquire, so a stopped Cron
// never keeps a lease (and its refresher) alive.
// TestDistributedStopSkipsLateFire 覆盖 Stop 之前已分发、但尚未到达租约闸门的
// 触发：它不得获取租约，因此停止后的 Cron 不会继续持有租约（及其续期器）。
func TestDistributedStopSkipsLateFire(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	locker := newFakeLocker(backend, "A", fakeJobID)
	gated := newGatedProvider(locker)

	c := New(WithLockerProvider(gated.provider(), WithLockRefreshInterval(time.Millisecond)))

	var runs atomic.Int32
	c.MustAdd(fakeJobID, "* * * * *", func() { runs.Add(1) })

	c.runDue(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))

	select {
	case <-gated.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the fire never reached the lease gate")
	}

	// Stop fences the Cron, then its release waits for the fire's opMu. Once
	// the fence is visible, let the fire finish the gate: it must not acquire,
	// so a stopped Cron never keeps a lease (or a refresher) alive. If Lock was
	// already in flight, the post-Lock fence check releases it.
	// Stop 先隔离 Cron，再由释放等待触发的 opMu。隔离可见后放行触发完成闸门：
	// 它不得获取租约，因此停止后的 Cron 不会持有租约（或续期器）；若 Lock 已经
	// 在途，由 Lock 之后的闸门检查负责释放。
	stopped := make(chan struct{})
	go func() {
		c.Stop()
		close(stopped)
	}()
	waitFor(t, "the Stop fence", func() bool {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.stopped
	})
	close(gated.release)
	<-stopped

	time.Sleep(50 * time.Millisecond)

	if got := runs.Load(); got != 0 {
		t.Fatalf("a fire dispatched before Stop ran the job %d times", got)
	}
	locks, refreshes, unlocks := backend.counts()
	if locks != 0 || unlocks != 0 || refreshes != 0 {
		t.Fatalf("stopped lease calls: %d locks, %d unlocks, %d refreshes; want 0, 0, 0", locks, unlocks, refreshes)
	}
	if owner := backend.ownerOf(fakeJobID); owner != "" {
		t.Fatalf("a stopped Cron still owns the lease: owner %q", owner)
	}

	// Start clears the fence: the next due fire acquires normally.
	// Start 清除隔离：下次到期触发正常获取。
	c.Start()
	defer func() {
		if err := c.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()
	c.runDue(time.Date(2026, 6, 1, 10, 1, 0, 0, time.UTC))
	waitFor(t, "the run after Start", func() bool { return runs.Load() == 1 })
	if owner := backend.ownerOf(fakeJobID); owner != "A" {
		t.Fatalf("lease owner after Start = %q, want A", owner)
	}
}

// TestDistributedAddReplacementSkipsOldFire covers Add replacing a job while a
// fire for the old version waits in the lease gate: the stale fire is skipped
// (its run func is the old one) and the new version runs on the next fire.
// TestDistributedAddReplacementSkipsOldFire 覆盖 Add 在旧版本的触发等待租约闸门时
// 替换任务：旧触发被跳过（它的运行函数属于旧版本），新版本在下次触发运行。
func TestDistributedAddReplacementSkipsOldFire(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	locker := newFakeLocker(backend, "A", fakeJobID)
	gated := newGatedProvider(locker)

	c := New(WithLockerProvider(gated.provider()))

	var oldRuns, newRuns atomic.Int32
	c.MustAdd(fakeJobID, "* * * * *", func() { oldRuns.Add(1) })

	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	c.runDue(base)

	select {
	case <-gated.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the fire never reached the lease gate")
	}

	c.MustAdd(fakeJobID, "* * * * *", func() { newRuns.Add(1) })
	close(gated.release)

	// The stale fire finishes the gate, keeps the lease for the id but drops
	// its run because the version was replaced.
	// 旧触发完成闸门，保留该 id 的租约，但因版本已被替换而放弃运行。
	waitFor(t, "the stale fire to finish the gate", func() bool {
		return backend.ownerOf(fakeJobID) == "A"
	})
	time.Sleep(50 * time.Millisecond)

	if got := oldRuns.Load(); got != 0 {
		t.Fatalf("the replaced job's stale fire ran the old func %d times", got)
	}

	c.runDue(base.Add(time.Minute))
	waitFor(t, "the new version to run", func() bool { return newRuns.Load() == 1 })

	// The lease state survived the replacement: no second Lock was needed.
	// 租约状态在替换中保留：不需要第二次 Lock。
	if locks, _, _ := backend.counts(); locks != 1 {
		t.Fatalf("%d Lock calls, want 1 (Add replacement keeps leadership)", locks)
	}

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestDistributedStopStartEpochSkipsInheritedFire covers a fire that is
// waiting in the lease gate across a Stop/Start cycle: the restarted Cron has
// a new stop epoch, so the old fire must neither run the job nor acquire a
// lease (which the restarted Cron would keep refreshing as a zombie leader).
// TestDistributedStopStartEpochSkipsInheritedFire 覆盖跨越 Stop/Start 仍停留在
// 租约闸门的旧触发：重启后的 Cron 拥有新的 stop epoch，旧触发既不得运行任务，
// 也不得获取租约（否则重启后的 Cron 会把僵尸 leader 的租约持续续期）。
func TestDistributedStopStartEpochSkipsInheritedFire(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	locker := newFakeLocker(backend, "A", fakeJobID)
	gated := newGatedProvider(locker)

	c := New(WithLockerProvider(gated.provider()))

	var runs atomic.Int32
	c.MustAdd(fakeJobID, "* * * * *", func() { runs.Add(1) })

	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	c.runDue(base)

	select {
	case <-gated.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the fire never reached the lease gate")
	}

	c.mu.RLock()
	epochBefore := c.epoch
	c.mu.RUnlock()

	stopped := make(chan struct{})
	go func() {
		c.Stop()
		close(stopped)
	}()
	waitFor(t, "the Stop epoch fence", func() bool {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.epoch != epochBefore
	})

	// Start while Stop is still waiting for the fire's opMu: the restarted
	// Cron opens the gate for new fires, but the old epoch stays fenced.
	// 在 Stop 仍等待触发 opMu 时 Start：重启后的 Cron 为新触发开门，但旧
	// epoch 仍被隔离。
	c.Start()
	defer func() {
		if err := c.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	// Let the inherited fire finish the gate after the new epoch is in place.
	// 在新 epoch 生效后放行旧触发完成闸门。
	close(gated.release)
	<-stopped

	if got := runs.Load(); got != 0 {
		t.Fatalf("a fire inherited across Stop/Start ran the job %d times", got)
	}
	if locks, _, unlocks := backend.counts(); locks != 0 || unlocks != 0 {
		t.Fatalf("inherited fire touched the lease: %d locks, %d unlocks; want 0, 0", locks, unlocks)
	}
	if owner := backend.ownerOf(fakeJobID); owner != "" {
		t.Fatalf("the restarted Cron inherited a lease: owner %q", owner)
	}

	// A fresh fire in the new epoch acquires normally.
	// 新 epoch 的触发正常获取租约。
	c.runDue(base.Add(time.Minute))
	waitFor(t, "the new-epoch fire to run", func() bool { return runs.Load() == 1 })
	if owner := backend.ownerOf(fakeJobID); owner != "A" {
		t.Fatalf("lease owner after the new-epoch fire = %q, want A", owner)
	}
}

// TestDistributedStopAbortsSlowRefresh covers Stop interrupting an in-flight
// Refresh: the refresher must exit promptly instead of waiting for the acquire
// timeout, and the aborted refresh must not be reported as LockLost.
// TestDistributedStopAbortsSlowRefresh 覆盖 Stop 中止在途的 Refresh：续期器必须
// 立即退出而不是等获取超时，且被中止的续期不得上报为 LockLost。
func TestDistributedStopAbortsSlowRefresh(t *testing.T) {
	t.Parallel()

	backend := newFakeBackend()
	locker := newFakeLocker(backend, "A", fakeJobID)
	locker.refreshEntered = make(chan struct{}, 1)
	locker.blockRefresh.Store(true)

	events := newLockEventRecorder()
	c := New(WithLockerProvider(
		fixedProvider(locker),
		WithLockRefreshInterval(time.Millisecond),
		WithLockEventHandler(events.handler()),
	))

	var runs atomic.Int32
	c.MustAdd(fakeJobID, "* * * * *", func() { runs.Add(1) })
	c.runDue(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))

	select {
	case <-locker.refreshEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("the refresher never entered Refresh")
	}

	stopped := make(chan struct{})
	go func() {
		c.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop waited for the slow Refresh instead of aborting it")
	}

	if !leaseOf(c, fakeJobID).refresherStopped() {
		t.Fatal("the refresher is still running after Stop")
	}
	if owner := backend.ownerOf(fakeJobID); owner != "" {
		t.Fatalf("Stop did not release the lease: owner %q", owner)
	}
	events.none(t)
}
