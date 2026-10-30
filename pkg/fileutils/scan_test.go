package fileutils

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// tree builds:
//
//	a.go  b.txt  empty/  sub/c.go  sub/deep/d.go
func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put(t, root, "a.go", "package a")
	put(t, root, "b.txt", "b")
	put(t, root, "sub/c.go", "package c")
	put(t, root, "sub/deep/d.go", "package d")
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o750); err != nil {
		t.Fatal(err)
	}
	return root
}

// rel converts scan results to slash-separated paths relative to root.
func rel(t *testing.T, root string, list []string) []string {
	t.Helper()
	out := make([]string, len(list))
	for i, p := range list {
		if !filepath.IsAbs(p) {
			t.Fatalf("result %q is not absolute", p)
		}
		r, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = filepath.ToSlash(r)
	}
	return out
}

func TestScan(t *testing.T) {
	t.Parallel()
	root := tree(t)
	cases := []struct {
		name string
		opt  ScanOptions
		want []string
	}{
		{"children only", ScanOptions{}, []string{"a.go", "b.txt", "empty", "sub"}},
		{"pattern, not recursive", ScanOptions{Patterns: []string{"*.go"}}, []string{"a.go"}},
		{
			"pattern, recursive",
			ScanOptions{Patterns: []string{"*.go"}, Recursive: true},
			[]string{"a.go", "sub/c.go", "sub/deep/d.go"},
		},
		{
			"several patterns",
			ScanOptions{Patterns: []string{"*.txt", "c.*"}, Recursive: true},
			[]string{"b.txt", "sub/c.go"},
		},
		{
			"files only",
			ScanOptions{FilesOnly: true, Recursive: true},
			[]string{"a.go", "b.txt", "sub/c.go", "sub/deep/d.go"},
		},
		{
			"everything",
			ScanOptions{Recursive: true},
			[]string{"a.go", "b.txt", "empty", "sub", "sub/c.go", "sub/deep", "sub/deep/d.go"},
		},
		{
			"pattern matches directories too",
			ScanOptions{Patterns: []string{"s*"}, Recursive: true},
			[]string{"sub"},
		},
	}
	for _, c := range cases {
		got, err := Scan(root, c.opt)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if r := rel(t, root, got); !slices.Equal(r, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.name, r, c.want)
		}
	}
}

func TestScanFilter(t *testing.T) {
	t.Parallel()
	root := tree(t)
	got, err := Scan(root, ScanOptions{
		Recursive: true,
		FilesOnly: true,
		Filter: func(p string, _ fs.DirEntry) bool {
			r, _ := filepath.Rel(root, p)
			ok, _ := MatchGlob("sub/**/*.go", filepath.ToSlash(r))
			return ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r := rel(t, root, got); !slices.Equal(r, []string{"sub/c.go", "sub/deep/d.go"}) {
		t.Fatalf("got %q", r)
	}
}

func TestScanErrors(t *testing.T) {
	t.Parallel()
	root := tree(t)
	if _, err := Scan(filepath.Join(root, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing root: err = %v", err)
	}
	if _, err := Scan(filepath.Join(root, "a.go")); err == nil {
		t.Error("scanning a file must fail")
	}
	if _, err := Scan(root, ScanOptions{Patterns: []string{"[bad"}}); !errors.Is(err, filepath.ErrBadPattern) {
		t.Errorf("bad pattern: err = %v", err)
	}
}

func TestScanSymlinks(t *testing.T) {
	t.Parallel()
	root := tree(t)
	// A link pointing back at the root would loop forever if followed.
	if err := os.Symlink(root, filepath.Join(root, "sub", "loop")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got, err := Scan(root, ScanOptions{Recursive: true, Patterns: []string{"*.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if r := rel(t, root, got); !slices.Equal(r, []string{"a.go", "sub/c.go", "sub/deep/d.go"}) {
		t.Fatalf("got %q", r)
	}

	// A root that is itself a symlink to a directory is scanned normally.
	link := filepath.Join(t.TempDir(), "link")
	if serr := os.Symlink(root, link); serr != nil {
		t.Skipf("symlinks unavailable: %v", serr)
	}
	got, err = Scan(link, ScanOptions{Patterns: []string{"*.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if r := rel(t, link, got); !slices.Equal(r, []string{"a.go"}) {
		t.Fatalf("through symlinked root: got %q", r)
	}
}

func TestSortFiles(t *testing.T) {
	t.Parallel()
	root := tree(t)
	in := []string{"b.txt", "sub", "a.go", "empty"}
	for i := range in {
		in[i] = filepath.Join(root, in[i])
	}
	SortFiles(in)
	if r := rel(t, root, in); !slices.Equal(r, []string{"empty", "sub", "a.go", "b.txt"}) {
		t.Fatalf("got %q, want directories first", r)
	}
}

func TestReplaceFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	swap := func(_, s string) string { return strings.ReplaceAll(s, "old", "new") }

	p := put(t, dir, "f.txt", "old and old")
	if err := ReplaceFile(p, swap); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != "new and new" {
		t.Fatalf("content = %q", got)
	}

	// Unchanged content must not rewrite the file: its mtime stays put.
	q := put(t, dir, "g.txt", "nothing here")
	past := mustTime(t)
	if err := os.Chtimes(q, past, past); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(q, swap); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(q); !fi.ModTime().Equal(past) {
		t.Fatalf("unchanged file was rewritten, mtime = %v", fi.ModTime())
	}

	// A missing file is an error and must not be created as an empty file.
	missing := filepath.Join(dir, "missing.txt")
	if err := ReplaceFile(missing, swap); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want ErrNotExist", err)
	}
	if Exists(missing) {
		t.Fatal("ReplaceFile created the missing file")
	}
}

func TestReplaceDir(t *testing.T) {
	t.Parallel()
	root := tree(t)
	var seen []string
	fn := func(p, s string) string {
		seen = append(seen, filepath.Base(p))
		return strings.ReplaceAll(s, "package", "pkg")
	}
	if err := ReplaceDir(root, fn, ScanOptions{Patterns: []string{"*.go"}, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	slices.Sort(seen)
	if !slices.Equal(seen, []string{"a.go", "c.go", "d.go"}) {
		t.Fatalf("fn saw %q", seen)
	}
	for _, f := range []string{"a.go", "sub/c.go", "sub/deep/d.go"} {
		if got := read(t, filepath.Join(root, f)); !strings.HasPrefix(got, "pkg ") {
			t.Errorf("%s = %q", f, got)
		}
	}
	if got := read(t, filepath.Join(root, "b.txt")); got != "b" {
		t.Errorf("b.txt was touched: %q", got)
	}
}
