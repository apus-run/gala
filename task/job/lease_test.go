package job

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	taskcontract "github.com/apus-run/gala/task"
)

// fakeLocker is a controllable taskcontract.Locker for lease tests.
// fakeLocker 是租约测试用的可控 taskcontract.Locker。
type fakeLocker struct {
	lockErr   error
	unlockErr error
	refreshEr error
	hold      chan struct{} // when non-nil, Lock blocks until it is closed or ctx ends | 非 nil 时 Lock 阻塞直到关闭或 ctx 结束

	refreshed atomic.Int32
	unlocks   atomic.Int32
	lockCalls atomic.Int32

	// refreshedCh receives a value on every successful refresh.
	// refreshedCh 每次成功续期都会收到一个值。
	refreshedCh chan struct{}
}

func (f *fakeLocker) Lock(ctx context.Context) error {
	f.lockCalls.Add(1)
	if f.hold != nil {
		select {
		case <-f.hold:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.lockErr
}

func (f *fakeLocker) Unlock(ctx context.Context) error {
	f.unlocks.Add(1)
	return f.unlockErr
}

func (f *fakeLocker) Refresh(ctx context.Context) error {
	if f.refreshEr != nil {
		return f.refreshEr
	}
	f.refreshed.Add(1)
	if f.refreshedCh != nil {
		select {
		case f.refreshedCh <- struct{}{}:
		default:
		}
	}
	return nil
}

// fixedProvider returns the same Locker for every Run, which is exactly the
// shape the provider contract forbids for concurrent Runs; tests that do not
// exercise concurrency use it to keep assertions simple.
// fixedProvider 对每次 Run 返回同一个 Locker——这正是 provider 契约在并发 Run
// 中禁止的形态；不做并发断言的测试用它来简化断言。
func fixedProvider(l taskcontract.Locker) LockerProvider {
	return func(context.Context, string) (taskcontract.Locker, error) {
		return l, nil
	}
}

func TestLeaseBusyLockerSkipsJob(t *testing.T) {
	locked := errors.New("backend: locked")
	locker := &fakeLocker{lockErr: locked}

	var ran atomic.Bool
	r := NewRegistry(WithLockerProvider(fixedProvider(locker)))
	if err := r.Register("export", func(context.Context) error {
		ran.Store(true)
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	err := r.Run(context.Background(), Invocation{Name: "export"})
	if err == nil {
		t.Fatal("expected an error when the lease is taken")
	}
	if !errors.Is(err, locked) {
		t.Fatalf("got %v, want a wrapped %v", err, locked)
	}
	if ran.Load() {
		t.Fatal("job ran without the lease")
	}
	if got := locker.unlocks.Load(); got != 0 {
		t.Fatalf("Unlock called %d times after a failed Lock, want 0", got)
	}
}

func TestLeaseAcquireTimeout(t *testing.T) {
	locker := &fakeLocker{hold: make(chan struct{})} // never released within the test | 测试内不会释放
	defer close(locker.hold)

	r := NewRegistry(WithLockerProvider(fixedProvider(locker), WithLockAcquireTimeout(10*time.Millisecond)))
	if err := r.Register("slow", func(context.Context) error {
		t.Fatal("job must not run")
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	start := time.Now()
	err := r.Run(context.Background(), Invocation{Name: "slow"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("acquire waited %s, want roughly the 10ms timeout", elapsed)
	}
}

func TestLeaseRefreshesDuringLongJob(t *testing.T) {
	locker := &fakeLocker{refreshedCh: make(chan struct{}, 1)}

	r := NewRegistry(WithLockerProvider(fixedProvider(locker), WithLockRefreshInterval(5*time.Millisecond)))
	refreshed := make(chan struct{})
	if err := r.Register("long", func(ctx context.Context) error {
		select {
		case <-refreshed:
			return nil
		case <-time.After(2 * time.Second):
			return errors.New("no refresh observed")
		}
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- r.Run(context.Background(), Invocation{Name: "long"}) }()

	select {
	case <-locker.refreshedCh:
		close(refreshed)
	case <-time.After(2 * time.Second):
		t.Fatal("no refresh while the job was running")
	}

	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := locker.refreshed.Load(); got < 1 {
		t.Fatalf("refreshed %d times, want at least 1", got)
	}
}

func TestLeaseRefreshFailureCancelsJob(t *testing.T) {
	refreshErr := errors.New("lease lost")
	locker := &fakeLocker{refreshEr: refreshErr}

	r := NewRegistry(WithLockerProvider(fixedProvider(locker), WithLockRefreshInterval(5*time.Millisecond)))
	ctxCanceled := make(chan struct{})
	if err := r.Register("watcher", func(ctx context.Context) error {
		<-ctx.Done()
		close(ctxCanceled)
		return ctx.Err()
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- r.Run(context.Background(), Invocation{Name: "watcher"}) }()

	select {
	case <-ctxCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh failure did not cancel the job context")
	}

	err := <-done
	if !errors.Is(err, refreshErr) {
		t.Fatalf("got %v, want the refresh error %v", err, refreshErr)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want the job's context error joined in", err)
	}
	if got := locker.unlocks.Load(); got != 1 {
		t.Fatalf("Unlock called %d times, want 1", got)
	}
}

func TestLeaseUnlockAfterRun(t *testing.T) {
	locker := &fakeLocker{}

	var jobSawUnlock atomic.Bool
	r := NewRegistry(WithLockerProvider(fixedProvider(locker)))
	if err := r.Register("quick", func(context.Context) error {
		jobSawUnlock.Store(locker.unlocks.Load() > 0)
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := r.Run(context.Background(), Invocation{Name: "quick"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if jobSawUnlock.Load() {
		t.Fatal("lease released while the job was still running")
	}
	if got := locker.lockCalls.Load(); got != 1 {
		t.Fatalf("Lock called %d times, want 1", got)
	}
	if got := locker.unlocks.Load(); got != 1 {
		t.Fatalf("Unlock called %d times, want 1", got)
	}
}

func TestLeaseJoinsJobAndUnlockErrors(t *testing.T) {
	jobErr := errors.New("job failed")
	unlockErr := errors.New("unlock failed")
	locker := &fakeLocker{unlockErr: unlockErr}

	r := NewRegistry(WithLockerProvider(fixedProvider(locker)))
	if err := r.Register("failing", func(context.Context) error { return jobErr }); err != nil {
		t.Fatalf("register: %v", err)
	}

	err := r.Run(context.Background(), Invocation{Name: "failing"})
	if !errors.Is(err, jobErr) {
		t.Fatalf("job error hidden: %v", err)
	}
	if !errors.Is(err, unlockErr) {
		t.Fatalf("unlock error hidden: %v", err)
	}
}

func TestLeaseUnlockUsesFreshContext(t *testing.T) {
	locker := &unlockCtxLocker{}
	r := NewRegistry(WithLockerProvider(fixedProvider(locker)))
	if err := r.Register("quick", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("register: %v", err)
	}

	// A canceled caller ctx must not prevent the lease release.
	// 已取消的调用方 ctx 不能阻止租约释放。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := r.Run(ctx, Invocation{Name: "quick"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := locker.unlockCtxErr(); err != nil {
		t.Fatalf("Unlock got a canceled ctx: %v", err)
	}
}

// unlockCtxLocker records the state of the context passed to Unlock.
// unlockCtxLocker 记录传给 Unlock 的 context 状态。
type unlockCtxLocker struct {
	mu  sync.Mutex
	err error
}

func (l *unlockCtxLocker) Lock(context.Context) error    { return nil }
func (l *unlockCtxLocker) Refresh(context.Context) error { return nil }
func (l *unlockCtxLocker) Unlock(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.err = ctx.Err()
	return nil
}
func (l *unlockCtxLocker) unlockCtxErr() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

func TestNoGoroutineLeakAfterRun(t *testing.T) {
	locker := &fakeLocker{}
	r := NewRegistry(WithLockerProvider(fixedProvider(locker), WithLockRefreshInterval(time.Millisecond)))
	if err := r.Register("quick", func(context.Context) error {
		time.Sleep(5 * time.Millisecond) // long enough for a refresh to fire | 足够触发一次续期
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	before := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		if err := r.Run(context.Background(), Invocation{Name: "quick"}); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines grew from %d to %d after Run returned", before, after)
	}
}

func TestNoLockerRunsWithCallerContext(t *testing.T) {
	r := NewRegistry()

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "value")
	if err := r.Register("plain", func(jobCtx context.Context) error {
		if jobCtx.Value(ctxKey{}) != "value" {
			return errors.New("caller context not passed through")
		}
		if _, ok := FromContext(jobCtx); !ok {
			return errors.New("invocation missing")
		}
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := r.Run(ctx, Invocation{Name: "plain"}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestLockOptionsKeepDefaults(t *testing.T) {
	noop := func(context.Context, string) (taskcontract.Locker, error) { return nil, nil }

	c := NewRegistry(WithLockerProvider(noop, WithLockRefreshInterval(0), WithLockAcquireTimeout(-time.Second)))

	if c.lock.refreshInterval != defaultLockRefreshInterval {
		t.Fatalf("refresh interval %s, want default %s", c.lock.refreshInterval, defaultLockRefreshInterval)
	}
	if c.lock.acquireTimeout != defaultLockAcquireTimeout {
		t.Fatalf("acquire timeout %s, want default %s", c.lock.acquireTimeout, defaultLockAcquireTimeout)
	}
	if c.lockProvider == nil {
		t.Fatal("provider was not stored")
	}

	// A nil provider is a no-op: the current configuration is kept.
	// nil provider 是空操作：保持当前配置。
	d := NewRegistry(WithLockerProvider(nil))
	if d.lockProvider != nil {
		t.Fatal("WithLockerProvider(nil) installed a provider")
	}
	if d.lock.refreshInterval != defaultLockRefreshInterval || d.lock.acquireTimeout != defaultLockAcquireTimeout {
		t.Fatalf("WithLockerProvider(nil) changed the timings: %+v", d.lock)
	}
}

// TestLeaseNilProviderRunsLocally covers a provider returning (nil, nil): the
// Run executes without lease I/O.
// TestLeaseNilProviderRunsLocally 覆盖 provider 返回 (nil, nil)：本次 Run 不进行
// 任何租约 I/O 直接执行。
func TestLeaseNilProviderRunsLocally(t *testing.T) {
	var ran atomic.Bool
	r := NewRegistry(WithLockerProvider(func(context.Context, string) (taskcontract.Locker, error) {
		return nil, nil
	}))
	if err := r.Register("local", func(context.Context) error {
		ran.Store(true)
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if err := r.Run(context.Background(), Invocation{Name: "local"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !ran.Load() {
		t.Fatal("job did not run for a (nil, nil) provider")
	}
}

// TestLeaseProviderErrorSkipsJob covers a provider error: Run fails without
// running the job and the error wraps the provider error.
// TestLeaseProviderErrorSkipsJob 覆盖 provider 错误：Run 失败、任务不运行，且错误
// 包装 provider 错误。
func TestLeaseProviderErrorSkipsJob(t *testing.T) {
	providerErr := errors.New("backend down")
	var ran atomic.Bool
	r := NewRegistry(WithLockerProvider(func(context.Context, string) (taskcontract.Locker, error) {
		return nil, providerErr
	}))
	if err := r.Register("export", func(context.Context) error {
		ran.Store(true)
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	err := r.Run(context.Background(), Invocation{Name: "export"})
	if !errors.Is(err, providerErr) {
		t.Fatalf("got %v, want a wrapped %v", err, providerErr)
	}
	if ran.Load() {
		t.Fatal("job ran although the provider failed")
	}
}

// TestLeaseEachRunGetsOwnLocker covers the provider being called once per Run,
// so concurrent Runs can carry their own tokens and the first Unlock does not
// release the lease of a Run still in flight.
// TestLeaseEachRunGetsOwnLocker 覆盖每次 Run 都调用 provider：并发 Run 因此各有
// 自己的令牌，第一个 Unlock 不会释放仍在运行的 Run 的租约。
func TestLeaseEachRunGetsOwnLocker(t *testing.T) {
	var calls atomic.Int32
	r := NewRegistry(WithLockerProvider(func(context.Context, string) (taskcontract.Locker, error) {
		calls.Add(1)
		return &fakeLocker{}, nil
	}))
	if err := r.Register("quick", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("register: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := r.Run(context.Background(), Invocation{Name: "quick"}); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("provider called %d times, want 3 (once per Run)", got)
	}
}

// errLeaseHeld is the backend's "another owner holds the key" sentinel.
// errLeaseHeld 是后端"键被其他持有者占用"的哨兵错误。
var errLeaseHeld = errors.New("lease held by another owner")

// tokenBackend models dlock's ownership rule: every Locker gets a random
// token; Lock succeeds when the key is free, extends when the token matches
// and fails with errLeaseHeld for a different token; Unlock only deletes its
// own token.
// tokenBackend 模拟 dlock 的所有权规则：每个 Locker 获得随机令牌；键空闲时 Lock
// 成功，令牌匹配时视为续期，令牌不同时返回 errLeaseHeld；Unlock 只删除自己的令牌。
type tokenBackend struct {
	mu     sync.Mutex
	owner  map[string]string
	tokens int
}

func newTokenBackend() *tokenBackend {
	return &tokenBackend{owner: map[string]string{}}
}

// locker returns a Locker with its own fresh token for key.
// locker 返回 key 上一个带全新令牌的 Locker。
func (b *tokenBackend) locker(key string) *tokenLocker {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.tokens++
	return &tokenLocker{backend: b, key: key, token: fmt.Sprintf("token-%d", b.tokens)}
}

func (b *tokenBackend) ownerOf(key string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.owner[key]
}

type tokenLocker struct {
	backend *tokenBackend
	key     string
	token   string
}

func (l *tokenLocker) Lock(context.Context) error {
	l.backend.mu.Lock()
	defer l.backend.mu.Unlock()

	if cur, held := l.backend.owner[l.key]; held && cur != l.token {
		return errLeaseHeld
	}
	l.backend.owner[l.key] = l.token
	return nil
}

func (l *tokenLocker) Unlock(context.Context) error {
	l.backend.mu.Lock()
	defer l.backend.mu.Unlock()

	if cur, held := l.backend.owner[l.key]; !held || cur != l.token {
		return fmt.Errorf("lease %q not held", l.key)
	}
	delete(l.backend.owner, l.key)
	return nil
}

func (l *tokenLocker) Refresh(context.Context) error {
	l.backend.mu.Lock()
	defer l.backend.mu.Unlock()

	if cur, held := l.backend.owner[l.key]; !held || cur != l.token {
		return fmt.Errorf("lease %q not held", l.key)
	}
	return nil
}

// TestLeasePerRunTokenSerializes covers the contract the per-Run provider
// exists for: with a fresh token per Run, overlapping triggers of one job are
// excluded by the backend and the first Unlock cannot affect the second Run.
// TestLeasePerRunTokenSerializes 覆盖按 Run 调用 provider 的契约：每次 Run 使用新
// 令牌时，同一任务的重叠触发被后端互斥，第一个 Unlock 也不会影响第二个 Run。
func TestLeasePerRunTokenSerializes(t *testing.T) {
	backend := newTokenBackend()
	r := NewRegistry(WithLockerProvider(func(_ context.Context, name string) (taskcontract.Locker, error) {
		return backend.locker("job:" + name), nil
	}))

	started := make(chan struct{})
	release := make(chan struct{})
	if err := r.Register("export", func(context.Context) error {
		close(started)
		<-release
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx := context.Background()
	first := make(chan error, 1)
	go func() { first <- r.Run(ctx, Invocation{Name: "export"}) }()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the first Run did not start")
	}

	// The second trigger must be refused while the first holds the lease.
	// 第一个 Run 持有租约期间，第二次触发必须被拒绝。
	err := r.Run(ctx, Invocation{Name: "export"})
	if !errors.Is(err, errLeaseHeld) {
		t.Fatalf("second Run = %v, want a wrapped %v", err, errLeaseHeld)
	}

	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if owner := backend.ownerOf("job:export"); owner != "" {
		t.Fatalf("lease not released after Run: owner %q", owner)
	}
}

// TestLeasePerJobKeyIndependence covers the provider choosing one key per job
// name: two different names never exclude each other.
// TestLeasePerJobKeyIndependence 覆盖 provider 按任务名选择锁键：不同任务名互不
// 排斥。
func TestLeasePerJobKeyIndependence(t *testing.T) {
	backend := newTokenBackend()
	r := NewRegistry(WithLockerProvider(func(_ context.Context, name string) (taskcontract.Locker, error) {
		return backend.locker("job:" + name), nil
	}))

	started := make(chan string, 2)
	release := make(chan struct{})
	for _, name := range []string{"resize", "export"} {
		name := name
		if err := r.Register(name, func(context.Context) error {
			started <- name
			<-release
			return nil
		}); err != nil {
			t.Fatalf("register %q: %v", name, err)
		}
	}

	errs := make(chan error, 2)
	for _, name := range []string{"resize", "export"} {
		name := name
		go func() { errs <- r.Run(context.Background(), Invocation{Name: name}) }()
	}

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case name := <-started:
			seen[name] = true
		case <-time.After(2 * time.Second):
			t.Fatal("different job names blocked each other")
		}
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	if !seen["resize"] || !seen["export"] {
		t.Fatalf("jobs that ran: %v", seen)
	}
	for _, name := range []string{"resize", "export"} {
		if owner := backend.ownerOf("job:" + name); owner != "" {
			t.Fatalf("lease %q not released: owner %q", name, owner)
		}
	}
}
