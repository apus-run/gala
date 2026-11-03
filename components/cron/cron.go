// Package cron is an in-process, dependency-free crontab that schedules jobs by id | cron 是进程内、零依赖的定时任务调度器，按 id 注册任务.
//
// # Notes | 说明
//
// One ticker drives all jobs at minute granularity; each due job runs in its
// own goroutine, so a slow job never delays the others (SkipIfRunning
// prevents a job from overlapping itself). A minute repeated by a DST
// fall-back fires once; a minute erased by a spring-forward is skipped.
// A panic in a job is recovered and reported to the PanicHandler instead of
// crashing the process. Not a distributed scheduler: no persistence and no
// coordination between replicas — every process fires its own jobs.
// 单个 ticker 以分钟粒度驱动所有任务；到期任务在各自 goroutine 中运行，慢任务
// 不阻塞其它任务（SkipIfRunning 防止同一任务重叠）。DST 回拨产生的重复分钟只
// 触发一次，前跳消失的分钟不触发。任务 panic 会被 recover 并交给
// PanicHandler，不会拖垮进程。它不是分布式调度器：不持久化、多副本间不协调，
// 每个进程各自触发自己的任务。
//
// # Features | 功能
//
// - 5-field expressions, @daily-style macros, per-job CRON_TZ= prefix | 5 段表达式、@daily 等宏、单任务的 CRON_TZ= 前缀
// - Add/Remove reconfigure a live cron; Jobs snapshots next run times | Add/Remove 在线增删任务，Jobs 快照各任务下次运行时间
// - Shutdown stops the ticker and waits for running jobs, bounded by ctx | Shutdown 停止 ticker 并等待运行中的任务结束，由 ctx 限时
//
// # Example | 示例
//
//	c := cron.New(cron.WithLocation(time.UTC))
//	c.MustAdd("dailyReport", "0 0 * * *", func() { ... }, cron.SkipIfRunning())
//	c.Start()
//	defer c.Shutdown(context.Background())
package cron

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// maxWallRewind is the largest backwards jump of the wall clock (in minutes)
// that is treated as a DST fall-back or a small clock correction, and
// therefore must not re-run jobs that already ran for those minutes.
// Bigger jumps (eg. a timezone change) are treated as a new timeline.
// maxWallRewind 是墙钟回拨仍被视为 DST 回拨或小幅校时的上限（分钟数）；
// 这些已运行过的分钟不会重新执行。更大的跳变（如切换时区）则视为新时间线。
const maxWallRewind = 3 * 60

// PanicHandler is called when a job panics.
// The stack is the stack trace of the panicking goroutine.
// PanicHandler 在任务 panic 时被调用。
// stack 是发生 panic 的 goroutine 的堆栈信息。
type PanicHandler func(jobId string, recovered any, stack []byte)

func defaultPanicHandler(jobId string, recovered any, stack []byte) {
	log.Printf("cron: job %q panicked: %v\n%s", jobId, recovered, stack)
}

