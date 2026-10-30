package fileutils

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchOrder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	d1, d2 := filepath.Join(root, "d1"), filepath.Join(root, "d2")
	only2 := put(t, d2, "only2.yaml", "x")
	both1 := put(t, d1, "both.yaml", "1")
	put(t, d2, "both.yaml", "2")

	if got, err := Search("only2.yaml", d1, d2); err != nil || got != only2 {
		t.Errorf("got (%q, %v), want %q", got, err, only2)
	}
	if got, err := Search("both.yaml", d1, d2); err != nil || got != both1 {
		t.Errorf("earlier dir must win: got (%q, %v), want %q", got, err, both1)
	}
}

func TestSearchFallsBackToWorkingDir(t *testing.T) { //nolint:paralleltest // t.Chdir cannot be used in a parallel test
	wd := t.TempDir()
	want := put(t, wd, "conf.yaml", "x")
	t.Chdir(wd)

	got, err := Search("conf.yaml", filepath.Join(wd, "nowhere"))
	if err != nil {
		t.Fatal(err)
	}
	if mustEval(t, got) != mustEval(t, want) {
		t.Fatalf("got %q, want %q", got, want)
	}

	// A relative dir is resolved against the working directory.
	put(t, wd, "rel/inner.yaml", "x")
	if _, err := Search("inner.yaml", "rel"); err != nil {
		t.Fatalf("relative dir: %v", err)
	}
}

func TestSearchAbsoluteName(t *testing.T) {
	t.Parallel()
	p := put(t, t.TempDir(), "f.txt", "x")
	if got, err := Search(p, "/does/not/matter"); err != nil || got != p {
		t.Errorf("got (%q, %v), want %q", got, err, p)
	}
	_, err := Search(filepath.Join(t.TempDir(), "missing.txt"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want ErrNotExist", err)
	}
}

func TestSearchNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := Search("nothing-by-this-name.cfg", dir, dir) // duplicate on purpose
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error should list the searched dirs: %v", err)
	}
	if n := strings.Count(err.Error(), dir); n != 1 {
		t.Errorf("duplicate dir listed %d times, want once", n)
	}
}

func TestSelfPath(t *testing.T) {
	t.Parallel()
	p := SelfPath()
	if p == "" {
		t.Skip("executable path unavailable on this platform")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("SelfPath() = %q does not exist: %v", p, err)
	}
}
