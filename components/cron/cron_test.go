package cron

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCronAddRemove(t *testing.T) {
	t.Parallel()

	c := New()

	if err := c.Add("", "* * * * *", func() {}); err == nil {
		t.Fatal("expected error for empty id")
	}
	if err := c.Add("a", "* * * * *", nil); err == nil {
		t.Fatal("expected error for nil func")
	}
	if err := c.Add("a", "bad", func() {}); err == nil {
		t.Fatal("expected error for invalid expression")
	}
	if c.Total() != 0 {
		t.Fatalf("expected 0 jobs, got %d", c.Total())
	}

	c.MustAdd("a", "* * * * *", func() {})
	c.MustAdd("b", "@hourly", func() {})
	c.MustAdd("a", "@daily", func() {}) // replace
	if c.Total() != 2 {
		t.Fatalf("expected 2 jobs, got %d", c.Total())
	}

	jobs := c.Jobs()
	if len(jobs) != 2 || jobs[0].ID != "a" || jobs[0].Expression != "@daily" || jobs[1].ID != "b" {
		t.Fatalf("unexpected jobs: %+v", jobs)
	}
	for _, j := range jobs {
		if !j.NextRun.After(time.Now()) {
			t.Fatalf("job %s: unexpected next run %v", j.ID, j.NextRun)
		}
	}

	c.Remove("a")
	c.Remove("missing")
	if c.Total() != 1 {
		t.Fatalf("expected 1 job, got %d", c.Total())
	}

	c.RemoveAll()
	if c.Total() != 0 {
		t.Fatalf("expected 0 jobs, got %d", c.Total())
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("MustAdd should panic on failure")
			}
		}()
		c.MustAdd("x", "bad", func() {})
	}()
}

func TestCronRunDue(t *testing.T) {
	t.Parallel()

	c := New()

	var every, nine, never atomic.Int32
	c.MustAdd("every", "* * * * *", func() { every.Add(1) })
	c.MustAdd("nine", "0 9 * * *", func() { nine.Add(1) })
	c.MustAdd("never", "0 0 30 2 *", func() { never.Add(1) })

	base := time.Date(2026, 6, 1, 8, 59, 0, 0, time.UTC)
	c.runDue(base)                      // 08:59
	c.runDue(base.Add(time.Minute))     // 09:00
	c.runDue(base.Add(2 * time.Minute)) // 09:01

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	if every.Load() != 3 || nine.Load() != 1 || never.Load() != 0 {
		t.Fatalf("unexpected runs: every=%d nine=%d never=%d", every.Load(), nine.Load(), never.Load())
	}
}

func TestCronRunsOncePerMinute(t *testing.T) {
	t.Parallel()

	c := New()

	var n atomic.Int32
	c.MustAdd("a", "* * * * *", func() { n.Add(1) })

	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ { // 6 ticks of 10s inside the same minute
		c.runDue(base.Add(time.Duration(i) * 10 * time.Second))
	}
	c.runDue(base.Add(time.Minute))

	_ = c.Shutdown(context.Background())

	if n.Load() != 2 {
		t.Fatalf("expected 2 runs, got %d", n.Load())
	}
}

func TestCronDSTFallBackRunsOnce(t *testing.T) {
	t.Parallel()

	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}

	c := New(WithLocation(ny))

	var hourly, halfPast, everyMinute atomic.Int32
	c.MustAdd("hourly", "0 * * * *", func() { hourly.Add(1) })
	c.MustAdd("halfPast", "30 1 * * *", func() { halfPast.Add(1) })
	c.MustAdd("everyMinute", "* * * * *", func() { everyMinute.Add(1) })

	// 2026-11-01: 01:00 EDT (05:00 UTC) ... 01:59 EDT, then 01:00 EST (06:00 UTC) ... 01:59 EST, then 02:00 EST (07:00 UTC)
	start := time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC)
	for i := 0; i < 180; i++ { // 3 real hours: 01:xx EDT, 01:xx EST, 02:xx EST
		c.runDue(start.Add(time.Duration(i) * time.Minute))
	}

	_ = c.Shutdown(context.Background())

	// hourly: 01:00 (first pass only) + 02:00
	if hourly.Load() != 2 {
		t.Fatalf("hourly: expected 2 runs, got %d", hourly.Load())
	}
	if halfPast.Load() != 1 {
		t.Fatalf("halfPast: expected 1 run, got %d", halfPast.Load())
	}
	// 01:00-01:59 once + 02:00-02:59 once
	if everyMinute.Load() != 120 {
		t.Fatalf("everyMinute: expected 120 runs, got %d", everyMinute.Load())
	}
}

