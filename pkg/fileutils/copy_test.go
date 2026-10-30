package fileutils

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func mustTime(t *testing.T) time.Time {
	t.Helper()
	return time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
}

func perm(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func skipIfNoModes(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
}

func TestCopyFile(t *testing.T) {
	t.Parallel()
	skipIfNoModes(t)
	dir := t.TempDir()
	src := put(t, dir, "src.txt", "payload")
	if err := os.Chmod(src, 0o600); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "x", "y", "dst.txt") // parents are created
	if err := Copy(src, dst); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dst); got != "payload" {
		t.Fatalf("content = %q", got)
	}
	if got := perm(t, dst); got != 0o600 {
		t.Fatalf("mode = %v, want the source's 0600", got)
	}

	// Overwrites an existing, longer file completely.
	put(t, dir, "long.txt", "0123456789012345")
	if err := Copy(src, filepath.Join(dir, "long.txt"), CopyOption{Sync: true, Mode: 0o400}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, "long.txt")); got != "payload" {
		t.Fatalf("overwrite left %q", got)
	}
	if got := perm(t, filepath.Join(dir, "long.txt")); got != 0o400 {
		t.Fatalf("mode = %v, want explicit 0400", got)
	}
}

func TestCopyFileIntoExistingDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := put(t, dir, "f.txt", "data")
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := Copy(src, target); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(target, "f.txt")); got != "data" {
		t.Fatalf("content = %q", got)
	}
}

func TestCopyOntoItselfKeepsData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := put(t, dir, "f.txt", "precious")

	for name, dst := range map[string]string{
		"same path":       src,
		"own directory":   dir, // resolves to dir/f.txt, which is src
		"unclean path":    filepath.Join(dir, ".", "f.txt"),
		"via parent hops": filepath.Join(dir, "..", filepath.Base(dir), "f.txt"),
	} {
		if err := Copy(src, dst); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := read(t, src); got != "precious" {
			t.Fatalf("%s: file was truncated, content = %q", name, got)
		}
	}
}

func TestCopyDir(t *testing.T) {
	t.Parallel()
	root := tree(t)
	dst := filepath.Join(t.TempDir(), "out")
	if err := Copy(root, dst); err != nil {
		t.Fatal(err)
	}
	for f, want := range map[string]string{
		"a.go":          "package a",
		"b.txt":         "b",
		"sub/c.go":      "package c",
		"sub/deep/d.go": "package d",
	} {
		if got := read(t, filepath.Join(dst, filepath.FromSlash(f))); got != want {
			t.Errorf("%s = %q, want %q", f, got, want)
		}
	}
	if !IsDir(filepath.Join(dst, "empty")) {
		t.Error("empty directory was not copied")
	}
}

func TestCopyDirMergesIntoExisting(t *testing.T) {
	t.Parallel()
	root := tree(t)
	dst := t.TempDir()
	put(t, dst, "keep.txt", "keep")
	put(t, dst, "a.go", "stale")
	if err := Copy(root, dst); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dst, "keep.txt")); got != "keep" {
		t.Errorf("unrelated file lost: %q", got)
	}
	if got := read(t, filepath.Join(dst, "a.go")); got != "package a" {
		t.Errorf("a.go not overwritten: %q", got)
	}
}

func TestCopyDirOptionsReachNestedFiles(t *testing.T) {
	t.Parallel()
	skipIfNoModes(t)
	root := tree(t)
	dst := filepath.Join(t.TempDir(), "out")
	if err := Copy(root, dst, CopyOption{Mode: 0o600}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"a.go", "sub/c.go", "sub/deep/d.go"} {
		if got := perm(t, filepath.Join(dst, filepath.FromSlash(f))); got != 0o600 {
			t.Errorf("%s mode = %v, want 0600 at every depth", f, got)
		}
	}
}

func TestCopyReadOnlyDir(t *testing.T) {
	t.Parallel()
	skipIfNoModes(t)
	src := filepath.Join(t.TempDir(), "ro")
	put(t, src, "f.txt", "x")
	if err := os.Chmod(src, 0o500); err != nil { //nolint:gosec // a directory needs the exec bit
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(src, 0o700) }) //nolint:gosec // a directory needs the exec bit

	dst := filepath.Join(t.TempDir(), "copy")
	if err := Copy(src, dst); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dst, "f.txt")); got != "x" {
		t.Fatalf("content = %q", got)
	}
	if got := perm(t, dst); got != 0o500 {
		t.Fatalf("copied directory mode = %v, want 0500", got)
	}
	t.Cleanup(func() { _ = os.Chmod(dst, 0o700) }) //nolint:gosec // a directory needs the exec bit
}

func TestCopySkipsSymlinks(t *testing.T) {
	t.Parallel()
	root := tree(t)
	if err := os.Symlink(filepath.Join(root, "a.go"), filepath.Join(root, "link.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := Copy(root, dst); err != nil {
		t.Fatal(err)
	}
	if Exists(filepath.Join(dst, "link.go")) {
		t.Fatal("symlink inside a directory should be skipped")
	}
}

func TestCopyErrors(t *testing.T) {
	t.Parallel()
	root := tree(t)
	file := filepath.Join(root, "a.go")
	out := t.TempDir()

	cases := []struct {
		name     string
		src, dst string
	}{
		{"empty src", "", filepath.Join(out, "x")},
		{"empty dst", file, ""},
		{"missing src", filepath.Join(root, "missing"), filepath.Join(out, "x")},
		{"dir onto existing file", root, file},
		{"dir into itself", root, filepath.Join(root, "sub", "copy")},
		{"dir into its own child that exists", root, filepath.Join(root, "sub")},
	}
	for _, c := range cases {
		if err := Copy(c.src, c.dst); err == nil {
			t.Errorf("%s: expected an error", c.name)
		}
	}
	// The rejected self-copy must not have created anything behind our back.
	if Exists(filepath.Join(root, "sub", "copy")) {
		t.Error("dir-into-itself created the destination before failing")
	}
}