// Option configures a Cron instance (see New) | Option 配置 Cron 实例（见 New）。
type Option interface {
	apply(*Cron)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配为 Option。
type OptionFunc func(*Cron)

func (f OptionFunc) apply(o *Cron) {
	f(o)
}

// WithLocation sets the timezone used to evaluate the job schedules
// (default UTC). A nil location is ignored.
// WithLocation 设置解析任务调度所用的时区（默认 UTC）。location 为 nil 时忽略。
func WithLocation(l *time.Location) Option {
	return OptionFunc(func(c *Cron) {
		if l != nil {
			c.location = l
		}
	})
}

// WithInterval sets the tick interval, see Cron.SetInterval.
// WithInterval 设置 tick 间隔，见 Cron.SetInterval。
func WithInterval(d time.Duration) Option {
	return OptionFunc(func(c *Cron) {
		c.interval = normalizeInterval(d)
	})
}

// WithPanicHandler replaces the default handler (that logs the panic and
// the stack trace). A nil handler is ignored.
//
// To crash the process on a job panic, panic again from the handler.
// WithPanicHandler 替换默认处理器（默认会记录 panic 与堆栈）。handler 为 nil 时忽略。
//
// 若想任务 panic 时直接崩溃进程，在处理器里再 panic 一次即可。
func WithPanicHandler(h PanicHandler) Option {
	return OptionFunc(func(c *Cron) {
		if h != nil {
			c.onPanic = h
		}
	})
}

// JobOption configures a single job (see Cron.Add) | JobOption 配置单个任务（见 Cron.Add）。
type JobOption interface {
	apply(*job)
}

// JobOptionFunc adapts a plain function to JobOption | JobOptionFunc 把普通函数适配为 JobOption。
type JobOptionFunc func(*job)

func (f JobOptionFunc) apply(o *job) {
	f(o)
}

// SkipIfRunning makes the job skip a due run when its previous run
// hasn't returned yet.
//
// The "running" state follows the job id, so replacing a job with Add
// doesn't allow the new version to overlap with the old one.
// SkipIfRunning 让任务在上一次运行尚未返回时跳过本次到期运行。
//
// "running" 状态跟随任务 id，因此用 Add 替换任务后，新版本也不会与旧版本重叠。
func SkipIfRunning() JobOption {
	return JobOptionFunc(func(j *job) {
		j.skipIfRunning = true
	})
}

type job struct {
	id            string
	schedule      *Schedule
	run           func()
	skipIfRunning bool
	running       *atomic.Bool // shared between the versions of the same job id | 同一任务 id 的各版本共享。

	// last wall-clock minute (see wallKey) the job was fired for;
	// guarded by Cron.mu.
	// 任务最近一次触发的墙钟分钟（见 wallKey）；由 Cron.mu 保护。
	ran     bool
	lastKey int64
}

// JobInfo describes a registered job | JobInfo 描述一个已注册的任务。
type JobInfo struct {
	ID         string    `json:"id"`
	Expression string    `json:"expression"`
	NextRun    time.Time `json:"nextRun"` // zero if the job will never run | 任务永远不会运行时为零值。
}

// Cron is a crontab-like struct for tasks/jobs scheduling.
//
// All methods are safe for concurrent use.
// Cron 是类 crontab 的任务调度器。
//
// 所有方法并发安全。
type Cron struct {
	mu       sync.RWMutex
	location *time.Location
	interval time.Duration
	jobs     map[string]*job
	cancel   context.CancelFunc // non-nil while started | 已启动时非 nil。
	onPanic  PanicHandler

	loops    sync.WaitGroup // tick loop goroutines | tick 循环 goroutine。
	inflight sync.WaitGroup // running jobs | 运行中的任务。
}

// New create a new Cron struct with default tick interval of 1 minute
// and timezone in UTC.
//
// You can change the defaults with the options or later with
// Cron.SetInterval() and Cron.SetTimezone().
// New 创建 Cron 实例，默认 tick 间隔 1 分钟、时区 UTC。
//
// 可通过选项修改默认值，或之后用 Cron.SetInterval() 和 Cron.SetTimezone() 修改。
func New(opts ...Option) *Cron {
	c := &Cron{
		interval: time.Minute,
		location: time.UTC,
		jobs:     map[string]*job{},
		onPanic:  defaultPanicHandler,
	}

	for _, opt := range opts {
		if opt != nil {
			opt.apply(c)
		}
	}

	return c
}

// normalizeInterval keeps the tick interval in (0, 1m].
//
// Jobs have minute granularity, so a longer interval would skip minutes,
// while a non-positive one would make time.NewTicker panic.
// normalizeInterval 将 tick 间隔限制在 (0, 1m]。
//
// 任务只有分钟粒度，更长的间隔会跳过分钟，而非正间隔会让 time.NewTicker panic。
func normalizeInterval(d time.Duration) time.Duration {
	if d <= 0 || d > time.Minute {
		return time.Minute
	}
	return d
}

// SetInterval changes the current cron tick interval and restarts
// the ticker if the cron is started.
//
// The interval must be in (0, 1m]; other values are replaced with 1m
// (the jobs have minute granularity, so a longer interval would
// skip minutes). Intervals shorter than 1m are safe, a job still
// runs once per matching minute.
// SetInterval 修改当前 tick 间隔；若 cron 已启动则重启 ticker。
//
// 间隔必须落在 (0, 1m]；其它值会被替换成 1m（任务只有分钟粒度，更长的间隔
// 会跳过分钟）。短于 1m 的间隔是安全的，任务在每个匹配分钟仍只运行一次。
func (c *Cron) SetInterval(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.interval = normalizeInterval(d)

	if c.cancel != nil {
		c.startLocked()
	}
}

// SetTimezone changes the current cron tick timezone
// (a nil location is ignored).
// SetTimezone 修改当前 cron 的时区（location 为 nil 时忽略）。
func (c *Cron) SetTimezone(l *time.Location) {
	if l == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.location = l

	// the wall clock jumps to a different timeline
	// 墙钟跳到了另一条时间线。
	for _, j := range c.jobs {
		if j.schedule.Location == nil {
			j.ran = false
		}
	}
}

// MustAdd is similar to Add() but panic on failure | MustAdd 与 Add() 类似，但失败时 panic。
func (c *Cron) MustAdd(jobId string, cronExpr string, run func(), opts ...JobOption) {
	if err := c.Add(jobId, cronExpr, run, opts...); err != nil {
		panic(err)
	}
}

// Add registers a single cron job.
//
// If there is already a job with the provided id, then the old job
// will be replaced with the new one (a run of the old job that is
// already in progress is not interrupted).
//
// cronExpr is a regular cron expression, eg. "0 */3 * * *" (aka. at minute 0
// past every 3rd hour). Check cron.NewSchedule() for the supported tokens.
// Add 注册一个定时任务。
//
// 若已有同 id 的任务，旧任务会被替换（进行中的那次运行不会被中断）。
//
// cronExpr 是常规 cron 表达式，如 "0 */3 * * *"（每 3 小时的第 0 分钟）。
// 支持的语法见 cron.NewSchedule()。
func (c *Cron) Add(jobId string, cronExpr string, run func(), opts ...JobOption) error {
	if jobId == "" {
		return errors.New("failed to add new cron job: job id must not be empty")
	}

	if run == nil {
		return errors.New("failed to add new cron job: run must be non-nil function")
	}

	schedule, err := NewSchedule(cronExpr)
	if err != nil {
		return fmt.Errorf("failed to add new cron job: %w", err)
	}

	j := &job{
		id:       jobId,
		schedule: schedule,
		run:      run,
		running:  new(atomic.Bool),
	}
	for _, opt := range opts {
		if opt != nil {
			opt.apply(j)
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if old, ok := c.jobs[jobId]; ok {
		j.running = old.running
	}

	c.jobs[jobId] = j

	return nil
}

// Remove removes a single cron job by its id.
//
// A run that is already in progress is not interrupted.
// Remove 按 id 移除一个定时任务。
//
// 进行中的那次运行不会被中断。
func (c *Cron) Remove(jobId string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.jobs, jobId)
}

// RemoveAll removes all registered cron jobs | RemoveAll 移除所有已注册的任务。
func (c *Cron) RemoveAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.jobs = map[string]*job{}
}

// Total returns the current total number of registered cron jobs.
// Total 返回当前已注册任务的总数。
func (c *Cron) Total() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.jobs)
}

