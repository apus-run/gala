package cron

import (
	"context"
	"testing"
	"time"
)

// TestTaskLifecycle verifies the gala.Task adapter starts and stops the
// scheduler and that Stop is idempotent and safe before Start.
// TestTaskLifecycle 验证 gala.Task 适配器启停调度器，且 Stop 幂等并可先于 Start 调用。
func TestTaskLifecycle(t *testing.T) {
	c := New()
	task := NewTask(c)

	if c.HasStarted() {
		t.Fatal("NewTask must not start the scheduler")
	}

	// Stop before Start must be safe: gala may stop a task whose Start never ran.
	// 先于 Start 调用 Stop 必须安全：gala 可能停止一个 Start 未曾执行的任务。
	if err := task.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() before Start error = %v", err)
	}

	if err := task.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !c.HasStarted() {
		t.Fatal("Start did not start the scheduler")
	}

	if err := task.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if c.HasStarted() {
		t.Fatal("Stop did not stop the scheduler")
	}

	if err := task.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
}

// TestTaskNilCron verifies that a missing scheduler is reported by Start and
// ignored by Stop instead of panicking.
// TestTaskNilCron 验证缺失调度器时 Start 返回错误、Stop 忽略，而不是 panic。
func TestTaskNilCron(t *testing.T) {
	task := NewTask(nil)

	if err := task.Start(context.Background()); err == nil {
		t.Fatal("Start() with nil Cron must return an error")
	}
	if err := task.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() with nil Cron error = %v", err)
	}
}

// TestTaskStopHonorsCtx verifies Stop is bounded by its context while an
// in-flight job keeps running.
// TestTaskStopHonorsCtx 验证运行中的任务未结束时 Stop 受其 context 限时。
func TestTaskStopHonorsCtx(t *testing.T) {
	c := New()

	release := make(chan struct{})
	running := make(chan struct{})
	c.MustAdd("slow", "* * * * *", func() {
		close(running)
		<-release
	})
	c.runDue(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	<-running

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := NewTask(c).Stop(ctx); err == nil {
		t.Fatal("Stop() must report ctx expiry while a job is running")
	}

	close(release)
	if err := NewTask(c).Stop(context.Background()); err != nil {
		t.Fatalf("final Stop() error = %v", err)
	}
}