func TestCronDSTSpringForwardSkips(t *testing.T) {
	t.Parallel()

	ny, _ := time.LoadLocation("America/New_York")
	c := New(WithLocation(ny))

	var n atomic.Int32
	c.MustAdd("a", "30 2 * * *", func() { n.Add(1) })

	// 2026-03-08 02:00 EST doesn't exist: 07:00 UTC is already 03:00 EDT
	start := time.Date(2026, 3, 8, 6, 0, 0, 0, time.UTC) // 01:00 EST
	for i := 0; i < 180; i++ {
		c.runDue(start.Add(time.Duration(i) * time.Minute))
	}

	_ = c.Shutdown(context.Background())

	if n.Load() != 0 {
		t.Fatalf("expected the job to be skipped, got %d runs", n.Load())
	}
}

func TestCronTimezones(t *testing.T) {
	t.Parallel()

	sh, _ := time.LoadLocation("Asia/Shanghai")

	c := New()
	var global, pinned atomic.Int32
	c.MustAdd("global", "0 9 * * *", func() { global.Add(1) })
	c.MustAdd("pinned", "CRON_TZ=Asia/Shanghai 0 9 * * *", func() { pinned.Add(1) })

	at := time.Date(2026, 6, 1, 1, 0, 0, 0, time.UTC) // 09:00 in Shanghai
	c.runDue(at)
	_ = c.Shutdown(context.Background())
	if global.Load() != 0 || pinned.Load() != 1 {
		t.Fatalf("UTC cron: global=%d pinned=%d", global.Load(), pinned.Load())
	}

	c = New(WithLocation(sh))
	global.Store(0)
	c.MustAdd("global", "0 9 * * *", func() { global.Add(1) })
	c.runDue(at)
	_ = c.Shutdown(context.Background())
	if global.Load() != 1 {
		t.Fatalf("Shanghai cron: global=%d", global.Load())
	}
}

func TestCronSetTimezoneResetsDedup(t *testing.T) {
	t.Parallel()

	c := New()
	var n atomic.Int32
	c.MustAdd("a", "* * * * *", func() { n.Add(1) })

	at := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	c.runDue(at)

	// Shanghai is 8h ahead of UTC; going from UTC to Honolulu (-10h) moves the
	// wall clock back by 10h, which must not suppress the jobs.
	hnl, _ := time.LoadLocation("Pacific/Honolulu")
	c.SetTimezone(hnl)
	c.SetTimezone(nil) // ignored
	c.runDue(at.Add(time.Minute))

	_ = c.Shutdown(context.Background())

	if n.Load() != 2 {
		t.Fatalf("expected 2 runs, got %d", n.Load())
	}
}

func TestCronPanicRecovery(t *testing.T) {
	t.Parallel()

	type report struct {
		id    string
		value any
		stack []byte
	}

	reports := make(chan report, 1)
	c := New(WithPanicHandler(func(id string, v any, stack []byte) {
		reports <- report{id, v, stack}
	}))

	var healthy atomic.Int32
	c.MustAdd("bad", "* * * * *", func() { panic("boom") })
	c.MustAdd("good", "* * * * *", func() { healthy.Add(1) })

	c.runDue(time.Now())

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-reports:
		if r.id != "bad" || r.value != "boom" || len(r.stack) == 0 {
			t.Fatalf("unexpected report: %+v", r)
		}
	default:
		t.Fatal("panic handler was not called")
	}

	if healthy.Load() != 1 {
		t.Fatal("the healthy job should not be affected by the panic of the other one")
	}
}

