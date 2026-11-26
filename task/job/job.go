// Package job runs explicitly registered one-shot jobs, triggered from the CLI
// or HTTP, with an optional single-run lease | job 运行显式注册的一次性任务，支持
// CLI 或 HTTP 触发，并可选单次执行租约.
//
// # Notes | 说明
//
// A Registry maps names to Funcs, and only names registered by the caller are
// runnable: there is no init-time registration, no global registry and no
// package-level state, so two registries are fully independent. Invocation
// metadata (name, run id, payload, headers) travels inside the context and is
// read with [FromContext]. [Registry.Run] recovers job panics and returns a
// [*PanicError], so neither an HTTP request nor a CLI invocation can crash the
// process. This package does no I/O of its own except reading a request body or
// stdin in the trigger adapters; persistence, retries and run history stay in
// the application.
// Registry 把名称映射到 Func，只有调用方显式注册的名称可执行：没有 init 注册、
// 没有全局 Registry、没有包级状态，两个 Registry 完全独立。Invocation 元数据
// （名称、运行 id、payload、请求头）随 context 传递，用 [FromContext] 读取。
// [Registry.Run] 会 recover 任务 panic 并返回 [*PanicError]，因此 HTTP 请求或
// CLI 调用都不会拖垮进程。本包自身不做 I/O，只有触发适配器会读取请求体或 stdin；
// 持久化、重试与运行历史留在应用层。
//
// # Features | 功能
//
// - Explicit registry: Register, Names and Run | 显式注册表：Register、Names、Run
// - CLI trigger: RunCLI parses --job / --job-data / --job-header | CLI 触发：RunCLI 解析 --job / --job-data / --job-header
// - HTTP trigger: Handler accepts POST with a job-name header | HTTP 触发：Handler 接受带任务名请求头的 POST
// - Optional lease: WithLockerProvider serializes a job across replicas | 可选租约：WithLockerProvider 在副本间串行化任务
//
// # Example | 示例
//
//	r := job.NewRegistry(job.WithLockerProvider(func(ctx context.Context, name string) (task.Locker, error) {
//		// One fresh Locker per Run, with this replica's own token.
//		// 每次 Run 一个新 Locker，并带本副本自己的令牌。
//		return lockClient.NewLock(ctx, dlock.WithLockName("job:"+name))
//	}))
//	r.Register("resize", func(ctx context.Context) error {
//		inv, _ := job.FromContext(ctx)
//		return resize(ctx, inv.Payload)
//	})
//
//	http.Handle("/jobs", r.Handler())
//	err := r.RunCLI(ctx, []string{"--job", "resize", "--job-data", "-"}, os.Stdin)
package job

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"sort"
	"sync"
)

var (
	// ErrNotFound is returned by Run when no job is registered under the name.
	// ErrNotFound 表示 Run 找不到该名称对应的任务。
	ErrNotFound = errors.New("job: not found")

	// ErrDuplicate is returned by Register when the name is already taken.
	// Registration never replaces an existing job.
	// ErrDuplicate 表示名称已被注册。注册不会替换已有任务。
	ErrDuplicate = errors.New("job: duplicate name")

	// ErrEmptyName is returned by Register for an empty job name.
	// ErrEmptyName 表示任务名称为空。
	ErrEmptyName = errors.New("job: empty name")

	// ErrNilFunc is returned by Register for a nil Func.
	// ErrNilFunc 表示任务函数为 nil。
	ErrNilFunc = errors.New("job: nil func")
)

// Func is a job body. The invocation is available from ctx via [FromContext].
// Func 是任务函数。可通过 [FromContext] 从 ctx 取出本次调用的 [Invocation]。
type Func func(ctx context.Context) error

// Invocation describes one job run.
//
// Payload and Header are owned by the caller and treated as read-only by the
// registry; a job that needs to mutate them should copy first.
// Invocation 描述一次任务运行。
//
// Payload 与 Header 归调用方所有，Registry 按只读处理；任务若需修改应先复制。
type Invocation struct {
	Name    string      // registered job name | 已注册的任务名称
	RunID   string      // optional trace/correlation id | 可选的追踪/关联 id
	Payload []byte      // optional body | 可选的任务数据
	Header  http.Header // optional metadata | 可选的元数据
}

