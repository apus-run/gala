package mr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestMapReducePreservesCancellationCause(t *testing.T) {
	custom := errors.New("request aborted")
	for _, want := range []error{context.Canceled, context.DeadlineExceeded, custom} {
		t.Run(want.Error(), func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(t.Context())
			cancel(want)
			called := false
			_, err := MapReduce(
				func(chan<- int) { called = true },
				func(int, Writer[int], func(error)) { called = true },
				func(<-chan int, Writer[int], func(error)) { called = true },
				WithContext(ctx),
			)
			if !errors.Is(err, want) || called {
				t.Fatalf("err=%v, want %v; callbacks invoked=%v", err, want, called)
			}
		})
	}
}

func TestGuardedWriterCancellationUnblocksSend(t *testing.T) {
	for _, signal := range []string{"context", "done"} {
		t.Run(signal, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				channel := make(chan int)
				stopped, completed := make(chan struct{}), make(chan struct{})
				writer := newGuardedWriter(ctx, channel, stopped)
				go func() { writer.Write(1); close(completed) }()
				synctest.Wait()
				if signal == "context" {
					cancel()
				} else {
					close(stopped)
				}
				synctest.Wait()
				select {
				case <-completed:
				default:
					t.Error("cancellation did not release the blocked writer")
					<-channel
					<-completed
				}
			})
		})
	}
}

func TestMapReducePreservesRunningCancellationCause(t *testing.T) {
	want := errors.New("request aborted during mapping")
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	_, err := MapReduce(
		func(source chan<- int) { source <- 1 },
		func(_ int, writer Writer[int], _ func(error)) {
			cancel(want)
			writer.Write(1)
		},
		func(pipe <-chan int, writer Writer[int], _ func(error)) {
			drain(pipe)
			writer.Write(0)
		},
		WithContext(ctx),
	)
	if !errors.Is(err, want) {
		t.Fatalf("err=%v, want %v", err, want)
	}
}

func TestMapReduceChanCancelsWithOpenSource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		source := make(chan int)
		returned := make(chan error, 1)
		go func() {
			_, err := MapReduceChan(source,
				func(int, Writer[int], func(error)) {},
				func(pipe <-chan int, _ Writer[int], _ func(error)) { drain(pipe) },
				WithContext(ctx), WithWorkers(1),
			)
			returned <- err
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case err := <-returned:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err=%v, want Canceled", err)
			}
		default:
			t.Error("cancellation waited for caller-owned source to close")
			close(source)
			<-returned
		}
	})
}

func TestMapReduceChanLeavesCanceledInputUntouched(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	source := make(chan int, 2)
	source <- 1
	source <- 2
	_, err := MapReduceChan(source,
		func(int, Writer[int], func(error)) { t.Fatal("mapper invoked") },
		func(<-chan int, Writer[int], func(error)) { t.Fatal("reducer invoked") },
		WithContext(ctx),
	)
	if !errors.Is(err, context.Canceled) || len(source) != 2 {
		t.Fatalf("err=%v input remaining=%d", err, len(source))
	}
}

func TestMapReduceWaitsForStartedCallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		source := make(chan int, 1)
		source <- 1
		close(source)
		started, release := make(chan struct{}), make(chan struct{})
		returned := make(chan error, 1)
		finished := false
		go func() {
			_, err := MapReduceChan(source,
				func(_ int, writer Writer[int], _ func(error)) {
					close(started)
					<-release
					writer.Write(1)
					finished = true
				},
				func(pipe <-chan int, _ Writer[int], _ func(error)) { drain(pipe) },
				WithContext(ctx),
			)
			returned <- err
		}()
		<-started
		cancel()
		synctest.Wait()
		select {
		case err := <-returned:
			t.Errorf("returned before callback completed: %v", err)
		default:
		}
		close(release)
		synctest.Wait()
		select {
		case err := <-returned:
			if !errors.Is(err, context.Canceled) || !finished {
				t.Fatalf("err=%v callback finished=%v", err, finished)
			}
		default:
			t.Fatal("did not return after callback completion")
		}
	})
}

