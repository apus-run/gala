// Package cron implements a crontab-like service to execute and schedule
// jobs by id.
//
// Example:
//
//	c := cron.New(cron.WithLocation(time.UTC))
//	c.MustAdd("dailyReport", "0 0 * * *", func() { ... }, cron.SkipIfRunning())
//	c.Start()
//	defer c.Shutdown(ctx) // stop ticking and wait for the running jobs
//
// Semantics worth knowing:
//   - Jobs have minute granularity (5 fields, see NewSchedule).
//   - A job runs at most once per wall-clock minute, and a wall-clock
//     minute that is repeated by a DST fall-back doesn't run it twice.
//     A minute that doesn't exist (DST spring forward) is skipped.
//   - Every run happens in its own goroutine, so a slow job never delays
//     the others. Use SkipIfRunning to prevent a job from overlapping
//     with its previous run.
//   - A panic inside a job is recovered and reported to the PanicHandler
//     instead of crashing the process.
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
const maxWallRewind = 3 * 60

// PanicHandler is called when a job panics.
// The stack is the stack trace of the panicking goroutine.
type PanicHandler func(jobId string, recovered any, stack []byte)

func defaultPanicHandler(jobId string, recovered any, stack []byte) {
	log.Printf("cron: job %q panicked: %v\n%s", jobId, recovered, stack)
}

// Option configures a Cron instance (see New).
type Option interface {
	apply(*Cron)
}

// OptionFunc adapts a plain function to Option.
type OptionFunc func(*Cron)

func (f OptionFunc) apply(o *Cron) {
	f(o)
}

// WithLocation sets the timezone used to evaluate the job schedules
// (default UTC). A nil location is ignored.
func WithLocation(l *time.Location) Option {
	return OptionFunc(func(c *Cron) {
		if l != nil {
			c.location = l
		}
	})
}

// WithInterval sets the tick interval, see Cron.SetInterval.
func WithInterval(d time.Duration) Option {
	return OptionFunc(func(c *Cron) {
		c.interval = normalizeInterval(d)
	})
}

// WithPanicHandler replaces the default handler (that logs the panic and
// the stack trace). A nil handler is ignored.
//
// To crash the process on a job panic, panic again from the handler.
func WithPanicHandler(h PanicHandler) Option {
	return OptionFunc(func(c *Cron) {
		if h != nil {
			c.onPanic = h
		}
	})
}

// JobOption configures a single job (see Cron.Add).
type JobOption interface {
	apply(*job)
}

// JobOptionFunc adapts a plain function to JobOption.
type JobOptionFunc func(*job)

func (f JobOptionFunc) apply(o *job) {
	f(o)
}

// SkipIfRunning makes the job skip a due run when its previous run
// hasn't returned yet.
//
// The "running" state follows the job id, so replacing a job with Add
// doesn't allow the new version to overlap with the old one.
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
	running       *atomic.Bool // shared between the versions of the same job id

	// last wall-clock minute (see wallKey) the job was fired for;
	// guarded by Cron.mu.
	ran     bool
	lastKey int64
}

// JobInfo describes a registered job.
type JobInfo struct {
	ID         string    `json:"id"`
	Expression string    `json:"expression"`
	NextRun    time.Time `json:"nextRun"` // zero if the job will never run
}

// Cron is a crontab-like struct for tasks/jobs scheduling.
//
// All methods are safe for concurrent use.
type Cron struct {
	mu       sync.RWMutex
	location *time.Location
	interval time.Duration
	jobs     map[string]*job
	cancel   context.CancelFunc // non-nil while started
	onPanic  PanicHandler

	loops    sync.WaitGroup // tick loop goroutines
	inflight sync.WaitGroup // running jobs
}

// New create a new Cron struct with default tick interval of 1 minute
// and timezone in UTC.
//
// You can change the defaults with the options or later with
// Cron.SetInterval() and Cron.SetTimezone().
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
func (c *Cron) SetTimezone(l *time.Location) {
	if l == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.location = l

	// the wall clock jumps to a different timeline
	for _, j := range c.jobs {
		if j.schedule.Location == nil {
			j.ran = false
		}
	}
}

// MustAdd is similar to Add() but panic on failure.
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
// cronExpr is a regular cron expression, eg. "0 */3 * * *" (aka. at minute 0 past every 3rd hour).
// Check cron.NewSchedule() for the supported tokens.
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
func (c *Cron) Remove(jobId string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.jobs, jobId)
}

// RemoveAll removes all registered cron jobs.
func (c *Cron) RemoveAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.jobs = map[string]*job{}
}

// Total returns the current total number of registered cron jobs.
func (c *Cron) Total() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.jobs)
}

// Jobs returns a snapshot of the registered jobs (sorted by id)
// together with their next run time.
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
// 不要在任务（job）里调用 Shutdown：它要等所有运行中的任务返回，包括调用它的这一个，
// 传 context.Background() 会永久阻塞，传带超时的 ctx 会白等到超时。任务想让调度器停下来时调用 Stop。
func (c *Cron) Shutdown(ctx context.Context) error {
	c.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.loops.Wait()    // no more jobs will be dispatched after this
		c.inflight.Wait() // wait for the already dispatched ones
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
func (c *Cron) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.startLocked()
}

// HasStarted checks whether the cron is started, aka. whether Start() was
// called (and takes effect immediately, even before the first tick)
// without a following Stop().
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
func (c *Cron) loop(ctx context.Context, interval time.Duration) {
	defer c.loops.Done()

	// delay the ticker to start at 00 of 1 interval duration
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
	if ctx.Err() != nil {
		return
	}
	c.runDue(time.Now())

	// run after each tick
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			if ctx.Err() != nil { // select picks randomly when both are ready
				return
			}
			c.runDue(t)
		}
	}
}

// runDue runs all registered jobs that are scheduled for the provided time.
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
	for _, j := range due {
		c.dispatch(j)
	}
}

// wallKey converts the wall-clock minute of t to a monotonic
// (in the absence of DST/clock changes) integer.
func wallKey(t time.Time) int64 {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC).Unix() / 60
}

func (c *Cron) dispatch(j *job) {
	if j.skipIfRunning && !j.running.CompareAndSwap(false, true) {
		return // the previous run is still in progress
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
