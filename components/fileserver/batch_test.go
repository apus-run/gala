package fileserver_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/fileservertest"
)

func TestUploadAllKeepsIndependentResults(t *testing.T) {
	store := fileservertest.NewMemStore()
	boom := errors.New("open failed")
	closed := &trackedCloser{Reader: bytes.NewReader([]byte("ok"))}
	tasks := []fileserver.UploadTask{
		{Key: "ok", Open: func(context.Context) (io.ReadCloser, error) { return closed, nil }},
		{Key: "bad", Open: func(context.Context) (io.ReadCloser, error) { return nil, boom }},
		{Key: "", Open: nil},
	}
	got := fileserver.UploadAll(t.Context(), store, tasks)
	if len(got) != 3 || got[0].Index != 0 || got[0].Key != "ok" || got[0].Err != nil {
		t.Fatalf("result 0 = %+v", got[0])
	}
	if !errors.Is(got[1].Err, boom) || !errors.Is(got[2].Err, fileserver.ErrInvalidArgument) {
		t.Fatalf("errors = %v, %v", got[1].Err, got[2].Err)
	}
	if !closed.closed.Load() {
		t.Fatal("successful upload did not close its reader")
	}
	body, err := readStored(t, store, "ok")
	if err != nil || string(body) != "ok" {
		t.Fatalf("stored = %q, %v", body, err)
	}
	if _, err := store.Stat(t.Context(), "bad"); !errors.Is(err, fileserver.ErrNotFound) {
		t.Fatal("failed task was published")
	}
}

func TestUploadAllFailFastCancelsTheRest(t *testing.T) {
	store := fileservertest.NewMemStore()
	release := make(chan struct{})
	// 无论测试如何结束，都放行被阻塞的 Open，避免在 fail-fast 失效时泄漏协程。
	defer close(release)
	tasks := []fileserver.UploadTask{
		{Key: "first", Open: func(context.Context) (io.ReadCloser, error) {
			return nil, errors.New("boom")
		}},
		{Key: "second", Open: func(ctx context.Context) (io.ReadCloser, error) {
			select {
			case <-release:
				return io.NopCloser(bytes.NewReader([]byte("x"))), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}},
	}
	done := make(chan []fileserver.Result, 1)
	go func() {
		done <- fileserver.UploadAll(t.Context(), store, tasks, fileserver.WithConcurrency(1), fileserver.WithFailFast())
	}()
	var got []fileserver.Result
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("UploadAll did not return: fail-fast left the second task blocked")
	}
	if got[0].Err == nil || got[0].Err.Error() != "boom" {
		t.Fatalf("first = %v", got[0].Err)
	}
	if !errors.Is(got[1].Err, context.Canceled) {
		t.Fatalf("second = %v, want context.Canceled", got[1].Err)
	}
}

func TestUploadAllLimitsConcurrency(t *testing.T) {
	store := fileservertest.NewMemStore()
	const n = 6
	var current, maxSeen atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, n)
	tasks := make([]fileserver.UploadTask, n)
	for i := range tasks {
		tasks[i] = fileserver.UploadTask{
			Key: "k",
			Open: func(ctx context.Context) (io.ReadCloser, error) {
				now := current.Add(1)
				for {
					seen := maxSeen.Load()
					if now <= seen || maxSeen.CompareAndSwap(seen, now) {
						break
					}
				}
				started <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					current.Add(-1)
					return nil, ctx.Err()
				}
				current.Add(-1)
				return io.NopCloser(bytes.NewReader([]byte("x"))), nil
			},
		}
	}
	done := make(chan []fileserver.Result, 1)
	go func() {
		done <- fileserver.UploadAll(t.Context(), store, tasks, fileserver.WithConcurrency(2))
	}()
	for range 2 {
		<-started
	}
	if maxSeen.Load() > 2 {
		t.Fatalf("in flight = %d, want at most 2", maxSeen.Load())
	}
	close(release)
	got := <-done
	if len(got) != n {
		t.Fatalf("len = %d", len(got))
	}
	for i, result := range got {
		if result.Err != nil || result.Index != i {
			t.Fatalf("result %d = %+v", i, result)
		}
	}
	if maxSeen.Load() > 2 {
		t.Fatalf("peak concurrency = %d", maxSeen.Load())
	}
}

