package plugin_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apus-run/gala/components/plugin"
)

func rec(log *[]string, name string, err error) plugin.Hook {
	return func(context.Context) error {
		*log = append(*log, name)
		return err
	}
}

func boomPanic(context.Context) error { panic("kaboom") }

// block 返回一个会一直阻塞、直到 release 被关闭的钩子（故意不理会 ctx）。
func block(release <-chan struct{}) plugin.Hook {
	return func(context.Context) error { <-release; return nil }
}

func TestZeroValueRunsNothing(t *testing.T) {
	var l plugin.Lifecycle
	ctx := context.Background()
	for name, run := range map[string]func(context.Context) error{
		"BeforeStart": l.RunBeforeStart, "AfterStart": l.RunAfterStart,
		"BeforeStop": l.RunBeforeStop, "AfterStop": l.RunAfterStop,
	} {
		if err := run(ctx); err != nil {
			t.Errorf("%s on empty lifecycle: %v", name, err)
		}
	}
}

// 启动侧：按登记顺序，遇错即停。
func TestStartHooksInOrderFailFast(t *testing.T) {
	var log []string
	boom := errors.New("boom")
	var l plugin.Lifecycle
	l.BeforeStart(rec(&log, "a", nil))
	l.BeforeStart(rec(&log, "b", boom))
	l.BeforeStart(rec(&log, "c", nil))

	if err := l.RunBeforeStart(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if want := []string{"a", "b"}; !slices.Equal(log, want) { // c 不应执行
		t.Fatalf("ran %v, want %v", log, want)
	}
}

// 停止侧：按登记的反序，全部执行，错误合并。
func TestStopHooksReverseRunAllJoinErrors(t *testing.T) {
	var log []string
	e1, e2 := errors.New("e1"), errors.New("e2")
	var l plugin.Lifecycle
	l.BeforeStop(rec(&log, "a", e1))
	l.BeforeStop(rec(&log, "b", nil))
	l.BeforeStop(rec(&log, "c", e2))

	err := l.RunBeforeStop(context.Background())
	if want := []string{"c", "b", "a"}; !slices.Equal(log, want) {
		t.Fatalf("ran %v, want %v", log, want)
	}
	if !errors.Is(err, e1) || !errors.Is(err, e2) {
		t.Fatalf("err = %v, want both e1 and e2", err)
	}
}

// 钩子里再登记钩子不会死锁，且新钩子不在本轮执行。
func TestHookMayRegisterHooks(t *testing.T) {
	var log []string
	var l plugin.Lifecycle
	l.BeforeStart(func(context.Context) error {
		l.BeforeStart(rec(&log, "late", nil))
		log = append(log, "first")
		return nil
	})
	if err := l.RunBeforeStart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"first"}; !slices.Equal(log, want) {
		t.Fatalf("ran %v, want %v", log, want)
	}
}

// 与 Install 配合：插件在 Install 里挂钩子，宿主随后执行。
type lifeHost struct {
	Life plugin.Lifecycle
	log  []string
}
type lifePlugin struct{}

func (lifePlugin) Name() string { return "life" }
func (lifePlugin) Install(h *lifeHost) error {
	h.Life.AfterStart(rec(&h.log, "warmup", nil))
	h.Life.BeforeStop(rec(&h.log, "drain", nil))
	return nil
}

func TestPluginAttachesHooksInInstall(t *testing.T) {
	h := &lifeHost{}
	ctx := context.Background()
	if err := plugin.Install[*lifeHost](h, lifePlugin{}); err != nil {
		t.Fatal(err)
	}
	_ = h.Life.RunAfterStart(ctx)
	_ = h.Life.RunBeforeStop(ctx)
	if want := []string{"warmup", "drain"}; !slices.Equal(h.log, want) {
		t.Fatalf("log = %v, want %v", h.log, want)
	}
}

func TestLifecycleConcurrent(t *testing.T) {
	var l plugin.Lifecycle
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() { defer wg.Done(); l.AfterStop(func(context.Context) error { return nil }) }()
		go func() { defer wg.Done(); _ = l.RunAfterStop(context.Background()) }()
	}
	wg.Wait()
	if err := l.RunAfterStop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// ---- 隔离：panic、阻塞、BestEffort ----

func TestPanicBecomesErrorAndFailsFast(t *testing.T) {
	var log []string
	var l plugin.Lifecycle
	l.BeforeStart(rec(&log, "a", nil))
	l.BeforeStart(boomPanic)
	l.BeforeStart(rec(&log, "never", nil))

	err := l.RunBeforeStart(context.Background())

	// 错误里有 panic 的值、堆栈，以及阶段和钩子名
	for _, want := range []string{"kaboom", "goroutine", "BeforeStart", "boomPanic"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want it to contain %q", err, want)
		}
	}
	if want := []string{"a"}; !slices.Equal(log, want) {
		t.Fatalf("ran %v, want %v", log, want)
	}
}

