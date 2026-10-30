package fileutils

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// skipIfRoot skips tests that rely on permission bits being enforced, since
// root ignores them.
func skipIfRoot(t *testing.T) {
	t.Helper()
	skipIfNoModes(t)
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
}

// restoreMode puts path back to mode at cleanup so t.TempDir can remove it.
func restoreMode(t *testing.T, path string, mode fs.FileMode) {
	t.Helper()
	t.Cleanup(func() { _ = os.Chmod(path, mode) })
}

// blockerFile returns a regular file; anything created below it fails with ENOTDIR.
func blockerFile(t *testing.T, dir string) string {
	t.Helper()
	return put(t, dir, "blocker", "x")
}

func TestCopyDestinationStatFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := put(t, dir, "a.txt", "x")
	err := Copy(src, filepath.Join(blockerFile(t, dir), "x"))
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want a stat error other than ErrNotExist", err)
	}
}

func TestCopyUnreadableFile(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	dir := t.TempDir()
	src := put(t, dir, "secret", "x")
	if err := os.Chmod(src, 0); err != nil {
		t.Fatal(err)
	}
	restoreMode(t, src, 0o600)
	if err := Copy(src, filepath.Join(dir, "out")); err == nil {
		t.Fatal("copying an unreadable file must fail")
	}
}

func TestCopyIntoUnwritableDirectory(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	dir := t.TempDir()
	src := put(t, dir, "a.txt", "x")
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	restoreMode(t, locked, 0o700)
	if err := Copy(src, filepath.Join(locked, "a.txt")); err == nil {
		t.Fatal("creating a file in a read-only directory must fail")
	}
}

func TestCopyDirIntoUnwritableParent(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "tree")
	put(t, src, "a.txt", "x")
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	restoreMode(t, locked, 0o700)
	if err := Copy(src, filepath.Join(locked, "copy")); err == nil {
		t.Fatal("creating a directory in a read-only parent must fail")
	}
}

func TestCopyUnreadableDirectory(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "tree")
	put(t, src, "a.txt", "x")
	if err := os.Chmod(src, 0); err != nil {
		t.Fatal(err)
	}
	restoreMode(t, src, 0o700)
	if err := Copy(src, filepath.Join(dir, "out")); err == nil {
		t.Fatal("listing an unreadable directory must fail")
	}
}

func TestCopyDirWithUnreadableFile(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "tree")
	secret := put(t, src, "sub/secret", "x")
	if err := os.Chmod(secret, 0); err != nil {
		t.Fatal(err)
	}
	restoreMode(t, secret, 0o600)
	if err := Copy(src, filepath.Join(dir, "out")); err == nil {
		t.Fatal("an unreadable file inside a tree must abort the copy")
	}
}

func TestCopyAfterWorkingDirectoryRemoved(t *testing.T) {
	// Not parallel: t.Chdir changes the process working directory.
	gone := t.TempDir()
	src := put(t, t.TempDir(), "tree/a.txt", "x")
	t.Chdir(gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	// The relative destination cannot be created once the working directory is gone.
	if err := Copy(filepath.Dir(src), "relative-out"); err == nil {
		t.Fatal("copy with no working directory must fail")
	}
}

func TestScanAfterWorkingDirectoryRemoved(t *testing.T) {
	// Not parallel: t.Chdir changes the process working directory.
	gone := t.TempDir()
	t.Chdir(gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan("relative"); err == nil {
		t.Fatal("scanning a relative root with no working directory must fail")
	}
}

func TestScanRecursiveUnreadableSubdirectory(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	put(t, locked, "x.txt", "x")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	restoreMode(t, locked, 0o700)
	if _, err := Scan(root, ScanOptions{Recursive: true}); err == nil {
		t.Fatal("an unreadable sub-directory must fail the scan")
	}
}

func TestReplaceDirMissingRoot(t *testing.T) {
	t.Parallel()
	err := ReplaceDir(filepath.Join(t.TempDir(), "missing"), func(_, s string) string { return s })
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want ErrNotExist", err)
	}
}

func TestReplaceDirUnreadableFile(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	dir := t.TempDir()
	secret := put(t, dir, "a.md", "x")
	if err := os.Chmod(secret, 0); err != nil {
		t.Fatal(err)
	}
	restoreMode(t, secret, 0o600)
	if err := ReplaceDir(dir, func(_, s string) string { return s + "!" }); err == nil {
		t.Fatal("an unreadable file must fail ReplaceDir")
	}
}

func TestCreateUnderRegularFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := Create(filepath.Join(blockerFile(t, dir), "x")); err == nil {
		t.Fatal("creating below a regular file must fail")
	}
}

func TestWriteFileUnderRegularFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteFile(filepath.Join(blockerFile(t, dir), "x"), "data"); err == nil {
		t.Fatal("writing below a regular file must fail")
	}
}

func TestWriteFileToDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteFile(dir, "data"); err == nil {
		t.Fatal("writing over a directory must fail")
	}
}

func TestLinesOnDirectoryYieldsError(t *testing.T) {
	t.Parallel()
	n := 0
	for line, err := range Lines(t.TempDir()) {
		n++
		if line != "" || err == nil {
			t.Fatalf("got (%q, %v), want an empty line and a read error", line, err)
		}
	}
	if n != 1 {
		t.Fatalf("yielded %d times, want exactly 1", n)
	}
}

func TestIsEmptyUnreadableDirectory(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	restoreMode(t, dir, 0o700)
	// Documented: a path that cannot be inspected counts as empty.
	if !IsEmpty(dir) {
		t.Fatal("unreadable directory must report empty")
	}
}

func TestIsWritableReadOnlyDirectory(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	dir := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	restoreMode(t, dir, 0o700)
	if IsWritable(dir) {
		t.Fatal("read-only directory must not be writable")
	}
}

func TestCacheReadErrorDropsEntry(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	p := put(t, t.TempDir(), "f.txt", "one")
	var c Cache
	if _, err := c.ReadFile(p); err != nil {
		t.Fatal(err)
	}
	// A size change forces a re-read, which the lost permission then refuses.
	if err := os.WriteFile(p, []byte("longer body"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	restoreMode(t, p, 0o600)
	if _, err := c.ReadFile(p); err == nil {
		t.Fatal("unreadable file must return an error")
	}
	c.mu.RLock()
	_, kept := c.m[p]
	c.mu.RUnlock()
	if kept {
		t.Fatal("entry must be dropped after a failed read")
	}
}

func TestCacheAfterWorkingDirectoryRemoved(t *testing.T) {
	// Not parallel: t.Chdir changes the process working directory.
	gone := t.TempDir()
	t.Chdir(gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	var c Cache
	if _, err := c.ReadFile("relative.txt"); err == nil {
		t.Fatal("a relative path with no working directory must fail")
	}
}
