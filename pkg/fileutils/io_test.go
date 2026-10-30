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

type myString string

func TestWriteAndAppend(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "a", "b", "f.txt") // parents do not exist

	if err := WriteFile(p, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := AppendFile(p, []byte(" world")); err != nil {
		t.Fatal(err)
	}
	if err := AppendFile(p, myString("!")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != "hello world!" {
		t.Fatalf("content = %q", got)
	}
	if err := WriteFile(p, []byte("x")); err != nil { // replaces, not appends
		t.Fatal(err)
	}
	if got := read(t, p); got != "x" {
		t.Fatalf("content after rewrite = %q", got)
	}

	fresh := filepath.Join(dir, "new", "g.txt")
	if err := AppendFile(fresh, "a"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, fresh); got != "a" {
		t.Fatalf("AppendFile on new file = %q", got)
	}
}

func TestWriteFileKeepsExistingMode(t *testing.T) {
	t.Parallel()
	p := put(t, t.TempDir(), "f", "x")
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(p, "y"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestCreate(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "x", "y", "f")
	f, err := Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("hi"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != "hi" {
		t.Fatalf("content = %q", got)
	}
}

func collect(t *testing.T, path string) []string {
	t.Helper()
	lines := make([]string, 0, 8)
	for line, err := range Lines(path) {
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	return lines
}

func TestLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cases := []struct {
		name, content string
		want          []string
	}{
		{"mixed endings", "a\r\nb\n\nlast", []string{"a", "b", "", "last"}},
		{"trailing newline adds nothing", "a\nb\n", []string{"a", "b"}},
		{"empty file", "", nil},
		{"single blank line", "\n", []string{""}},
	}
	for _, c := range cases {
		p := put(t, dir, c.name, c.content)
		if got := collect(t, p); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLinesVeryLongLine(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 1<<20) // far beyond bufio.Scanner's 64KiB default
	p := put(t, t.TempDir(), "long", long+"\nshort\n")
	got := collect(t, p)
	if len(got) != 2 || got[0] != long || got[1] != "short" {
		t.Fatalf("long line not preserved: %d lines", len(got))
	}
}

func TestLinesEarlyBreak(t *testing.T) {
	t.Parallel()
	p := put(t, t.TempDir(), "f", "1\n2\n3\n")
	n := 0
	for _, err := range Lines(p) {
		if err != nil {
			t.Fatal(err)
		}
		n++
		if n == 2 {
			break
		}
	}
	if n != 2 {
		t.Fatalf("iterated %d lines, want 2", n)
	}
}

func TestLinesMissingFile(t *testing.T) {
	t.Parallel()
	n := 0
	for line, err := range Lines(filepath.Join(t.TempDir(), "nope")) {
		n++
		if line != "" || !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("got (%q, %v), want empty line and ErrNotExist", line, err)
		}
	}
	if n != 1 {
		t.Fatalf("yielded %d times, want exactly 1", n)
	}
}