// Jobs returns a snapshot of the registered jobs (sorted by id)
// together with their next run time.
// Jobs 返回已注册任务的快照（按 id 排序）及各自下次运行时间。
func (c *Cron) Jobs() []JobInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now().In(c.location)

	result := make([]JobInfo, 0, len(c.jobs))
	for _, j := range c.jobs {
		result = append(result, JobInfo{
			ID:         j.id,
			Expression: j.schedule.Expression(),
			NextRun:    j.schedule.Next(now),
		})
	}

	sort.Slice(result, func(i, k int) bool { return result[i].ID < result[k].ID })

	return result
}

// Stop stops the current cron ticker (if not already).
//
// It doesn't wait for the jobs that are already running (use Shutdown for that).
// You can resume the ticker by calling Start().
// Stop 停止当前 cron ticker（若尚未停止）。
//
// 它不等待已在运行的任务（那用 Shutdown）。之后可以再调用 Start() 恢复 ticker。
func (c *Cron) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stopLocked()
}

// Shutdown stops the ticker and waits for the running jobs to return
// or for ctx to be done, whichever comes first (in which case ctx.Err()
// is returned and the remaining jobs keep running in the background).
//
// Don't call Start() concurrently with or after Shutdown().
//
// Don't call Shutdown from inside a job: it waits for all running jobs,
// including the calling one, so context.Background() blocks forever and a
// ctx with a timeout just waits out the timeout for nothing. Call Stop when
// a job wants the scheduler to stop.
// Shutdown 停止 ticker 并等待运行中的任务返回，或直到 ctx 结束（此时返回
// ctx.Err()，剩余任务继续在后台运行），以先到者为准。
//
// 不要与 Shutdown() 并发调用 Start()，也不要在 Shutdown() 之后再调用。
//
// 不要在任务（job）里调用 Shutdown：它要等所有运行中的任务返回，包括调用它的这一个，
// 传 context.Background() 会永久阻塞，传带超时的 ctx 会白等到超时。任务想让调度器停下来时调用 Stop。
func (c *Cron) Shutdown(ctx context.Context) error {
	c.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.loops.Wait()    // no more jobs will be dispatched after this | 此后不再分发新任务。
		c.inflight.Wait() // wait for the already dispatched ones | 等已分发的任务结束。
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Start starts the cron ticker.
//
// The first tick is aligned to the next boundary of the tick interval
// (eg. the next full minute for the default interval).
//
// Calling Start() on already started cron will restart the ticker.
// Start 启动 cron ticker。
//
// 首个 tick 对齐到 tick 间隔的下一个边界（如默认间隔下的下一个整分钟）。
//
// 对已启动的 cron 调用 Start() 会重启 ticker。
func (c *Cron) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.startLocked()
}