func TestStopContinuesAfterPanic(t *testing.T) {
	var log []string
	var l plugin.Lifecycle
	l.AfterStop(rec(&log, "a", nil))
	l.AfterStop(boomPanic)
	l.AfterStop(rec(&log, "c", nil))

	err := l.RunAfterStop(context.Background())

	if err == nil || !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("err = %v, want the panic reported", err)
	}
	if want := []string{"c", "a"}; !slices.Equal(log, want) {
		t.Fatalf("ran %v, want %v", log, want)
	}
}

func TestBlockedHookIsAbandonedAfterTimeout(t *testing.T) {
	release := make(chan struct{})
	defer close(release) // 让被放弃的 goroutine 退出

	l := plugin.Lifecycle{Timeout: 20 * time.Millisecond}
	l.BeforeStart(block(release))

	start := time.Now()
	err := l.RunBeforeStart(context.Background())

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("host was blocked for %v", d)
	}
}

func TestHostContextEndsWaiting(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	var l plugin.Lifecycle // 没有 Timeout，只靠宿主传入的 ctx
	l.BeforeStop(block(release))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := l.RunBeforeStop(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("err = %v after %v, want prompt DeadlineExceeded", err, time.Since(start))
	}
}

// 钩子里调用 runtime.Goexit 既不返回也不 panic，不能让宿主一直等（这里故意不设 Timeout）。
func TestGoexitDoesNotHangHost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var l plugin.Lifecycle
		l.BeforeStart(func(context.Context) error { runtime.Goexit(); return nil })

		err := l.RunBeforeStart(context.Background())
		if err == nil || !strings.Contains(err.Error(), "runtime.Goexit") {
			t.Fatalf("err = %v, want Goexit error", err)
		}
	})
}

// BestEffort：panic、返回错误、超时都不返回给宿主、不中断后续钩子，且被记录而不是静默丢失。
func TestBestEffortFailuresAreLoggedNotReturned(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	release := make(chan struct{})
	defer close(release)

	var log []string
	l := plugin.Lifecycle{Timeout: 20 * time.Millisecond}
	l.BeforeStart(boomPanic, plugin.BestEffort())
	l.BeforeStart(rec(&log, "soft-err", errors.New("meh")), plugin.BestEffort())
	l.BeforeStart(block(release), plugin.BestEffort())
	l.BeforeStart(rec(&log, "critical", nil))

	if err := l.RunBeforeStart(context.Background()); err != nil {
		t.Fatalf("best-effort failures must not reach the host, got %v", err)
	}
	if want := []string{"soft-err", "critical"}; !slices.Equal(log, want) {
		t.Fatalf("ran %v, want %v", log, want)
	}

	out := buf.String()
	for _, want := range []string{"best-effort hook failed", "kaboom", "meh", "abandoned", "boomPanic"} {
		if !strings.Contains(out, want) {
			t.Errorf("log should contain %q, got:\n%s", want, out)
		}
	}
}

// BestEffort 不会掩盖旁边必须成功的钩子的失败。
func TestBestEffortDoesNotMaskCriticalFailure(t *testing.T) {
	boom := errors.New("db down")
	var l plugin.Lifecycle
	l.BeforeStart(boomPanic, plugin.BestEffort())
	l.BeforeStart(func(context.Context) error { return boom })

	if err := l.RunBeforeStart(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want db down", err)
	}
}

func TestCanceledContextDoesNotStartHooks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, stage := range []string{"start", "stop"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var l plugin.Lifecycle
				called := make(chan struct{}, 2)
				fn := func(context.Context) error { called <- struct{}{}; return nil }
				l.BeforeStart(fn)
				l.BeforeStop(fn)
				l.BeforeStop(fn)
				run := l.RunBeforeStart
				if stage == "stop" {
					run = l.RunBeforeStop
				}
				if err := run(ctx); !errors.Is(err, context.Canceled) {
					t.Fatalf("err = %v, want Canceled", err)
				}
				synctest.Wait()
				select {
				case <-called:
					t.Fatal("hook ran with an already canceled context")
				default:
				}
			})
		})
	}
}

func TestStopCancellationSkipsRemainingHooks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var l plugin.Lifecycle
		var log []string
		l.BeforeStop(rec(&log, "skipped-first", nil))
		l.BeforeStop(rec(&log, "skipped-second", nil))
		l.BeforeStop(func(context.Context) error {
			log = append(log, "cancel")
			cancel()
			return nil
		})

		if err := l.RunBeforeStop(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want Canceled", err)
		}
		synctest.Wait()
		if !slices.Equal(log, []string{"cancel"}) {
			t.Fatalf("ran %v, want only the cancel hook", log)
		}
	})
}

func TestGoexitStopsStartup(t *testing.T) {
	var l plugin.Lifecycle
	l.BeforeStart(func(context.Context) error { runtime.Goexit(); return nil })
	ran := false
	l.BeforeStart(func(context.Context) error { ran = true; return nil })
	if err := l.RunBeforeStart(context.Background()); err == nil {
		t.Fatal("want Goexit error")
	}
	if ran {
		t.Fatal("startup continued after Goexit")
	}
}
