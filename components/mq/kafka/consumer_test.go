package kafka

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apus-run/gala/components/mq"
	"github.com/twmb/franz-go/pkg/kgo"
)

type handlerFunc func(context.Context, *mq.MessageExt) error

func (f handlerFunc) HandleMessage(ctx context.Context, m *mq.MessageExt) error { return f(ctx, m) }

type fakeConsumer struct {
	poll   func(context.Context, int) kgo.Fetches
	commit func(context.Context, ...*kgo.Record) error
	seek   func(map[string]map[int32]kgo.EpochOffset)
	allow  func()
	close  func()
}

func (f *fakeConsumer) PollRecords(ctx context.Context, n int) kgo.Fetches { return f.poll(ctx, n) }
func (f *fakeConsumer) CommitRecords(ctx context.Context, rs ...*kgo.Record) error {
	return f.commit(ctx, rs...)
}
func (f *fakeConsumer) SetOffsets(os map[string]map[int32]kgo.EpochOffset) { f.seek(os) }
func (f *fakeConsumer) AllowRebalance() {
	if f.allow != nil {
		f.allow()
	}
}
func (f *fakeConsumer) CloseAllowingRebalance() {
	if f.close != nil {
		f.close()
	}
}

func TestCommitOnlyAfterSuccess(t *testing.T) {
	failure := errors.New("handler failed")
	for _, mode := range []string{"success", "error", "panic", "canceled", "commit_error"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			commits, handled := 0, false
			client := &fakeConsumer{commit: func(context.Context, ...*kgo.Record) error {
				if !handled {
					t.Fatal("commit preceded processing")
				}
				commits++
				if mode == "commit_error" {
					return failure
				}
				return nil
			}}
			c := &Consumer{client: client, timeout: time.Second}
			err := c.process(ctx, handlerFunc(func(ctx context.Context, m *mq.MessageExt) error {
				handled = true
				switch mode {
				case "error":
					return failure
				case "panic":
					panic("boom")
				case "canceled":
					cancel()
				}
				return nil
			}), &kgo.Record{Topic: "events", Offset: 4})
			wantCommits := 0
			if mode == "success" || mode == "commit_error" {
				wantCommits = 1
			}
			if commits != wantCommits {
				t.Fatalf("commits=%d want=%d", commits, wantCommits)
			}
			if (err != nil) != (mode != "success") {
				t.Fatalf("error=%v", err)
			}
			if (mode == "error" || mode == "commit_error") && !errors.Is(err, failure) {
				t.Fatalf("error lost: %v", err)
			}
		})
	}
}

func TestConsumerRetriesBeforeAdvancing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var next int64
		var attempts, commits []int64
		var events []string
		closed := 0
		client := &fakeConsumer{
			poll: func(ctx context.Context, n int) kgo.Fetches {
				if n != 1 {
					panic("poll must be bounded to one record")
				}
				if next == 2 {
					<-ctx.Done()
					return nil
				}
				r := &kgo.Record{Topic: "events", Offset: next}
				next++
				return kgo.Fetches{{Topics: []kgo.FetchTopic{{Topic: "events", Partitions: []kgo.FetchPartition{{Records: []*kgo.Record{r}}}}}}}
			},
			commit: func(_ context.Context, rs ...*kgo.Record) error {
				commits = append(commits, rs[0].Offset)
				events = append(events, "commit")
				return nil
			},
			seek: func(os map[string]map[int32]kgo.EpochOffset) {
				next = os["events"][0].Offset
				events = append(events, "rewind")
			},
			allow: func() { events = append(events, "allow") },
			close: func() { closed++ },
		}
		c := &Consumer{client: client, timeout: time.Second}
		c.RegisterHandler(handlerFunc(func(_ context.Context, m *mq.MessageExt) error {
			if m.MsgID == "events:0:0" {
				attempts = append(attempts, 0)
			} else {
				attempts = append(attempts, 1)
			}
			if len(attempts) == 1 {
				return errors.New("retry me")
			}
			return nil
		}))
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(attempts, []int64{0, 0, 1}) {
			t.Fatalf("attempts=%v", attempts)
		}
		if !reflect.DeepEqual(commits, []int64{0, 1}) {
			t.Fatalf("commits=%v", commits)
		}
		if !reflect.DeepEqual(events, []string{"rewind", "allow", "commit", "allow", "commit", "allow"}) {
			t.Fatalf("events=%v", events)
		}
		if closed != 1 {
			t.Fatalf("closed=%d", closed)
		}
		if err := c.Start(); !errors.Is(err, ErrClosed) {
			t.Fatalf("restart=%v", err)
		}
	})
}

func TestConsumerCloseCancelsHandler(t *testing.T) {
	entered := make(chan struct{})
	client := &fakeConsumer{
		poll: func(context.Context, int) kgo.Fetches {
			return kgo.Fetches{{Topics: []kgo.FetchTopic{{Partitions: []kgo.FetchPartition{{Records: []*kgo.Record{{Topic: "events"}}}}}}}}
		},
		commit: func(context.Context, ...*kgo.Record) error { panic("canceled message committed") },
		seek:   func(map[string]map[int32]kgo.EpochOffset) {},
	}
	c := &Consumer{client: client, timeout: time.Hour}
	c.RegisterHandler(handlerFunc(func(ctx context.Context, _ *mq.MessageExt) error { close(entered); <-ctx.Done(); return ctx.Err() }))
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	<-entered
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { c.Close() })
	}
	wg.Wait()
}

func TestConsumerTerminalError(t *testing.T) {
	failure := errors.New("fetch failed")
	c := &Consumer{client: &fakeConsumer{poll: func(context.Context, int) kgo.Fetches {
		return kgo.Fetches{{Topics: []kgo.FetchTopic{{Partitions: []kgo.FetchPartition{{Err: failure}}}}}}
	}}, timeout: time.Second}
	c.RegisterHandler(handlerFunc(func(context.Context, *mq.MessageExt) error { t.Error("unexpected handler"); return nil }))
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	<-c.done
	if err := c.Close(); !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
}

func TestConsumerCommitPolicy(t *testing.T) {
	consumer, err := NewFactory().NewConsumer(mq.ConsumerConfig{Addr: []string{"127.0.0.1:1"}, Topic: "events", ConsumerGroup: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	client := consumer.(*Consumer).client.(*kgo.Client)
	if client.OptValue(kgo.DisableAutoCommit) != true {
		t.Fatal("automatic commits enabled")
	}
	if client.OptValue(kgo.BlockRebalanceOnPoll) != true {
		t.Fatal("rebalance not blocked during processing")
	}
}

func TestConsumerTimeoutDoesNotCommit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &Consumer{timeout: time.Second, client: &fakeConsumer{commit: func(context.Context, ...*kgo.Record) error {
			t.Fatal("timed out handler committed")
			return nil
		}}}
		err := c.process(context.Background(), handlerFunc(func(ctx context.Context, _ *mq.MessageExt) error {
			<-ctx.Done()
			return nil // Even a handler that returns nil after the deadline cannot acknowledge.
		}), &kgo.Record{Topic: "events"})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestConsumerCloseBeforeStart(t *testing.T) {
	closed := 0
	c := &Consumer{client: &fakeConsumer{close: func() { closed++ }}}
	if err := c.Start(); err == nil {
		t.Fatal("missing handler accepted")
	}
	for range 2 {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if closed != 1 {
		t.Fatalf("closed=%d", closed)
	}
	if err := c.Start(); !errors.Is(err, ErrClosed) {
		t.Fatalf("start after close=%v", err)
	}
}
