// Package gala is the framework entry point: a Service that starts servers,
// registers the instance, and shuts everything down on signal | 框架入口：
// Service 启动各服务器、注册实例并在收到信号时优雅停止.
//
// # Notes | 说明
//
// New assembles the app from options; Run starts every server in an
// errgroup, registers the ServiceInstance with the configured registry once
// they are running, then blocks on the configured signals (SIGINT, SIGTERM,
// SIGQUIT by default) or until a server returns. Stop deregisters and cancels
// the context; a stopTimeout, when set, bounds how long servers get to drain.
// Endpoints default to what the servers expose as server.Endpointer. The
// package also carries typed context metadata (trace ID, user ID, color tag)
// and build Info helpers overridden via -ldflags. WithTasks registers
// background tasks that start after the servers and the afterStart hooks and
// stop before the servers during shutdown. Each task Stop gets its own cleanup
// context: it carries the application context values but not its cancellation,
// and its budget is taskStopTimeout when set, otherwise stopTimeout when set,
// otherwise a 30s default. Nil tasks are ignored.
// New 按选项组装应用；Run 在 errgroup 中启动所有服务器，就绪后向配置的 registry
// 注册 ServiceInstance，随后阻塞等待信号（默认 SIGINT、SIGTERM、SIGQUIT）或任一
// 服务器返回。Stop 注销实例并取消 context；设置 stopTimeout 时限定服务器的排水
// 时间。未显式配置时端点取各 server 的 Endpointer 实现。包内还提供类型化的
// context 元数据（trace ID、user ID、color tag）与可用 -ldflags 覆盖的构建 Info。
// WithTasks 注册的后台任务在服务器与 afterStart 钩子之后启动，并在停机时先于
// 服务器停止。每个任务的 Stop 获得独立的清理 context：它携带应用 context 的值
// 但不继承其取消信号，预算优先取 taskStopTimeout，其次 stopTimeout，两者都未
// 设置时默认 30s。nil 任务会被忽略。
//
// # Features | 功能
//
// - Run / Stop lifecycle with before/after hooks | 带 before/after 钩子的 Run / Stop 生命周期
// - Servers managed in an errgroup | errgroup 托管的多个服务器
// - Background tasks ordered around the server lifecycle | 围绕服务器生命周期有序启停的后台任务
// - Per-task stop budgets with WithTaskStopTimeout | 通过 WithTaskStopTimeout 为每个后台任务设置停止预算
// - Optional registry with registration timeout | 可选服务注册与注册超时
// - WithTraceID / WithUserID / WithColorTag context helpers | context 元数据助手
// - Version Info with build metadata | 携带构建元数据的版本 Info
//
// # Example | 示例
//
//	app := gala.New(
//		gala.WithName("demo"),
//		gala.WithVersion("v1.0.0"),
//		gala.WithServers(httpServer),
//	)
//	if err := app.Run(); err != nil {
//		log.Fatal(err)
//	}
package gala

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"uuid"

	"golang.org/x/sync/errgroup"

	"github.com/apus-run/gala/registry"
	"github.com/apus-run/gala/server"
)

// Service is the application lifecycle manager | Service 是应用生命周期管理器.
type Service struct {
	opts   *Options
	ctx    context.Context
	cancel context.CancelFunc

	mux      sync.Mutex
	instance *registry.ServiceInstance
}

// New create an application lifecycle manager | New 创建应用生命周期管理器.
func New(opts ...Option) *Service {
	options := NewOptions(opts...)
	if options.id == "" {
		options.id = uuid.NewV4().String()
	}

	ctx, cancel := context.WithCancel(options.context)
	return &Service{
		ctx:    ctx,
		cancel: cancel,
		opts:   options,
	}
}

// ID returns app instance id | ID 返回应用实例 id.
func (s *Service) ID() string { return s.opts.id }

// Name returns service name | Name 返回服务名.
func (s *Service) Name() string { return s.opts.name }

// Version returns app version | Version 返回应用版本.
func (s *Service) Version() string { return s.opts.version }

// Metadata returns service metadata | Metadata 返回服务元数据.
func (s *Service) Metadata() map[string]string { return s.opts.metadata }

// Endpoint returns endpoints | Endpoint 返回端点列表.
func (s *Service) Endpoint() []string {
	if s.instance != nil {
		return s.instance.Endpoints
	}
	return nil
}