// contextKey is the private key under which NewContext stores an Invocation.
// contextKey 是 NewContext 存放 Invocation 的私有键。
type contextKey struct{}

// NewContext returns a child of ctx carrying inv for the job to read with
// FromContext.
//
// A nil ctx is treated as context.Background.
// NewContext 返回携带 inv 的 ctx 子 context，任务可用 FromContext 读取。
//
// ctx 为 nil 时按 context.Background 处理。
func NewContext(ctx context.Context, inv Invocation) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, inv)
}

// FromContext returns the invocation stored by NewContext.
//
// It reports false when ctx carries no invocation (including a nil ctx).
// FromContext 返回 NewContext 存入的 Invocation。
//
// ctx 未携带 Invocation（包括 nil ctx）时返回 false。
func FromContext(ctx context.Context) (Invocation, bool) {
	if ctx == nil {
		return Invocation{}, false
	}
	inv, ok := ctx.Value(contextKey{}).(Invocation)
	return inv, ok
}

// PanicError is returned by Registry.Run when a job panics.
//
// Run recovers the panic, so an HTTP-triggered or CLI-triggered job cannot
// crash the process; callers inspect the panic value and stack with errors.As.
// Error deliberately contains only the panic value, not the stack: its text
// may be written to an HTTP response, so stack traces belong in logs, not in
// responses.
// PanicError 是 Registry.Run 在任务 panic 时返回的错误。
//
// Run 会 recover panic，因此 HTTP 或 CLI 触发的任务不会拖垮进程；调用方通过
// errors.As 读取 panic 值与堆栈。Error 刻意只包含 panic 值而不含堆栈：它的文本
// 可能被写入 HTTP 响应，堆栈应留在日志里而不是响应里。
type PanicError struct {
	Value any    // value passed to panic | panic 时传入的值
	Stack []byte // stack trace of the panicking goroutine | panic 时的 goroutine 堆栈
}

// Error implements error.
// Error 实现 error。
func (e *PanicError) Error() string {
	return fmt.Sprintf("job: panic: %v", e.Value)
}

// config is the immutable construction-time configuration of a Registry.
// config 是 Registry 构造时确定的不可变配置。
type config struct {
	lockProvider LockerProvider
	lock         lockConfig
}

// Option configures a Registry, see [NewRegistry].
// Option 配置 Registry，见 [NewRegistry]。
type Option func(*config)

// Registry is an explicit job registry: only names registered by the caller
// can run.
//
// All methods are safe for concurrent use.
// Registry 是显式任务注册表：只有调用方注册过的名称才能运行。
//
// 所有方法并发安全。
type Registry struct {
	mu    sync.RWMutex
	funcs map[string]Func
	config
}

