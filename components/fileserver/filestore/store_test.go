package filestore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/fileservertest"
)

// localCaps 声明本地实现的能力：不持久化内容类型与元数据，支持 IfNotExists（link 发布）。
var localCaps = fileservertest.Capabilities{IfNotExists: true, ModTime: true}

func newStore(t *testing.T, opts ...Option) *Store {
	t.Helper()
	s, err := New(t.TempDir(), opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStorageSuite(t *testing.T) {
	fileservertest.RunStorageSuite(t, func(t *testing.T) fileserver.Storage {
		return newStore(t)
	}, localCaps)
}

func TestSuiteWithoutSyncStillSatisfiesContract(t *testing.T) {
	fileservertest.RunStorageSuite(t, func(t *testing.T) fileserver.Storage {
		return newStore(t, WithDurability(SyncNone))
	}, localCaps)
}

func TestNewRejectsBadRoots(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "plain-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := New(filepath.Join(dir, "missing")); err == nil {
		t.Error("New accepted a missing root; it must not create directories")
	} else if strings.Contains(err.Error(), dir) {
		t.Errorf("error %q leaks the absolute path", err)
	}
	if _, err := New(file); err == nil {
		t.Error("New accepted a regular file as root")
	} else if !strings.Contains(err.Error(), "root is not a directory") {
		t.Errorf("New(file) error = %q, want the root-is-not-a-directory error", err)
	}
}

func TestNewRejectsSymlinkedInternalDirectories(t *testing.T) {
	for _, dir := range []string{dataDirName, tempDirName} {
		t.Run(dir, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "real"), 0o755); err != nil {
				t.Fatal(err)
			}
			if dir == tempDirName {
				if err := os.Mkdir(filepath.Join(root, dataDirName), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink("real", filepath.Join(root, dir)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if _, err := New(root); err == nil {
				t.Fatalf("New accepted %s as a symbolic link", dir)
			}
		})
	}
}

func TestNewValidatesOptions(t *testing.T) {
	dir := t.TempDir()
	bad := []Option{
		WithDurability(Durability(42)),
		WithFileMode(0o10644),
		WithDirMode(0o1000 | 0o755),
		WithMaxObjectSize(-1),
		WithBufferSize(0),
		WithBufferSize(maxBufferSize + 1),
	}
	for i, opt := range bad {
		if _, err := New(dir, opt); !errors.Is(err, fileserver.ErrInvalidArgument) {
			t.Errorf("option %d: New error = %v, want ErrInvalidArgument", i, err)
		}
	}
}

func TestNewAcceptsNilOption(t *testing.T) {
	s, err := New(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New with nil option: %v", err)
	}
	_ = s.Close()
}

func TestCloseSemantics(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.Upload(ctx, "k", bytes.NewReader([]byte("data"))); err != nil {
		t.Fatal(err)
	}
	rc, err := s.Read(ctx, "k")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := s.Upload(ctx, "k2", bytes.NewReader([]byte("x"))); !errors.Is(err, ErrClosed) {
		t.Fatalf("Upload after Close = %v, want ErrClosed", err)
	}
	if _, err := s.Stat(ctx, "k"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Stat after Close = %v, want ErrClosed", err)
	}

	// 已打开的流不属于 Store 的方法调用，可以读完并关闭。
	data := make([]byte, 4)
	if _, err := rc.Read(data); err != nil || string(data) != "data" {
		t.Fatalf("stream opened before Close: %q, %v", data, err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("closing stream: %v", err)
	}
}

func TestCloseWaitsForInFlightUpload(t *testing.T) {
	s := newStore(t)
	blocking := fileservertest.NewBlockingReader(bytes.NewReader([]byte("in flight")))
	uploaded := make(chan error, 1)
	go func() {
		uploaded <- s.Upload(context.Background(), "k", blocking)
	}()
	<-blocking.Started()

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case <-closed:
		t.Fatal("Close returned while an upload was in flight")
	case <-time.After(50 * time.Millisecond):
	}

	blocking.Release()
	if err := <-uploaded; err != nil {
		t.Fatalf("in-flight upload: %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestConcurrentWritersNeverMixContent(t *testing.T) {
	s := newStore(t)
	const writers = 6
	payloads := make([][]byte, writers)
	for i := range payloads {
		payloads[i] = bytes.Repeat([]byte{byte('A' + i)}, 1<<20)
	}
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Upload(context.Background(), "same", bytes.NewReader(payloads[i])); err != nil {
				t.Errorf("writer %d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	var buf fileservertest.BufferAt
	if err := s.Download(context.Background(), "same", &buf); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()
	for _, p := range payloads {
		if bytes.Equal(got, p) {
			return
		}
	}
	t.Fatalf("object is not equal to any single writer's payload (%d bytes)", len(got))
}

func TestCleanupTempKeepsRecentEntries(t *testing.T) {
	s := newStore(t)
	recent := filepath.Join(s.root.Name(), tempDirName, uploadPrefix+"recent")
	if err := os.WriteFile(recent, []byte("in progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := s.CleanupTemp(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatalf("CleanupTemp removed %d recent entries", removed)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("recent temporary file was removed: %v", err)
	}
}

// TestUploadProgressCallbackRunsOutsideStoreLock pins the Progress contract:
// callbacks must not run while the store holds its read lock. A callback that
// closes the store used to deadlock against Close's write lock.
func TestUploadProgressCallbackRunsOutsideStoreLock(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	finished := make(chan error, 1)
	go func() {
		finished <- s.Upload(context.Background(), "k", bytes.NewReader([]byte("data")),
			fileserver.WithProgress(fileserver.ProgressFunc(func(context.Context, fileserver.ProgressInfo) {
				closed <- s.Close()
			})))
	}()

	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close in progress callback: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("progress callback deadlocked with Store lock")
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("Upload: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Upload did not return after progress callback")
	}
}

// TestDownloadProgressCallbackRunsOutsideStoreLock is the Download counterpart
// of the test above.
func TestDownloadProgressCallbackRunsOutsideStoreLock(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(context.Background(), "k", bytes.NewReader([]byte("data"))); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	finished := make(chan error, 1)
	go func() {
		finished <- s.Download(context.Background(), "k", &fileservertest.BufferAt{},
			fileserver.WithProgress(fileserver.ProgressFunc(func(context.Context, fileserver.ProgressInfo) {
				closed <- s.Close()
			})))
	}()

	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close in progress callback: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("progress callback deadlocked with Store lock")
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("Download: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Download did not return after progress callback")
	}
}

type readCountingReader struct {
	r     io.Reader
	reads int
}

func (r *readCountingReader) Read(p []byte) (int, error) {
	r.reads++
	return r.r.Read(p)
}

// TestIfNotExistsUnsupportedBeforeReading pins the "unsupported options fail
// before consuming the input" contract on filesystems without hard links.
func TestIfNotExistsUnsupportedBeforeReading(t *testing.T) {
	s := newStore(t)
	s.links = linkUnsupported
	r := &readCountingReader{r: bytes.NewReader([]byte("data"))}
	err := s.Upload(context.Background(), "k", r, fileserver.WithIfNotExists())
	if !errors.Is(err, fileserver.ErrUnsupported) {
		t.Fatalf("Upload error = %v, want ErrUnsupported", err)
	}
	if r.reads != 0 {
		t.Fatalf("Upload read the input %d time(s) before rejecting the option", r.reads)
	}
	if _, err := s.Stat(context.Background(), "k"); !errors.Is(err, fileserver.ErrNotFound) {
		t.Fatalf("object was created: %v", err)
	}
}

func TestIfNotExistsDeniedBeforeReading(t *testing.T) {
	s := newStore(t)
	s.links = linkDenied
	r := &readCountingReader{r: bytes.NewReader([]byte("data"))}
	err := s.Upload(context.Background(), "k", r, fileserver.WithIfNotExists())
	if !errors.Is(err, fileserver.ErrPermission) {
		t.Fatalf("Upload error = %v, want ErrPermission", err)
	}
	if r.reads != 0 {
		t.Fatalf("Upload read the input %d time(s) before rejecting the option", r.reads)
	}
}