// Run executes all OnStart hooks registered with the application's Lifecycle | Run 执行应用 Lifecycle 中注册的所有 OnStart 钩子.
func (s *Service) Run() error {
	instance, err := s.registryService()
	if err != nil {
		return err
	}
	s.mux.Lock()
	s.instance = instance
	s.mux.Unlock()
	c := ServiceContextKey.NewContext(s.ctx, s)
	eg, ctx := errgroup.WithContext(c)
	wg := sync.WaitGroup{}
	var taskStopErr error

	// tasks is the nil-free view of the configured tasks. A nil Task interface
	// must never be invoked through Start or Stop.
	// tasks 是已配置任务去除 nil 后的视图；nil Task 接口绝不能通过 Start 或
	// Stop 被调用。
	tasks := make([]Task, 0, len(s.opts.tasks))
	for _, task := range s.opts.tasks {
		if task != nil {
			tasks = append(tasks, task)
		}
	}

	for _, fn := range s.opts.beforeStart {
		if err = fn(c); err != nil {
			return err
		}
	}

	// octx is the cancellable context handed to server Start, so transports see
	// shutdown. cleanupBase carries the same values without the cancellation:
	// an externally canceled app context must not pre-cancel graceful cleanup,
	// so both server and task stop contexts derive from it.
	// octx 是传给服务器 Start 的可取消 context，使传输服务感知停机。cleanupBase
	// 携带相同的值但不继承取消信号：外部取消应用 context 不应提前取消优雅清理，
	// 因此服务器与任务的停止 context 都从它派生。
	octx := ServiceContextKey.NewContext(s.opts.context, s)
	// WithoutCancel drops a deadline as well as cancellation; keep the
	// caller's deadline so a WithContext deadline still bounds transport drain
	// when WithStopTimeout is unset.
	// WithoutCancel 同时丢弃截止时间与取消信号；保留调用方截止时间，使未设置
	// WithStopTimeout 时 WithContext 的截止时间仍能约束传输服务的排水。
	detached := context.WithoutCancel(s.opts.context)
	if deadline, ok := s.opts.context.Deadline(); ok {
		var cancel context.CancelFunc
		detached, cancel = context.WithDeadline(detached, deadline)
		defer cancel()
	}
	cleanupBase := ServiceContextKey.NewContext(detached, s)

	// tasksStopped is closed once task shutdown finishes, or immediately when no
	// tasks are configured. The server stop goroutines wait for it, so tasks
	// stop before the transports they may depend on.
	// tasksStopped 在任务停止完成后关闭，未配置任务时立即关闭。服务器的停止
	// goroutine 会等待它，从而让任务先于其可能依赖的传输服务停止。
	tasksStopped := make(chan struct{})
	var tasksStoppedOnce sync.Once
	closeTasksStopped := func() {
		tasksStoppedOnce.Do(func() { close(tasksStopped) })
	}
	if len(tasks) == 0 {
		closeTasksStopped()
	}

	// tasksStarted guards the early-return paths below: if Run returns before
	// the task goroutines are launched, no task will ever stop, yet the channel
	// must still be closed to keep the server stop goroutines from blocking
	// forever on it should the context be canceled later.
	// tasksStarted 约束下面的提前返回路径：若 Run 在任务 goroutine 启动前返回，
	// 任务永远不会停止，但仍须关闭通道，避免 context 之后被取消时服务器的停止
	// goroutine 永久阻塞。
	tasksStarted := false
	defer func() {
		if !tasksStarted {
			closeTasksStopped()
		}
	}()

	for _, srv := range s.opts.servers {
		eg.Go(func() error {
			<-ctx.Done()   // wait for stop signal | 等待停止信号
			<-tasksStopped // tasks stop before their transports | 任务先于传输服务停止
			stopCtx := cleanupBase
			if s.opts.stopTimeout > 0 {
				var cancel context.CancelFunc
				stopCtx, cancel = context.WithTimeout(stopCtx, s.opts.stopTimeout)
				defer cancel()
			}
			return srv.Stop(stopCtx)
		})
		wg.Add(1)
		eg.Go(func() error {
			wg.Done() // here is to ensure core start has begun running before register, so defer is not needed | 这里确保核心启动先于注册开始运行，因此无需 defer
			return srv.Start(octx)
		})
	}
	wg.Wait()
	if s.opts.registry != nil {
		rctx, rcancel := context.WithTimeout(ctx, s.opts.registryTimeout)
		defer rcancel()
		if err = s.opts.registry.Register(rctx, instance); err != nil {
			return err
		}
	}
	for _, fn := range s.opts.afterStart {
		if err = fn(c); err != nil {
			return err
		}
	}

	// Tasks start only after the transports are running and the afterStart
	// hooks have completed, so their jobs can use the app context and the
	// registered services. One goroutine both starts and stops them: the start
	// pass finishes (or fails) before any Stop runs, so a shutdown racing with
	// startup can never leave a task started after its Stop. It is the only
	// closer of tasksStopped once tasks exist.
	// 任务仅在传输服务已启动且 afterStart 钩子完成后启动，使其作业可以使用应用
	// context 与已注册的服务。由同一个 goroutine 负责启停：启动阶段结束（或失败）
	// 后才执行任何 Stop，因此与启动竞争的停机不会让任务在 Stop 之后又被启动。
	// 任务存在时它是 tasksStopped 的唯一关闭者。
	if len(tasks) > 0 {
		tasksStarted = true
		eg.Go(func() error {
			var startErr error
			for _, task := range tasks {
				if ctx.Err() != nil {
					// Shutdown was requested before this task started; do not
					// start new work. Stop below still runs for every task.
					// 该任务启动前已请求停机：不再启动新工作。下面的 Stop 仍会
					// 对每个任务执行。
					break
				}
				// Start sequentially in option order. A failure skips the
				// remaining starts and is returned after stopping everything.
				// 按选项顺序依次启动。失败时跳过剩余启动，并在全部停止后返回错误。
				if startErr = task.Start(c); startErr != nil {
					break
				}
			}

			if startErr == nil && ctx.Err() == nil {
				<-ctx.Done() // signal, Stop, or a failing goroutine triggered shutdown | 由信号、Stop 或失败的 goroutine 触发停机
			}

			// Stop sequentially in option order. Every task gets its own
			// deadline-bounded context derived from cleanupBase, so a task that
			// exhausts its budget cannot starve the tasks stopped after it.
			// Cancel happens after the call, never deferred inside the loop.
			// Every Stop error is recorded: a context.Canceled reported by a
			// task is a cleanup failure, not the normal signal path.
			// 按选项顺序依次停止。每个任务都获得从 cleanupBase 派生的独立、带截止
			// 时间的 context，因此耗尽预算的任务不会饿死其后停止的任务。cancel 在
			// 调用之后执行，绝不在循环中 defer。每个 Stop 错误都会被收集：任务返回
			// 的 context.Canceled 属于清理失败，而非正常的信号停机路径。
			taskTimeout := s.opts.effectiveTaskStopTimeout()
			var errs []error
			for _, task := range tasks {
				stopCtx, cancel := context.WithTimeout(cleanupBase, taskTimeout)
				if err := task.Stop(stopCtx); err != nil {
					errs = append(errs, err)
				}
				cancel()
			}
			taskStopErr = errors.Join(errs...)
			closeTasksStopped()

			return startErr
		})
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, s.opts.signals...)
	eg.Go(func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
			return s.Stop()
		}
	})
	err = eg.Wait()
	if err != nil && errors.Is(err, context.Canceled) {
		err = nil
	}
	// Surface task stop errors even when eg.Wait only reported the
	// context.Canceled of a normal shutdown, and still run the afterStop
	// cleanup hooks: a task that exhausted its budget must not skip DB/tracer
	// cleanup.
	// 即使 eg.Wait 只返回正常停机时的 context.Canceled，任务停止错误也在此返回；
	// 同时仍执行 afterStop 清理钩子：耗尽预算的任务不应跳过数据库/追踪器清理。
	if taskStopErr != nil {
		err = errors.Join(err, taskStopErr)
	}
	for _, fn := range s.opts.afterStop {
		err = errors.Join(err, fn(c))
	}
	return err
}

