package rds

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apus-run/gala/components/mq"
	"github.com/redis/go-redis/v9"
)

// Hold replies only after XADD has actually completed on Redis. Close must wait
// for these accepted calls, even though their operation contexts are canceled.
type lifecycleReplyBarrier struct {
	entered chan struct{}
	release <-chan struct{}
}

func (h *lifecycleReplyBarrier) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *lifecycleReplyBarrier) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *lifecycleReplyBarrier) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if cmd.Name() == "xadd" && err == nil {
			h.entered <- struct{}{}
			<-h.release
		}
		return err
	}
}

func TestConcurrentSendCloseReleasesCallbacksAndSlots(t *testing.T) {
	// A broken WaitGroup cannot be forcibly unwound in-process. Isolate the
	// scenario so failed Close calls cannot leak goroutines into subsequent tests.
	if os.Getenv("RDS_LIFECYCLE_CHILD") != "1" {
		for _, mode := range []string{"sync_io", "async_callback"} {
			t.Run(mode, func(t *testing.T) {
				client, topic := setup(t)
				exe, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, exe, "-test.run=^TestConcurrentSendCloseReleasesCallbacksAndSlots$", "-test.timeout=15s")
				cmd.Env = append(os.Environ(), "RDS_LIFECYCLE_CHILD=1", "RDS_LIFECYCLE_MODE="+mode, "RDS_LIFECYCLE_ADDR="+client.Options().Addr, "RDS_LIFECYCLE_TOPIC="+topic)
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("lifecycle subprocess: %v (context: %v)\n%s", err, ctx.Err(), output)
				}
			})
		}
		return
	}
	// Parent owns key cleanup and keeps Redis alive until this process exits.
	client := redis.NewClient(&redis.Options{Addr: os.Getenv("RDS_LIFECYCLE_ADDR"), ContextTimeoutEnabled: true, MaxRetries: -1, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second})
	defer client.Close()
	topic := os.Getenv("RDS_LIFECYCLE_TOPIC")
	const sends = 16
	async := os.Getenv("RDS_LIFECYCLE_MODE") == "async_callback"
	ioEntered := make(chan struct{}, sends)
	ioRelease, callbackRelease := make(chan struct{}), make(chan struct{})
	var ioOnce, callbackOnce sync.Once
	releaseIO := func() { ioOnce.Do(func() { close(ioRelease) }) }
	releaseCallbacks := func() { callbackOnce.Do(func() { close(callbackRelease) }) }
	// Release both barriers even on Fatal. Never synchronously
	// retry Close in cleanup: a regression may have leaked its WaitGroup counter.
	defer releaseIO()
	defer releaseCallbacks()
	client.AddHook(&lifecycleReplyBarrier{entered: ioEntered, release: ioRelease})
	raw, err := factory(t, client).NewProducer(mq.ProducerConfig{ProduceTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	p := raw.(*Producer)
	defer p.cancel()
	var calls [sends]atomic.Int32
	callbackEntered := make(chan struct{}, sends)
	results := make(chan error, sends)
	for i := 0; i < sends; i++ {
		if async {
			go func() {
				err := p.SendAsync(context.Background(), func(_ context.Context, r mq.SendResponse, e error) {
					calls[i].Add(1)
					results <- e
					callbackEntered <- struct{}{}
					<-callbackRelease
					if i%2 == 0 {
						panic("lifecycle regression: callback panic")
					}
				}, mq.NewMessage(topic, nil))
				if err != nil {
					results <- err
				}
			}()
		} else {
			go func() { _, err := p.Send(context.Background(), mq.NewMessage(topic, nil)); results <- err }()
		}
	}
	wait := func(ch <-chan struct{}, what string) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for " + what)
		}
	}
	for range sends {
		wait(ioEntered, "actual XADD completion")
	}
	if n, err := client.XLen(context.Background(), StreamKey(topic)).Result(); err != nil || n != sends {
		t.Fatalf("I/O barrier did not cover every send: length=%d err=%v", n, err)
	}
	// In the callback scenario all I/O is finished before Close starts; no
	// synchronous send or blocked command can hide a missing callback wait.
	if async {
		releaseIO()
		for range sends {
			wait(callbackEntered, "accepted callback")
		}
	}
	closed := make(chan struct{}, 3)
	for range 3 {
		go func() { p.Close(); closed <- struct{}{} }()
	}
	wait(p.life.Done(), "Close cancellation")
	assertWaiting := func(stage string) {
		t.Helper()
		select {
		case <-closed:
			t.Fatal("Close returned before " + stage)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if async {
		assertWaiting("callbacks were released")
		releaseCallbacks()
	} else {
		// There are no async sends in this scenario: only synchronous I/O can
		// keep Close blocked.
		assertWaiting("synchronous I/O replies were released")
		releaseIO()
	}
	for range 3 {
		wait(closed, "concurrent Close completion")
	}
	for range sends {
		select {
		case err := <-results:
			if async && err != nil {
				t.Fatalf("completed async publish: %v", err)
			}
			if !async && !errors.Is(err, context.Canceled) {
				t.Fatalf("in-flight sync send must report Close cancellation: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("missing send result")
		}
	}
	for i := range calls {
		want := int32(0)
		if async {
			want = 1
		}
		if n := calls[i].Load(); n != want {
			t.Fatalf("callback %d ran %d times", i, n)
		}
	}
	if len(p.slots) != 0 {
		t.Fatal("async slot leaked")
	}
	var rejectedCallback atomic.Int32
	if err := p.SendAsync(context.Background(), func(context.Context, mq.SendResponse, error) { rejectedCallback.Add(1) }, mq.NewMessage(topic, nil)); !errors.Is(err, ErrClosed) {
		t.Fatalf("late async: %v", err)
	}
	if _, err := p.Send(context.Background(), mq.NewMessage(topic, nil)); !errors.Is(err, ErrClosed) {
		t.Fatalf("late sync: %v", err)
	}
	if rejectedCallback.Load() != 0 {
		t.Fatal("rejected send invoked callback")
	}
	go func() { p.Close(); closed <- struct{}{} }()
	wait(closed, "repeated Close")
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatal("borrowed client closed:", err)
	}
}

// Check the test hook against the client interface.
var _ redis.Hook = (*lifecycleReplyBarrier)(nil)