func TestMapReduceDrainsOwnedGenerator(t *testing.T) {
	want := errors.New("mapper failed")
	generated := make(chan struct{})
	_, err := MapReduce(
		func(source chan<- int) {
			defer close(generated)
			for i := range 100 {
				source <- i
			}
		},
		func(_ int, _ Writer[int], cancel func(error)) { cancel(want) },
		func(pipe <-chan int, _ Writer[int], _ func(error)) { drain(pipe) },
		WithWorkers(1),
	)
	if !errors.Is(err, want) {
		t.Fatal(err)
	}
	select {
	case <-generated:
	default:
		t.Fatal("generator still running after return")
	}
}

func TestMapReduceCallbackPanics(t *testing.T) {
	for _, stage := range []string{"generate", "map", "reduce"} {
		t.Run(stage, func(t *testing.T) {
			defer func() {
				if value := recover(); value == nil || !strings.Contains(fmt.Sprint(value), stage+"-panic") {
					t.Fatalf("panic was lost: %v", value)
				}
			}()
			_, _ = MapReduce(
				func(source chan<- int) {
					if stage == "generate" {
						panic("generate-panic")
					}
					source <- 1
				},
				func(item int, writer Writer[int], _ func(error)) {
					if stage == "map" {
						panic("map-panic")
					}
					writer.Write(item)
				},
				func(pipe <-chan int, writer Writer[int], _ func(error)) {
					if stage == "reduce" {
						panic("reduce-panic")
					}
					drain(pipe)
					writer.Write(0)
				},
			)
		})
	}
}

func TestMapReduceOutputContract(t *testing.T) {
	_, err := MapReduce(
		func(chan<- int) {},
		func(int, Writer[int], func(error)) {},
		func(<-chan int, Writer[int], func(error)) {},
	)
	if !errors.Is(err, ErrReduceNoOutput) {
		t.Fatalf("err=%v, want no output", err)
	}
	t.Run("multiple outputs", func(t *testing.T) {
		defer func() {
			if value := recover(); value == nil || !strings.Contains(fmt.Sprint(value), "more than one") {
				t.Fatalf("multiple outputs did not panic: %v", value)
			}
		}()
		_, _ = MapReduce(
			func(chan<- int) {},
			func(int, Writer[int], func(error)) {},
			func(_ <-chan int, writer Writer[int], _ func(error)) { writer.Write(1); writer.Write(2) },
		)
	})
}

func TestMapReduceWorkerLimitAndHelpers(t *testing.T) {
	for _, workers := range []int{0, 1, 3} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var active, peak atomic.Int64
				result, err := MapReduce(
					func(source chan<- int) {
						for i := range 20 {
							source <- i
						}
					},
					func(_ int, writer Writer[int], _ func(error)) {
						n := active.Add(1)
						defer active.Add(-1)
						for previous := peak.Load(); n > previous; previous = peak.Load() {
							if peak.CompareAndSwap(previous, n) {
								break
							}
						}
						time.Sleep(time.Millisecond)
						writer.Write(1)
					},
					func(pipe <-chan int, writer Writer[int], _ func(error)) {
						total := 0
						for value := range pipe {
							total += value
						}
						writer.Write(total)
					}, WithWorkers(workers), WithContext(nil),
				)
				if err != nil || result != 20 || peak.Load() > int64(max(1, workers)) {
					t.Fatalf("result=%d err=%v peak=%d", result, err, peak.Load())
				}
			})
		})
	}
	var count atomic.Int64
	ForEach(func(source chan<- int) {
		for i := range 10 {
			source <- i
		}
	}, func(int) { count.Add(1) }, WithWorkers(2))
	FinishVoid(func() { count.Add(1) }, func() { count.Add(1) })
	if count.Load() != 12 || Finish() != nil {
		t.Fatalf("helper count=%d", count.Load())
	}
	FinishVoid()
}

func TestMapReduceCancelWithNil(t *testing.T) {
	_, err := MapReduce(
		func(source chan<- int) { source <- 1 },
		func(_ int, _ Writer[int], cancel func(error)) { cancel(nil) },
		func(pipe <-chan int, _ Writer[int], _ func(error)) { drain(pipe) },
	)
	if !errors.Is(err, ErrCancelWithNil) {
		t.Fatalf("err=%v, want ErrCancelWithNil", err)
	}
}