// Stop gracefully stops the application | Stop 优雅地停止应用.
func (s *Service) Stop() error {
	var err error
	sctx := ServiceContextKey.NewContext(s.ctx, s)
	for _, fn := range s.opts.beforeStop {
		if err = fn(sctx); err != nil {
			return err
		}
	}

	s.mux.Lock()
	instance := s.instance
	s.mux.Unlock()
	if s.opts.registry != nil && instance != nil {
		ctx, cancel := context.WithTimeout(ServiceContextKey.NewContext(s.ctx, s), s.opts.registryTimeout)
		defer cancel()
		if err = s.opts.registry.Deregister(ctx, instance); err != nil {
			return err
		}
	}
	if s.cancel != nil {
		s.cancel()
	}
	return err
}

func (s *Service) registryService() (*registry.ServiceInstance, error) {
	endpoints := make([]string, 0, len(s.opts.endpoints))
	for _, e := range s.opts.endpoints {
		endpoints = append(endpoints, e.String())
	}
	if len(endpoints) == 0 {
		for _, srv := range s.opts.servers {
			if r, ok := srv.(server.Endpointer); ok {
				e, err := r.Endpoint()
				if err != nil {
					return nil, err
				}
				endpoints = append(endpoints, e.String())
			}
		}
	}
	return &registry.ServiceInstance{
		ID:        s.opts.id,
		Name:      s.opts.name,
		Version:   s.opts.version,
		Metadata:  s.opts.metadata,
		Endpoints: endpoints,
	}, nil
}