func TestCronDefaultPanicHandlerDoesNotCrash(t *testing.T) {
	t.Parallel()

	c := New()
	c.MustAdd("bad", "* * * * *", func() { panic(errors.New("boom")) })
	c.runDue(time.Now())

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCronSkipIfRunning(t *testing.T) {
	t.Parallel()

	c := New()

	var started atomic.Int32
	release := make(chan struct{})

	c.MustAdd("slow", "* * * * *", func() {
		started.Add(1)
		<-release
	}, SkipIfRunning())

	var overlapping atomic.Int32
	c.MustAdd("overlap", "* * * * *", func() {
		overlapping.Add(1)
		<-release
	})

	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	c.runDue(base)
	c.runDue(base.Add(time.Minute))
	c.runDue(base.Add(2 * time.Minute))

	// replacing the job must not allow it to overlap with the previous run
	c.MustAdd("slow", "* * * * *", func() {
		started.Add(1)
		<-release
	}, SkipIfRunning())
	c.runDue(base.Add(3 * time.Minute))

	// wait for the first runs to actually start
	deadline := time.After(2 * time.Second)
	for started.Load() < 1 || overlapping.Load() < 4 {
		select {
		case <-deadline:
			t.Fatalf("jobs didn't start: started=%d overlapping=%d", started.Load(), overlapping.Load())
		case <-time.After(time.Millisecond):
		}
	}

	if started.Load() != 1 {
		t.Fatalf("SkipIfRunning job must run once while running, got %d", started.Load())
	}
	if overlapping.Load() != 4 {
		t.Fatalf("regular job should overlap, got %d runs", overlapping.Load())
	}

	close(release)
	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	// after the previous run returned it can run again
	c.runDue(base.Add(4 * time.Minute))
	_ = c.Shutdown(context.Background())
	if started.Load() != 2 {
		t.Fatalf("expected the job to run again, got %d", started.Load())
	}
}

func TestCronShutdownWaitsForJobs(t *testing.T) {
	t.Parallel()

	c := New()

	release := make(chan struct{})
	var finished atomic.Bool

	c.MustAdd("slow", "* * * * *", func() {
		<-release
		finished.Store(true)
	})
	c.Start()
	c.runDue(time.Now())

	// ctx expires while the job is still running
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := c.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if c.HasStarted() {
		t.Fatal("Shutdown must stop the ticker")
	}
	if finished.Load() {
		t.Fatal("the job should still be running")
	}

	time.AfterFunc(30*time.Millisecond, func() { close(release) })

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !finished.Load() {
		t.Fatal("Shutdown returned before the job finished")
	}
}

func TestCronLifecycle(t *testing.T) {
	t.Parallel()

	c := New(WithInterval(10 * time.Millisecond))

	if c.HasStarted() {
		t.Fatal("must not be started")
	}

	c.Start()
	if !c.HasStarted() {
		t.Fatal("must be started immediately after Start()")
	}

	c.Start() // restart
	if !c.HasStarted() {
		t.Fatal("must still be started after a restart")
	}

	c.SetInterval(20 * time.Millisecond) // restarts
	if !c.HasStarted() {
		t.Fatal("must still be started after SetInterval()")
	}

	c.Stop()
	if c.HasStarted() {
		t.Fatal("must be stopped")
	}

	c.SetInterval(20 * time.Millisecond) // must not start it
	if c.HasStarted() {
		t.Fatal("SetInterval must not start a stopped cron")
	}

	c.Stop() // idempotent

	// no leaked loop goroutines: every loop must be gone after Shutdown
	c.Start()
	c.Start()
	c.Start()
	done := make(chan error, 1)
	go func() { done <- c.Shutdown(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown didn't return: a loop goroutine leaked")
	}
}

func TestCronIntervalNormalization(t *testing.T) {
	t.Parallel()

	for _, d := range []time.Duration{0, -1, 2 * time.Minute} {
		c := New(WithInterval(d))
		if c.interval != time.Minute {
			t.Fatalf("WithInterval(%v): got %v", d, c.interval)
		}

		c.SetInterval(d)
		if c.interval != time.Minute {
			t.Fatalf("SetInterval(%v): got %v", d, c.interval)
		}
	}

	c := New(WithInterval(5 * time.Second))
	if c.interval != 5*time.Second {
		t.Fatalf("got %v", c.interval)
	}
}

// Real time test: with a short tick interval a "* * * * *" job runs
// once for the current minute - never once per tick.
func TestCronTickerFiresAndDeduplicates(t *testing.T) {
	t.Parallel()

	c := New(WithInterval(20 * time.Millisecond))

	var n atomic.Int32
	c.MustAdd("a", "* * * * *", func() { n.Add(1) })
	c.Start()

	deadline := time.Now().Add(3 * time.Second)
	for n.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	time.Sleep(250 * time.Millisecond) // ~12 more ticks

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	// 2 only if the test happened to straddle a minute boundary
	if got := n.Load(); got < 1 || got > 2 {
		t.Fatalf("expected 1 (or 2 on a minute boundary) runs, got %d", got)
	}
}

// Hammer every public method concurrently; meant to be run with -race.
func TestCronConcurrentUse(t *testing.T) {
	t.Parallel()

	c := New(WithInterval(time.Millisecond))

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

	var ran atomic.Int64

	run(func(i int) { c.Start() })
	run(func(i int) { c.Stop() })
	run(func(i int) { c.SetInterval(time.Duration(1+i%5) * time.Millisecond) })
	run(func(i int) { c.SetTimezone(time.UTC) })
	run(func(i int) { c.HasStarted(); c.Total(); c.Jobs() })
	run(func(i int) {
		c.MustAdd("a", "* * * * *", func() { ran.Add(1) }, SkipIfRunning())
		c.MustAdd("b", "* * * * *", func() { ran.Add(1) })
	})
	run(func(i int) {
		if i%3 == 0 {
			c.Remove("a")
		} else {
			c.RemoveAll()
		}
	})
	run(func(i int) { c.runDue(time.Date(2026, 1, 1, 0, i%60, 0, 0, time.UTC)) })

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
