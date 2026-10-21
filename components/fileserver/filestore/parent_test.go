package filestore

import (
	"bytes"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/apus-run/gala/components/fileserver"
)

// syncLog 记录耐久性钩子收到的目录同步点。
type syncLog struct {
	dirs []string
}

func (l *syncLog) hook(point string) error {
	if dir, ok := strings.CutPrefix(point, "sync:"); ok {
		l.dirs = append(l.dirs, dir)
	}
	return nil
}

// TestNestedUploadSyncsEachDirectoryEntryOnce pins the durability wiring of a
// new nested key: every new directory is synced in its parent when it is
// created, and the leaf parent is synced once after publish. fsync itself is
// not observable in-process (see the Durability doc), so this test pins the
// wiring, not the crash behaviour.
func TestNestedUploadSyncsEachDirectoryEntryOnce(t *testing.T) {
	log := &syncLog{}
	s := newStore(t, withHook(log.hook))
	log.dirs = nil // New 同步根目录，不属于本次上传

	if err := s.Upload(t.Context(), "a/b/c", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	want := []string{
		dataDirName,
		filepath.Join(dataDirName, "a"),
		filepath.Join(dataDirName, "a", "b"),
	}
	if !reflect.DeepEqual(log.dirs, want) {
		t.Fatalf("directory syncs = %v, want %v", log.dirs, want)
	}
}

// Existing directories must satisfy the same durability requirement as new ones.
func TestSecondUploadSyncsParentDirectories(t *testing.T) {
	log := &syncLog{}
	s := newStore(t, withHook(log.hook))
	if err := s.Upload(t.Context(), "sub/one", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	log.dirs = nil
	if err := s.Upload(t.Context(), "sub/two", bytes.NewReader([]byte("y"))); err != nil {
		t.Fatal(err)
	}
	want := []string{dataDirName, filepath.Join(dataDirName, "sub")}
	if !reflect.DeepEqual(log.dirs, want) {
		t.Fatalf("directory syncs = %v, want %v", log.dirs, want)
	}
}

// Directories inherited from an earlier process also require parent synchronization.
func TestUploadIntoDirectoryFromAnotherProcessSyncsItsParent(t *testing.T) {
	dir := t.TempDir()
	first, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Upload(t.Context(), "sub/obj", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	log := &syncLog{}
	second, err := New(dir, withHook(log.hook))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	if err := second.Upload(t.Context(), "sub/other", bytes.NewReader([]byte("y"))); err != nil {
		t.Fatal(err)
	}
	want := []string{
		".",
		dataDirName,
		filepath.Join(dataDirName, "sub"),
	}
	if !reflect.DeepEqual(log.dirs, want) {
		t.Fatalf("directory syncs = %v, want %v", log.dirs, want)
	}
}

// TestFailedDirectorySyncRemovesTheNewDirectory checks the rollback: when the
// parent cannot be synced, the directory that was just created is removed
// again, so no half-prepared path stays behind.
func TestFailedDirectorySyncRemovesTheNewDirectory(t *testing.T) {
	boom := errors.New("injected directory sync failure")
	s := newStore(t, withHook(failAt("sync:"+dataDirName, boom)))
	err := s.Upload(t.Context(), "sub/obj", bytes.NewReader([]byte("x")))
	if !errors.Is(err, boom) {
		t.Fatalf("Upload error = %v, want the injected failure", err)
	}
	if _, statErr := s.root.Stat(filepath.Join(dataDirName, "sub")); !isNotExist(statErr) {
		t.Fatalf("the new directory survived a failed sync: %v", statErr)
	}
	if _, statErr := s.Stat(t.Context(), "sub/obj"); !errors.Is(statErr, fileserver.ErrNotFound) {
		t.Fatalf("the object became visible: %v", statErr)
	}
}

// TestEnsureParentRejectsAFileAsParent checks the conflict path: a key component
// that exists as a regular file must not be treated as a directory.
func TestEnsureParentRejectsAFileAsParent(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	if err := s.Upload(ctx, "file", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(ctx, "file/child", bytes.NewReader([]byte("y"))); !errors.Is(err, fileserver.ErrConflict) {
		t.Fatalf("Upload under a file = %v, want ErrConflict", err)
	}
	info, err := s.Stat(ctx, "file")
	if err != nil || info.FSize != 1 {
		t.Fatalf("the file was disturbed: %+v, %v", info, err)
	}
}
