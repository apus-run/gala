package filestore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/fileservertest"
)

// withHook 只供测试使用：在命名步骤注入错误。它是 Store 的私有配置，外部包无法构造。
func withHook(h func(point string) error) Option {
	return func(c *config) { c.hook = h }
}

// failAt 返回一个钩子：仅在 point 处返回 err。
func failAt(point string, err error) func(string) error {
	return func(p string) error {
		if p == point {
			return err
		}
		return nil
	}
}

func tempEntries(t *testing.T, s *Store) []string {
	t.Helper()
	f, err := s.root.Open(tempDirName)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	names, err := f.Readdirnames(-1)
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestFailedPublishLeavesNoTemporaryFile(t *testing.T) {
	boom := errors.New("injected before publish")
	s := newStore(t, withHook(failAt("upload.before-publish", boom)))
	ctx := context.Background()
	err := s.Upload(ctx, "obj", bytes.NewReader([]byte("new")))
	if !errors.Is(err, boom) {
		t.Fatalf("Upload error = %v, want injected error", err)
	}
	if _, err := s.Stat(ctx, "obj"); !errors.Is(err, fileserver.ErrNotFound) {
		t.Fatalf("object became visible after a failed publish: %v", err)
	}
	if names := tempEntries(t, s); len(names) != 0 {
		t.Fatalf("temporary files left behind: %v", names)
	}
}

func TestFailedPublishKeepsPreviousVersion(t *testing.T) {
	boom := errors.New("injected before publish")
	dir := t.TempDir()
	first, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Upload(context.Background(), "obj", bytes.NewReader([]byte("old version"))); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()

	s, err := New(dir, withHook(failAt("upload.before-publish", boom)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Upload(context.Background(), "obj", bytes.NewReader([]byte("new version"))); !errors.Is(err, boom) {
		t.Fatalf("Upload error = %v", err)
	}
	rc, err := s.Read(context.Background(), "obj")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	got := make([]byte, 64)
	n, _ := rc.Read(got)
	if string(got[:n]) != "old version" {
		t.Fatalf("previous version changed to %q", got[:n])
	}
}

func TestNewSyncsTheStoreRoot(t *testing.T) {
	var sawRoot bool
	s := newStore(t, withHook(func(point string) error {
		if point == "sync:." {
			sawRoot = true
		}
		return nil
	}))
	if !sawRoot {
		t.Fatal("New returned without syncing the store root")
	}
	if err := s.Upload(context.Background(), "k", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
}

func TestSyncNoneDoesNotSyncTheStoreRoot(t *testing.T) {
	var sawRoot bool
	_ = newStore(t, WithDurability(SyncNone), withHook(func(point string) error {
		if point == "sync:." {
			sawRoot = true
		}
		return nil
	}))
	if sawRoot {
		t.Fatal("SyncNone synced the store root")
	}
}

func TestCancelDuringCommitDoesNotPublish(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newStore(t, withHook(func(point string) error {
		if point == "upload.after-sync" {
			cancel()
		}
		return nil
	}))
	err := s.Upload(ctx, "k", bytes.NewReader([]byte("x")))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Upload error = %v, want context.Canceled", err)
	}
	if _, err := s.Stat(context.Background(), "k"); !errors.Is(err, fileserver.ErrNotFound) {
		t.Fatalf("cancelled upload became visible: %v", err)
	}
}

func TestTemporaryFileStaysPrivateUntilAfterSync(t *testing.T) {
	if !supportsPermissionBits() {
		t.Skip("permission bits are not meaningful on this platform")
	}
	var s *Store
	var got fs.FileMode
	s = newStore(t, WithFileMode(0o640), withHook(func(point string) error {
		if point != "upload.after-sync" {
			return nil
		}
		f, err := s.root.Open(tempDirName)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		names, err := f.Readdirnames(-1)
		if err != nil {
			return err
		}
		for _, name := range names {
			if !strings.HasPrefix(name, uploadPrefix) {
				continue
			}
			fi, err := s.root.Lstat(filepath.Join(tempDirName, name))
			if err != nil {
				return err
			}
			got = fi.Mode().Perm()
		}
		return nil
	}))
	if err := s.Upload(context.Background(), "mode", bytes.NewReader([]byte("m"))); err != nil {
		t.Fatal(err)
	}
	if got != tempFileMode.Perm() {
		t.Fatalf("mode during sync = %o, want %o", got, tempFileMode.Perm())
	}
	fi, err := s.root.Stat(keyPath("mode"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Fatalf("published mode = %o, want 0640", fi.Mode().Perm())
	}
}

func TestRemoveCannotDeleteAVersionItHasNotObserved(t *testing.T) {
	var s *Store
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	saw := make(chan struct{})
	var once sync.Once
	var arm atomic.Bool
	published := make(chan struct{})
	s = newStore(t, WithDurability(SyncNone), withHook(func(point string) error {
		switch point {
		case "remove.after-stat":
			once.Do(func() { close(saw) })
			<-release
		case "upload.before-publish":
			if arm.Load() {
				close(published)
			}
		}
		return nil
	}))
	ctx := context.Background()
	if err := s.Upload(ctx, "k", bytes.NewReader([]byte("old"))); err != nil {
		t.Fatal(err)
	}
	arm.Store(true)

	removeDone := make(chan error, 1)
	go func() {
		removeDone <- s.Remove(ctx, "k")
	}()
	select {
	case <-saw:
	case <-time.After(2 * time.Second):
		t.Fatal("Remove did not reach its stat point")
	}

	uploadDone := make(chan error, 1)
	go func() {
		uploadDone <- s.Upload(ctx, "k", bytes.NewReader([]byte("new")))
	}()
	select {
	case <-published:
		t.Fatal("Upload published while Remove still held the key")
	case <-time.After(150 * time.Millisecond):
	}
	close(release)

	if err := <-removeDone; err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := <-uploadDone; err != nil {
		t.Fatalf("Upload: %v", err)
	}
	rc, err := s.Read(ctx, "k")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("object = %q, want the upload that waited for Remove", got)
	}
}

func TestDirectorySyncFailureIsIndeterminate(t *testing.T) {
	boom := errors.New("injected directory sync failure")
	s := newStore(t, withHook(failAt("upload.before-dir-sync", boom)))
	ctx := context.Background()
	err := s.Upload(ctx, "obj", bytes.NewReader([]byte("visible")))
	if !errors.Is(err, fileserver.ErrIndeterminate) || !errors.Is(err, boom) {
		t.Fatalf("Upload error = %v, want ErrIndeterminate wrapping the cause", err)
	}
	info, err := s.Stat(ctx, "obj")
	if err != nil || info.FSize != int64(len("visible")) {
		t.Fatalf("object should be visible after an indeterminate result: %v, %+v", err, info)
	}
}

func TestMaxObjectSize(t *testing.T) {
	s := newStore(t, WithMaxObjectSize(8))
	ctx := context.Background()

	err := s.Upload(ctx, "big", bytes.NewReader(make([]byte, 9)))
	if !errors.Is(err, fileserver.ErrTooLarge) {
		t.Fatalf("unknown-size upload over limit = %v, want ErrTooLarge", err)
	}
	if _, err := s.Stat(ctx, "big"); !errors.Is(err, fileserver.ErrNotFound) {
		t.Fatalf("oversized object became visible: %v", err)
	}
	err = s.Upload(ctx, "declared", bytes.NewReader(make([]byte, 9)), fileserver.WithSize(9))
	if !errors.Is(err, fileserver.ErrTooLarge) {
		t.Fatalf("declared size over limit = %v, want ErrTooLarge", err)
	}
	if err := s.Upload(ctx, "fits", bytes.NewReader(make([]byte, 8))); err != nil {
		t.Fatalf("object at the limit rejected: %v", err)
	}
}

func TestUploadRejectsParentThatIsAFile(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.Upload(ctx, "file", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	err := s.Upload(ctx, "file/child", bytes.NewReader([]byte("y")))
	if !errors.Is(err, fileserver.ErrConflict) {
		t.Fatalf("Upload below a file = %v, want ErrConflict", err)
	}
}

func TestUploadRejectsTargetThatIsADirectory(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.Upload(ctx, "dir/child", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	err := s.Upload(ctx, "dir", bytes.NewReader([]byte("y")))
	if !errors.Is(err, fileserver.ErrConflict) {
		t.Fatalf("Upload over a directory = %v, want ErrConflict", err)
	}
	if _, err := s.Stat(ctx, "dir/child"); err != nil {
		t.Fatalf("directory contents were disturbed: %v", err)
	}
}

func TestSymlinkCannotEscapeRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, dataDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, dataDirName, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, dataDirName, "outdir")); err != nil {
		t.Fatal(err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	if info, err := s.Stat(ctx, "escape"); err == nil {
		t.Fatalf("Stat followed an escaping symlink: %+v", info)
	}
	if rc, err := s.Read(ctx, "escape"); err == nil {
		data := make([]byte, 64)
		n, _ := rc.Read(data)
		_ = rc.Close()
		t.Fatalf("Read followed an escaping symlink and returned %q", data[:n])
	}
	var buf fileservertest.BufferAt
	if err := s.Download(ctx, "escape", &buf); err == nil || len(buf.Bytes()) != 0 {
		t.Fatalf("Download followed an escaping symlink: err=%v bytes=%q", err, buf.Bytes())
	}

	if err := s.Upload(ctx, "outdir/planted", bytes.NewReader([]byte("x"))); err == nil {
		t.Fatal("Upload wrote through an escaping directory symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "planted")); !os.IsNotExist(err) {
		t.Fatalf("file was planted outside the root: %v", err)
	}
}

func TestSymlinkInsideRootIsAllowed(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, dataDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, dataDirName, "target"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(root, dataDirName, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	rc, err := s.Read(context.Background(), "alias")
	if err != nil {
		t.Fatalf("Read of an in-root symlink: %v", err)
	}
	defer func() { _ = rc.Close() }()
	data := make([]byte, 16)
	n, _ := rc.Read(data)
	if string(data[:n]) != "inside" {
		t.Fatalf("Read through in-root symlink = %q", data[:n])
	}
}

func TestNonRegularFilesAreNotObjects(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "pipe")
	if err := mkfifo(fifo); err != nil {
		t.Skipf("FIFOs unavailable: %v", err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := s.Stat(ctx, "pipe"); !errors.Is(err, fileserver.ErrNotFound) {
			t.Errorf("Stat(FIFO) = %v, want ErrNotFound", err)
		}
		if rc, err := s.Read(ctx, "pipe"); !errors.Is(err, fileserver.ErrNotFound) {
			if rc != nil {
				_ = rc.Close()
			}
			t.Errorf("Read(FIFO) = %v, want ErrNotFound", err)
		}
		var buf fileservertest.BufferAt
		if err := s.Download(ctx, "pipe", &buf); !errors.Is(err, fileserver.ErrNotFound) {
			t.Errorf("Download(FIFO) = %v, want ErrNotFound", err)
		}
		if err := s.Remove(ctx, "pipe"); err != nil {
			t.Errorf("Remove(FIFO) = %v, want nil", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("operations on a FIFO blocked")
	}
	if _, err := os.Lstat(fifo); err != nil {
		t.Fatalf("Remove deleted a FIFO that is not an object: %v", err)
	}
}

func TestRemoveLeavesDirectoryAlone(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.root.Mkdir("emptydir", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, "emptydir"); err != nil {
		t.Fatalf("Remove(directory) = %v, want nil", err)
	}
	fi, err := s.root.Stat("emptydir")
	if err != nil || !fi.IsDir() {
		t.Fatalf("Remove deleted the directory: %v", err)
	}
}

func TestFileModeIsAppliedBeforePublish(t *testing.T) {
	if !supportsPermissionBits() {
		t.Skip("permission bits are not meaningful on this platform")
	}
	s := newStore(t, WithFileMode(0o640))
	if err := s.Upload(context.Background(), "mode", bytes.NewReader([]byte("m"))); err != nil {
		t.Fatal(err)
	}
	fi, err := s.root.Stat(keyPath("mode"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o640 {
		t.Fatalf("file mode = %o, want 0640", got)
	}
}

func TestTemporaryAreaIsOutsideTheKeySpace(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.Upload(ctx, "ok", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	// 临时区在根目录下、数据区之外：同名的 Key 只是数据区里一个不存在的对象。
	if _, err := s.Stat(ctx, tempDirName); !errors.Is(err, fileserver.ErrNotFound) {
		t.Fatalf("Stat(%q) = %v, want ErrNotFound", tempDirName, err)
	}
	// 真正的临时文件不会因为 Key 规则被访问到。
	if err := s.Upload(ctx, tempDirName+"/x", bytes.NewReader([]byte("y"))); err != nil {
		t.Fatalf("upload to a key that shares the temp name: %v", err)
	}
	info, err := s.Stat(ctx, tempDirName+"/x")
	if err != nil || info.FSize != 1 {
		t.Fatalf("Stat(%q/x) = %+v, %v", tempDirName, info, err)
	}
}

func TestLocalReadSupportsReadAtWithinTheOpenedRange(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.Upload(ctx, "k", bytes.NewReader([]byte("0123456789"))); err != nil {
		t.Fatal(err)
	}
	rc, err := s.Read(ctx, "k", fileserver.WithRange(2, 5))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()

	ra, ok := rc.(io.ReaderAt)
	if !ok {
		t.Fatal("local Read stream does not implement io.ReaderAt")
	}
	buf := make([]byte, 2)
	if _, err := ra.ReadAt(buf, 1); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "34" {
		t.Fatalf("ReadAt = %q, want 34 within the opened range", buf)
	}
	sequential, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(sequential) != "23456" {
		t.Fatalf("Read after ReadAt = %q, want the range from the start", sequential)
	}
}

// requireNoErrT 在测试中断言错误为 nil。
func requireNoErrT(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestFailedFileSyncKeepsPreviousVersion checks the failure path between the
// content sync and the publish: the object must not become visible and the
// previous version must stay intact.
func TestFailedFileSyncKeepsPreviousVersion(t *testing.T) {
	boom := errors.New("injected file sync failure")
	dir := t.TempDir()
	first, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Upload(context.Background(), "obj", bytes.NewReader([]byte("old version"))); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := New(dir, withHook(failAt("upload.after-sync", boom)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Upload(context.Background(), "obj", bytes.NewReader([]byte("new version"))); !errors.Is(err, boom) {
		t.Fatalf("Upload error = %v, want the injected failure", err)
	}
	rc, err := s.Read(context.Background(), "obj")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old version" {
		t.Fatalf("object = %q, want the previous version", got)
	}
	if names := tempEntries(t, s); len(names) != 0 {
		t.Fatalf("temporary files left behind: %v", names)
	}
}

// TestNewFailsWhenTheRootCannotBeSynced checks that a Store which cannot make
// its root durable is not created at all.
func TestNewFailsWhenTheRootCannotBeSynced(t *testing.T) {
	boom := errors.New("injected root sync failure")
	_, err := New(t.TempDir(), withHook(failAt("sync:.", boom)))
	if err == nil {
		t.Fatal("New accepted a root that cannot be synced")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("New error = %v, want the injected failure", err)
	}
	if !strings.Contains(err.Error(), "sync root") {
		t.Fatalf("New error = %q, want it to mention the root sync", err)
	}
}

// TestNewRejectsAFileAsTheDataDirectory checks that the data area is a real
// directory: a regular file in its place must not be used as the object space.
func TestNewRejectsAFileAsTheDataDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, dataDirName), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New(dir)
	if err == nil {
		t.Fatal("New accepted a regular file as the data directory")
	}
	if !strings.Contains(err.Error(), "prepare "+dataDirName) {
		t.Fatalf("New error = %q, want the prepare error", err)
	}
}
