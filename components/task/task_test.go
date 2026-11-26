package gala

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apus-run/gala/server"
)

var _ server.Server = (*fakeServer)(nil)

// eventLog records lifecycle events in append order | eventLog 按追加顺序记录生命周期事件.
type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) add(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *eventLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

func (l *eventLog) index(event string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, e := range l.events {
		if e == event {
			return i
		}
	}
	return -1
}

// fakeServer is a minimal server.Server that blocks in Start until Stop is
// called, so lifecycle ordering can be observed without opening a port.
// fakeServer 是极简的 server.Server：Start 阻塞至 Stop 被调用，无需监听端口
// 即可观察生命周期顺序。
type fakeServer struct {
	events   *eventLog
	started  chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
}

func newFakeServer(events *eventLog) *fakeServer {
	return &fakeServer{
		events:  events,
		started: make(chan struct{}),
		stopCh:  make(chan struct{}),
	}
}

func (s *fakeServer) Start(context.Context) error {
	s.events.add("server:start")
	close(s.started)
	<-s.stopCh
	return nil
}

func (s *fakeServer) Stop(context.Context) error {
	s.events.add("server:stop")
	s.stopOnce.Do(func() { close(s.stopCh) })
	return nil
}

func (s *fakeServer) Health() bool { return true }

// fakeTask records Start/Stop calls, can fail either step, and can signal when
// its Start has been invoked.
// fakeTask 记录 Start/Stop 调用，可使任一步骤失败，并可发出 Start 已调用的信号。
type fakeTask struct {
	name      string
	events    *eventLog
	startErr  error
	stopErr   error
	started   chan struct{}
	startOnce sync.Once
	stopCalls atomic.Int32
}

func (t *fakeTask) Start(context.Context) error {
	t.events.add("task:" + t.name + ":start")
	t.startOnce.Do(func() {
		if t.started != nil {
			close(t.started)
		}
	})
	return t.startErr
}

func (t *fakeTask) Stop(context.Context) error {
	t.stopCalls.Add(1)
	t.events.add("task:" + t.name + ":stop")
	return t.stopErr
}

// lifecycleTask records Start/Stop entry and detects a Start that happens
// after the task has already been stopped.
// lifecycleTask 记录 Start/Stop 的进入，并检测任务被停止后才发生的 Start。
type lifecycleTask struct {
	name      string
	events    *eventLog
	startGate chan struct{} // when non-nil, Start blocks until it is closed
	startedCh chan struct{} // closed when Start is entered
	stopDone  chan struct{} // closed when Stop is entered

	startedOnce sync.Once
	stopOnce    sync.Once

	mu             sync.Mutex
	started        bool
	stopped        bool
	startAfterStop bool
}

func newLifecycleTask(name string, events *eventLog, startGate chan struct{}) *lifecycleTask {
	return &lifecycleTask{
		name:      name,
		events:    events,
		startGate: startGate,
		startedCh: make(chan struct{}),
		stopDone:  make(chan struct{}),
	}
}

func (t *lifecycleTask) Start(context.Context) error {
	t.events.add(t.name + ":start")
	t.mu.Lock()
	if t.stopped {
		t.startAfterStop = true
	}
	t.started = true
	t.mu.Unlock()
	t.startedOnce.Do(func() { close(t.startedCh) })

	if t.startGate != nil {
		<-t.startGate
	}

	return nil
}

func (t *lifecycleTask) Stop(context.Context) error {
	t.events.add(t.name + ":stop")
	t.mu.Lock()
	t.stopped = true
	t.mu.Unlock()
	t.stopOnce.Do(func() { close(t.stopDone) })

	return nil
}

func (t *lifecycleTask) state() (started, stopped, startAfterStop bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.started, t.stopped, t.startAfterStop
}

// stopObservation is the context state a stopProbeTask saw when its Stop was
// entered.
// stopObservation 是 stopProbeTask 进入 Stop 时观察到的 context 状态.
type stopObservation struct {
	ctxErr         error
	hadDeadline    bool
	deadlineRemain time.Duration
	hadService     bool
}

// stopProbeTask is a Task whose Stop can block until its context expires,
// record the context state it observed, and return a configured error. The
// shutdown tests use it to assert per-task budgets and cleanup-context
// semantics.
// stopProbeTask 的 Stop 可阻塞至其 context 过期、记录观察到的 context 状态并返回
// 预设错误。停机测试用它断言每个任务的预算与清理 context 的语义。
type stopProbeTask struct {
	name      string
	events    *eventLog
	started   chan struct{}
	blockStop bool
	stopErr   error

	startOnce sync.Once
	stopCalls atomic.Int32

	mu  sync.Mutex
	obs stopObservation
}

func newStopProbeTask(name string, events *eventLog) *stopProbeTask {
	return &stopProbeTask{
		name:    name,
		events:  events,
		started: make(chan struct{}),
	}
}

func (t *stopProbeTask) Start(context.Context) error {
	t.events.add("task:" + t.name + ":start")
	t.startOnce.Do(func() { close(t.started) })

	return nil
}

func (t *stopProbeTask) Stop(ctx context.Context) error {
	t.stopCalls.Add(1)
	t.events.add("task:" + t.name + ":stop")

	deadline, hasDeadline := ctx.Deadline()
	_, hasService := ServiceContextKey.FromContext(ctx)
	obs := stopObservation{
		ctxErr:      ctx.Err(),
		hadDeadline: hasDeadline,
		hadService:  hasService,
	}
	if hasDeadline {
		obs.deadlineRemain = time.Until(deadline)
	}
	t.mu.Lock()
	t.obs = obs
	t.mu.Unlock()

	if t.blockStop {
		<-ctx.Done()
		return ctx.Err()
	}

	return t.stopErr
}

func (t *stopProbeTask) observed() stopObservation {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.obs
}

func waitForSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func waitRun(t *testing.T, runErr <-chan error) error {
	t.Helper()
	select {
	case err := <-runErr:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

func assertEventOrder(t *testing.T, events *eventLog, want ...string) {
	t.Helper()
	prev := -1
	for _, event := range want {
		idx := events.index(event)
		if idx < 0 {
			t.Fatalf("event %q not recorded, got %v", event, events.snapshot())
		}
		if idx <= prev {
			t.Fatalf("event %q out of order, got %v", event, events.snapshot())
		}
		prev = idx
	}
}

// TestTask_LifecycleOrder verifies that tasks start after the transport server
// and afterStart hooks, and stop before the transport server.
// TestTask_LifecycleOrder 验证任务在传输服务器与 afterStart 钩子之后启动，并在
// 传输服务器之前停止。
func TestTask_LifecycleOrder(t *testing.T) {
	events := &eventLog{}
	srv := newFakeServer(events)
	first := &fakeTask{name: "first", events: events, started: make(chan struct{})}
	second := &fakeTask{name: "second", events: events, started: make(chan struct{})}

	app := New(
		WithServers(srv),
		AfterStart(func(_ context.Context) error {
			select {
			case <-srv.started:
			case <-time.After(5 * time.Second):
				return errors.New("server did not start before afterStart hook")
			}
			events.add("afterStart")
			return nil
		}),
		WithTasks(first, second),
	)

	runErr := make(chan error, 1)
	go func() { runErr <- app.Run() }()

	waitForSignal(t, second.started, "second task to start")
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := waitRun(t, runErr); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	assertEventOrder(t, events,
		"server:start",
		"afterStart",
		"task:first:start",
		"task:second:start",
		"task:first:stop",
		"task:second:stop",
		"server:stop",
	)
}

// TestTask_StopOrder verifies that multiple tasks stop sequentially in option
// order.
// TestTask_StopOrder 验证多个任务按选项顺序依次停止。
func TestTask_StopOrder(t *testing.T) {
	events := &eventLog{}
	tasks := []*fakeTask{
		{name: "one", events: events, started: make(chan struct{})},
		{name: "two", events: events, started: make(chan struct{})},
		{name: "three", events: events, started: make(chan struct{})},
	}

	app := New(WithTasks(tasks[0], tasks[1], tasks[2]))

	runErr := make(chan error, 1)
	go func() { runErr <- app.Run() }()

	waitForSignal(t, tasks[2].started, "third task to start")
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := waitRun(t, runErr); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	assertEventOrder(t, events,
		"task:one:start",
		"task:two:start",
		"task:three:start",
		"task:one:stop",
		"task:two:stop",
		"task:three:stop",
	)
}

// TestTask_StartError verifies that a Task.Start error is returned by Run and
// still triggers shutdown of the remaining tasks and servers.
// TestTask_StartError 验证 Task.Start 的错误由 Run 返回，且仍会触发其余任务与
// 服务器的停机。
func TestTask_StartError(t *testing.T) {
	events := &eventLog{}
	srv := newFakeServer(events)
	wantErr := errors.New("task start failed")
	okTask := &fakeTask{name: "ok", events: events}
	badTask := &fakeTask{name: "bad", events: events, startErr: wantErr}

	app := New(WithServers(srv), WithTasks(okTask, badTask))

	runErr := make(chan error, 1)
	go func() { runErr <- app.Run() }()

	if err := waitRun(t, runErr); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if got := okTask.stopCalls.Load(); got != 1 {
		t.Fatalf("ok task Stop calls = %d, want 1", got)
	}
	if got := badTask.stopCalls.Load(); got != 1 {
		t.Fatalf("bad task Stop calls = %d, want 1", got)
	}
	if events.index("server:stop") < 0 {
		t.Fatalf("server not stopped after task start failure, got %v", events.snapshot())
	}
}

// TestTask_StopError verifies that a Task.Stop error is returned by Run.
// TestTask_StopError 验证 Task.Stop 的错误由 Run 返回。
func TestTask_StopError(t *testing.T) {
	events := &eventLog{}
	wantErr := errors.New("task stop failed")
	task := &fakeTask{
		name:    "failing",
		events:  events,
		started: make(chan struct{}),
		stopErr: wantErr,
	}

	app := New(WithTasks(task))

	runErr := make(chan error, 1)
	go func() { runErr <- app.Run() }()

	waitForSignal(t, task.started, "task to start")
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := waitRun(t, runErr); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
}

// TestTask_StopDuringStart verifies that a shutdown requested while a task is
// still starting cannot produce a Stop-before-Start pair or a start that lands
// after a stop: every task Start is followed by exactly one Stop.
// TestTask_StopDuringStart 验证在任务仍在启动时请求停机，不会产生
// Stop 先于 Start，或 Stop 之后又启动的情况：每个任务的 Start 之后都恰好有一次 Stop。
func TestTask_StopDuringStart(t *testing.T) {
	events := &eventLog{}
	gate := make(chan struct{})
	blocked := newLifecycleTask("blocked", events, gate)
	second := newLifecycleTask("second", events, nil)

	app := New(WithTasks(blocked, second))

	runErr := make(chan error, 1)
	go func() { runErr <- app.Run() }()

	waitForSignal(t, blocked.startedCh, "first task to enter Start")
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Give an implementation that stops tasks independently of the start pass
	// the chance to stop the second task while the first Start is still
	// blocked. The corrected implementation cannot: Start and Stop share one
	// goroutine, so this select waits out the grace period.
	// 给"启动与停止由不同 goroutine 执行"的实现一个机会：在第一个 Start 仍阻塞时
	// 先停止第二个任务。修正后的实现不会如此——Start 与 Stop 共用一个 goroutine，
	// 因此这里会等满宽限期。
	select {
	case <-second.stopDone:
	case <-time.After(100 * time.Millisecond):
	}
	close(gate)

	if err := waitRun(t, runErr); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	started, stopped, startAfterStop := blocked.state()
	if !started || !stopped {
		t.Fatalf("blocked task started=%v stopped=%v, want both true", started, stopped)
	}
	if startAfterStop {
		t.Fatal("blocked task was started after it had been stopped")
	}

	secondStarted, secondStopped, secondStartAfterStop := second.state()
	if !secondStopped || secondStartAfterStop {
		t.Fatalf("second task started=%v stopped=%v startAfterStop=%v, want stopped without start-after-stop; events = %v",
			secondStarted, secondStopped, secondStartAfterStop, events.snapshot())
	}
	if idxStart, idxStop := events.index("second:start"), events.index("second:stop"); idxStart >= 0 && idxStart > idxStop {
		t.Fatalf("second task events out of order, got %v", events.snapshot())
	}
}

// TestTask_NoTasks verifies that an app without tasks keeps the plain
// server-only lifecycle.
// TestTask_NoTasks 验证未配置任务的应用保持纯服务器生命周期。
func TestTask_NoTasks(t *testing.T) {
	events := &eventLog{}
	srv := newFakeServer(events)

	app := New(WithServers(srv))

	runErr := make(chan error, 1)
	go func() { runErr <- app.Run() }()

	waitForSignal(t, srv.started, "server to start")
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := waitRun(t, runErr); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	assertEventOrder(t, events, "server:start", "server:stop")
}

// TestTask_ServiceStopStopsOnce verifies that Service.Stop triggers each task's
// Stop exactly once, even when Stop is called repeatedly.
// TestTask_ServiceStopStopsOnce 验证 Service.Stop 恰好触发每个任务的 Stop 一次，
// 重复调用 Stop 亦然。
func TestTask_ServiceStopStopsOnce(t *testing.T) {
	events := &eventLog{}
	task := &fakeTask{name: "once", events: events, started: make(chan struct{})}

	app := New(WithTasks(task))

	runErr := make(chan error, 1)
	go func() { runErr <- app.Run() }()

	waitForSignal(t, task.started, "task to start")
	if err := app.Stop(); err != nil {
		t.Fatalf("first Stop() error = %v", err)
	}
	if err := app.Stop(); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
	if err := waitRun(t, runErr); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := task.stopCalls.Load(); got != 1 {
		t.Fatalf("task Stop calls = %d, want 1", got)
	}
}

// TestTask_StopPerTaskBudget verifies that every task Stop gets its own fresh
// deadline-bounded context: a task that blocks until its budget expires does
// not starve the tasks stopped after it, and its timeout error is returned.
// TestTask_StopPerTaskBudget 验证每个任务的 Stop 获得独立的、带截止时间的新鲜
// context：阻塞至预算耗尽的任务不会饿死其后停止的任务，其超时错误会被返回。
func TestTask_StopPerTaskBudget(t *testing.T) {
	events := &eventLog{}
	first := newStopProbeTask("first", events)
	first.blockStop = true
	second := newStopProbeTask("second", events)

	const budget = 50 * time.Millisecond
	app := New(WithTasks(first, second), WithTaskStopTimeout(budget))

	runErr := make(chan error, 1)
	go func() { runErr <- app.Run() }()

	waitForSignal(t, second.started, "second task to start")
	begin := time.Now()
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	err := waitRun(t, runErr)
	elapsed := time.Since(begin)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want context.DeadlineExceeded from the exhausted first task", err)
	}
	if elapsed < budget {
		t.Fatalf("Run() returned after %v, want at least one %v task budget", elapsed, budget)
	}
	if got := first.stopCalls.Load(); got != 1 {
		t.Fatalf("first task Stop calls = %d, want 1", got)
	}
	if got := second.stopCalls.Load(); got != 1 {
		t.Fatalf("second task Stop calls = %d, want 1", got)
	}

	obs := second.observed()
	if obs.ctxErr != nil {
		t.Fatalf("second task Stop context error = %v, want a fresh live context", obs.ctxErr)
	}
	if !obs.hadDeadline || obs.deadlineRemain <= 0 {
		t.Fatalf("second task Stop deadline remaining = %v (deadline set = %v), want a fresh future deadline",
			obs.deadlineRemain, obs.hadDeadline)
	}
	if !obs.hadService {
		t.Fatal("second task Stop context is missing the application service value")
	}
	assertEventOrder(t, events, "task:first:stop", "task:second:stop")
}

// TestTask_StopSurvivesCanceledAppContext verifies that externally canceling the
// application context does not pre-cancel task cleanup: Stop still sees a live
// context carrying the app values, and its error (even context.Canceled) is
// surfaced by Run instead of filtered.
// TestTask_StopSurvivesCanceledAppContext 验证外部取消应用 context 不会提前取消
// 任务清理：Stop 仍看到携带应用值的可用 context，其错误（即使是
// context.Canceled）也会由 Run 返回而不是被过滤。
func TestTask_StopSurvivesCanceledAppContext(t *testing.T) {
	events := &eventLog{}
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	task := newStopProbeTask("cleanup", events)
	task.stopErr = context.Canceled

	app := New(WithContext(appCtx), WithTasks(task))

	runErr := make(chan error, 1)
	go func() { runErr <- app.Run() }()

	waitForSignal(t, task.started, "task to start")
	cancelApp()

	err := waitRun(t, runErr)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want the task stop error context.Canceled to be surfaced", err)
	}
	if got := task.stopCalls.Load(); got != 1 {
		t.Fatalf("task Stop calls = %d, want 1", got)
	}

	obs := task.observed()
	if obs.ctxErr != nil {
		t.Fatalf("task Stop context error = %v, want a cleanup context detached from app cancellation", obs.ctxErr)
	}
	if !obs.hadDeadline || obs.deadlineRemain <= 0 {
		t.Fatalf("task Stop deadline remaining = %v (deadline set = %v), want the default task budget",
			obs.deadlineRemain, obs.hadDeadline)
	}
	if !obs.hadService {
		t.Fatal("task Stop context is missing the application service value")
	}
}

// TestTask_EffectiveStopTimeout verifies the task stop budget precedence:
// WithTaskStopTimeout overrides WithStopTimeout, which overrides the default;
// non-positive task values are ignored.
// TestTask_EffectiveStopTimeout 验证任务停止预算的优先级：WithTaskStopTimeout
// 覆盖 WithStopTimeout，后者覆盖默认值；任务预算的非正值被忽略。
func TestTask_EffectiveStopTimeout(t *testing.T) {
	if defaultTaskStopTimeout != 30*time.Second {
		t.Fatalf("defaultTaskStopTimeout = %v, want 30s", defaultTaskStopTimeout)
	}

	tests := []struct {
		name string
		opts []Option
		want time.Duration
	}{
		{
			name: "default",
			want: defaultTaskStopTimeout,
		},
		{
			name: "stop timeout fallback",
			opts: []Option{WithStopTimeout(75 * time.Millisecond)},
			want: 75 * time.Millisecond,
		},
		{
			name: "task timeout wins",
			opts: []Option{WithStopTimeout(75 * time.Millisecond), WithTaskStopTimeout(50 * time.Millisecond)},
			want: 50 * time.Millisecond,
		},
		{
			name: "non-positive task timeouts ignored",
			opts: []Option{WithStopTimeout(75 * time.Millisecond), WithTaskStopTimeout(0), WithTaskStopTimeout(-time.Second)},
			want: 75 * time.Millisecond,
		},
		{
			name: "non-positive task timeout keeps default",
			opts: []Option{WithTaskStopTimeout(-time.Second)},
			want: defaultTaskStopTimeout,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewOptions(tt.opts...).effectiveTaskStopTimeout(); got != tt.want {
				t.Fatalf("effectiveTaskStopTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestTask_NilTask verifies that nil tasks are skipped at option time and
// defensively by Run, so a nil Task cannot panic the lifecycle and the app
// behaves as if no tasks were configured.
// TestTask_NilTask 验证 nil 任务在选项阶段被跳过、Run 也会防御性跳过，nil Task
// 不会让生命周期 panic，应用的行为与未配置任务一致。
func TestTask_NilTask(t *testing.T) {
	events := &eventLog{}
	srv := newFakeServer(events)

	app := New(WithServers(srv), WithTasks(nil))
	if got := len(app.opts.tasks); got != 0 {
		t.Fatalf("WithTasks(nil) recorded %d tasks, want 0", got)
	}
	// Bypass WithTasks to prove that Run also tolerates a nil entry that reached
	// the options directly.
	// 绕过 WithTasks，验证 Run 同样容忍直接进入选项的 nil 条目。
	app.opts.tasks = append(app.opts.tasks, nil)

	runErr := make(chan error, 1)
	go func() { runErr <- app.Run() }()

	waitForSignal(t, srv.started, "server to start")
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := waitRun(t, runErr); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	assertEventOrder(t, events, "server:start", "server:stop")
}

// deadlineServer records the deadline of the context passed to Stop.
// deadlineServer 记录传给 Stop 的 context 的截止时间。
type deadlineServer struct {
	started  chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
	deadline atomic.Value
}

func (s *deadlineServer) Start(context.Context) error {
	close(s.started)
	<-s.stopCh
	return nil
}

func (s *deadlineServer) Stop(ctx context.Context) error {
	if d, ok := ctx.Deadline(); ok {
		s.deadline.Store(d)
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
	return nil
}

func (s *deadlineServer) Health() bool { return true }

func TestTask_StopErrorStillRunsAfterStop(t *testing.T) {
	t.Parallel()

	events := &eventLog{}
	srv := newFakeServer(events)
	task := &fakeTask{
		name:    "bad",
		events:  events,
		started: make(chan struct{}),
		stopErr: errors.New("stop failed"),
	}
	var afterRan atomic.Bool
	app := New(
		WithServers(srv),
		WithTasks(task),
		AfterStop(func(context.Context) error {
			afterRan.Store(true)
			return nil
		}),
	)

	runErr := make(chan error, 1)
	go func() { runErr <- app.Run() }()

	waitForSignal(t, task.started, "task to start")
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := waitRun(t, runErr); err == nil || !strings.Contains(err.Error(), "stop failed") {
		t.Fatalf("Run() error = %v, want the task stop error", err)
	}
	if !afterRan.Load() {
		t.Fatal("AfterStop hook was skipped after a task stop error")
	}
}

func TestTask_ServerStopContextKeepsCallerDeadline(t *testing.T) {
	t.Parallel()

	deadline := time.Now().Add(time.Minute)
	base, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	events := &eventLog{}
	srv := &deadlineServer{started: make(chan struct{}), stopCh: make(chan struct{})}
	task := &fakeTask{name: "ok", events: events}

	app := New(
		WithContext(base),
		WithServers(srv),
		WithTasks(task),
	)

	runErr := make(chan error, 1)
	go func() { runErr <- app.Run() }()

	waitForSignal(t, srv.started, "server to start")
	if err := app.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := waitRun(t, runErr); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got, ok := srv.deadline.Load().(time.Time)
	if !ok || got.IsZero() {
		t.Fatal("server Stop context lost the caller deadline")
	}
	if !got.Equal(deadline) {
		t.Fatalf("server Stop deadline = %v, want %v", got, deadline)
	}
}