// NewRegistry creates an empty registry. It performs no I/O and starts no
// goroutines.
//
// Unless configured otherwise, a lease is refreshed every 5s and lease
// operations time out after 5s.
// NewRegistry 创建空注册表。它不做任何 I/O，也不启动 goroutine。
//
// 未另行配置时，租约每 5s 续期一次，租约操作超时为 5s。
func NewRegistry(opts ...Option) *Registry {
	cfg := config{
		lock: lockConfig{
			refreshInterval: defaultLockRefreshInterval,
			acquireTimeout:  defaultLockAcquireTimeout,
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return &Registry{
		funcs:  make(map[string]Func),
		config: cfg,
	}
}

// Register adds fn under name. It rejects an empty name with [ErrEmptyName], a
// nil fn with [ErrNilFunc] and an already registered name with [ErrDuplicate];
// an existing job is never replaced.
//
// It is safe for concurrent use.
// Register 以 name 注册 fn。空名称返回 [ErrEmptyName]，nil 函数返回
// [ErrNilFunc]，名称已存在返回 [ErrDuplicate]；已有任务不会被替换。
//
// 并发安全。
func (r *Registry) Register(name string, fn Func) error {
	if name == "" {
		return ErrEmptyName
	}
	if fn == nil {
		return ErrNilFunc
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.funcs[name]; ok {
		return ErrDuplicate
	}
	r.funcs[name] = fn

	return nil
}

// Run looks up the job by inv.Name, puts inv into the context and calls it.
//
// Unknown names return an error wrapping [ErrNotFound]. A panicking job is
// recovered and returned as a [*PanicError], never crashing the process.
//
// Without [WithLockerProvider] the job runs directly with the caller's ctx.
// With a provider, every Run resolves its own Locker (bounded by the acquire
// timeout) and then acquires the lease (also bounded); when the lease is taken
// it returns the Lock error wrapped with %w without running the job, so
// errors.Is still matches the backend's sentinel (for example
// dlock.ErrLocked). A provider returning (nil, nil) runs that call without a
// lease; a provider error fails the call without running the job. While the
// job runs, a goroutine refreshes the lease; on a refresh error the job's
// context is canceled (so a well-behaved job stops) and refreshing stops. A
// refresh still in flight when the job returns is aborted and that abort is
// not reported as an error. After the job returns, Run waits for the
// refresher, releases the lease with a fresh timeout context, and joins the
// job, refresh and unlock errors (nil errors are ignored), so no failure is
// hidden.
// Run 按 inv.Name 查找任务，把 inv 放入 context 后调用。
//
// 未知名称返回包装了 [ErrNotFound] 的错误。任务 panic 会被 recover 并以
// [*PanicError] 返回，绝不会拖垮进程。
//
// 未配置 [WithLockerProvider] 时，任务直接使用调用方的 ctx 运行。配置了 provider
// 时，每次 Run 都在获取超时内解析自己的 Locker，再在同样的超时内获取租约；租约被
// 占用时以 %w 包装 Lock 错误返回且不运行任务，因此 errors.Is 仍能匹配后端的哨兵
// 错误（例如 dlock.ErrLocked）。provider 返回 (nil, nil) 时本次调用不加租约；返回
// 错误时本次调用失败、任务不运行。任务运行期间由 goroutine 周期续期；续期失败会
// 取消任务的 ctx（行为良好的任务会停止）并停止续期。任务返回时仍在途的续期会被
// 中止，该中止不会作为错误上报。任务返回后，Run 等续期 goroutine 退出，再用全新的
// 超时 context 释放租约，并 join 任务、续期与解锁错误（忽略 nil），不隐藏任何失败。
func (r *Registry) Run(ctx context.Context, inv Invocation) error {
	if ctx == nil {
		ctx = context.Background()
	}

	fn, provider, lockCfg, ok := r.lookup(inv.Name)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, inv.Name)
	}

	if provider == nil {
		return callFunc(ctx, inv, fn)
	}

	return r.runWithLease(ctx, inv, fn, provider, lockCfg)
}

// Names returns a sorted snapshot of the registered job names.
// Names 返回已注册任务名称的排序快照。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.funcs))
	for name := range r.funcs {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// lookup resolves a job and the lease configuration in one read lock.
// lookup 在一次读锁内解析任务与租约配置。
func (r *Registry) lookup(name string) (Func, LockerProvider, lockConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	fn, ok := r.funcs[name]

	return fn, r.lockProvider, r.lock, ok
}

// has reports whether name is registered. The HTTP adapter uses it to reject
// an unknown job before reading the request body.
// has 报告 name 是否已注册。HTTP 适配器用它先拒绝未知任务，再读取请求体。
func (r *Registry) has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.funcs[name]

	return ok
}

// callFunc invokes fn with the invocation in ctx and converts a panic into a
// *PanicError.
// callFunc 带着 context 中的 Invocation 调用 fn，并把 panic 转换为 *PanicError。
func callFunc(ctx context.Context, inv Invocation, fn Func) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = &PanicError{Value: v, Stack: debug.Stack()}
		}
	}()

	return fn(NewContext(ctx, inv))
}