func TestDownloadAllWritesEachObject(t *testing.T) {
	store := fileservertest.NewMemStore()
	if err := store.Upload(t.Context(), "a", bytes.NewReader([]byte("alpha"))); err != nil {
		t.Fatal(err)
	}
	if err := store.Upload(t.Context(), "b", bytes.NewReader([]byte("beta"))); err != nil {
		t.Fatal(err)
	}
	var a, b fileservertest.BufferAt
	got := fileserver.DownloadAll(t.Context(), store, []fileserver.DownloadTask{
		{Key: "missing", W: &fileservertest.BufferAt{}},
		{Key: "a", W: &a, Opts: []fileserver.DownloadOpt{fileserver.WithRange(1, 3)}},
		{Key: "b", W: &b},
		{Key: "nil"},
	})
	if !errors.Is(got[0].Err, fileserver.ErrNotFound) || got[1].Err != nil || got[2].Err != nil || !errors.Is(got[3].Err, fileserver.ErrInvalidArgument) {
		t.Fatalf("results = %+v", got)
	}
	// Download 把对象字节 i 写到 writer 的偏移 i，范围从 1 开始的 3 字节落在下标 1..3。
	raw := a.Bytes()
	if len(raw) < 4 || string(raw[1:4]) != "lph" {
		t.Fatalf("ranged download = %q", raw)
	}
	if string(b.Bytes()) != "beta" {
		t.Fatalf("b = %q", b.Bytes())
	}
}

func TestBatchRejectsBadConcurrencyWithoutStarting(t *testing.T) {
	var opened atomic.Int32
	tasks := []fileserver.UploadTask{{
		Key: "k",
		Open: func(context.Context) (io.ReadCloser, error) {
			opened.Add(1)
			return io.NopCloser(bytes.NewReader(nil)), nil
		},
	}}
	got := fileserver.UploadAll(t.Context(), fileservertest.NewMemStore(), tasks, fileserver.WithConcurrency(0), nil)
	if !errors.Is(got[0].Err, fileserver.ErrInvalidArgument) || opened.Load() != 0 {
		t.Fatalf("result = %+v, opened %d", got[0], opened.Load())
	}
	if len(fileserver.UploadAll(t.Context(), fileservertest.NewMemStore(), nil)) != 0 {
		t.Fatal("empty task list should return an empty result")
	}
	if got := fileserver.UploadAll(t.Context(), nil, tasks); !errors.Is(got[0].Err, fileserver.ErrInvalidArgument) || opened.Load() != 0 {
		t.Fatalf("nil uploader = %+v", got[0])
	}
}

type trackedCloser struct {
	io.Reader
	closed atomic.Bool
}

func (t *trackedCloser) Close() error {
	t.closed.Store(true)
	return nil
}

func readStored(t *testing.T, s fileserver.Storage, key string) ([]byte, error) {
	t.Helper()
	rc, err := s.Read(context.Background(), key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

// uploadFunc adapts a function to fileserver.Uploader.
type uploadFunc func(context.Context, string, io.Reader, ...fileserver.UploadOpt) error

func (f uploadFunc) Upload(ctx context.Context, key string, r io.Reader, opts ...fileserver.UploadOpt) error {
	return f(ctx, key, r, opts...)
}

// TestUploadAllBoundsConcurrencyAcrossManyTasks pins the batch semaphore. The
// existing concurrency test releases the workers before it finishes measuring,
// so replacing the semaphore with an unbounded channel still passes. This test
// measures the peak over the whole run instead.
func TestUploadAllBoundsConcurrencyAcrossManyTasks(t *testing.T) {
	const (
		tasks       = 512
		concurrency = 4
	)
	var current, peak atomic.Int64
	u := uploadFunc(func(context.Context, string, io.Reader, ...fileserver.UploadOpt) error {
		n := current.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		current.Add(-1)
		return nil
	})

	list := make([]fileserver.UploadTask, tasks)
	for i := range list {
		list[i] = fileserver.UploadTask{
			Key:  fmt.Sprintf("k%d", i),
			Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(nil)), nil },
		}
	}
	results := fileserver.UploadAll(t.Context(), u, list, fileserver.WithConcurrency(concurrency))
	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("result %d: %v", r.Index, r.Err)
		}
	}
	if got := peak.Load(); got > concurrency {
		t.Fatalf("peak concurrency = %d, want at most %d", got, concurrency)
	}
	if got := peak.Load(); got < 2 {
		t.Fatalf("peak concurrency = %d: the test never exercised the limit", got)
	}
}