// HasStarted checks whether the cron is started, aka. whether Start() was
// called (and takes effect immediately, even before the first tick)
// without a following Stop().
// HasStarted 检查 cron 是否已启动，即是否调用过 Start()（立即生效，甚至早于
// 首个 tick）且之后没有调用 Stop()。
func (c *Cron) HasStarted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.cancel != nil
}

func (c *Cron) stopLocked() {
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
}

func (c *Cron) startLocked() {
	c.stopLocked()

	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	c.loops.Add(1)
	go c.loop(ctx, c.interval)
}

// loop is the single goroutine that drives a started Cron.
// It exits as soon as ctx is canceled.
// loop 是驱动已启动 Cron 的唯一 goroutine，ctx 一取消即退出。
func (c *Cron) loop(ctx context.Context, interval time.Duration) {
	defer c.loops.Done()

	// delay the ticker to start at 00 of 1 interval duration
	// 延迟 ticker，使其从间隔边界的 00 开始。
	now := time.Now()
	timer := time.NewTimer(now.Add(interval).Truncate(interval).Sub(now))
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// run immediately at 00
	// 在 00 边界立即执行一次。
	if ctx.Err() != nil {
		return
	}
	c.runDue(time.Now())

	// run after each tick
	// 每个 tick 之后执行。
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			if ctx.Err() != nil { // select picks randomly when both are ready | 两个 case 同时就绪时 select 随机选择。
				return
			}
			c.runDue(t)
		}
	}
}

// runDue runs all registered jobs that are scheduled for the provided time.
// runDue 运行所有在指定时间到期的已注册任务。
func (c *Cron) runDue(t time.Time) {
	c.mu.Lock()

	var due []*job

	for _, j := range c.jobs {
		lt := t.In(c.location)
		if j.schedule.Location != nil {
			lt = t.In(j.schedule.Location)
		}

		if !j.schedule.IsDue(NewMoment(lt)) {
			continue
		}

		// Fire at most once per wall-clock minute. This covers ticks
		// shorter than 1 minute and the repeated hour of a DST fall-back.
		// 每个墙钟分钟至多触发一次。这覆盖了短于 1 分钟的 tick，
		// 以及 DST 回拨产生的重复小时。
		key := wallKey(lt)
		if j.ran && key <= j.lastKey && j.lastKey-key <= maxWallRewind {
			continue
		}

		j.ran = true
		j.lastKey = key

		due = append(due, j)
	}

	c.mu.Unlock()

	// dispatch without holding the lock so that Add/Remove never wait for it
	// 不持锁分发，Add/Remove 无需等待该锁。
	for _, j := range due {
		c.dispatch(j)
	}
}

// wallKey converts the wall-clock minute of t to a monotonic
// (in the absence of DST/clock changes) integer.
// wallKey 把 t 的墙钟分钟转换为单调递增的整数（前提是没有 DST/校时跳变）。
func wallKey(t time.Time) int64 {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC).Unix() / 60
}

func (c *Cron) dispatch(j *job) {
	if j.skipIfRunning && !j.running.CompareAndSwap(false, true) {
		return // the previous run is still in progress | 上一次运行尚未结束。
	}

	c.inflight.Add(1)
	go c.execute(j)
}

func (c *Cron) execute(j *job) {
	defer c.inflight.Done()

	if j.skipIfRunning {
		defer j.running.Store(false)
	}

	defer func() {
		if r := recover(); r != nil {
			c.onPanic(j.id, r, debug.Stack())
		}
	}()

	j.run()
}
