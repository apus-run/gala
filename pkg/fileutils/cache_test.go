package fileutils

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCacheServesUnchangedFileFromMemory(t *testing.T) {
	t.Parallel()
	p := put(t, t.TempDir(), "f.txt", "one")
	past := mustTime(t)
	if err := os.Chtimes(p, past, past); err != nil {
		t.Fatal(err)
	}

	var c Cache
	if b, err := c.ReadFile(p); err != nil || string(b) != "one" {
		t.Fatalf("first read = (%q, %v)", b, err)
	}

	// Rewrite with the same size and restore the mtime: stat cannot tell, so the
	// cached bytes must be returned. This proves the second call skipped the read.
	if err := os.WriteFile(p, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, past, past); err != nil {
		t.Fatal(err)
	}
	if b, _ := c.ReadFile(p); string(b) != "one" {
		t.Fatalf("expected cached %q, got %q", "one", b)
	}
}

func TestCacheNoticesChanges(t *testing.T) {
	t.Parallel()
	p := put(t, t.TempDir(), "f.txt", "short")
	var c Cache
	if _, err := c.ReadFile(p); err != nil {
		t.Fatal(err)
	}

	// A different size is detected immediately, whatever the mtime granularity.
	if err := os.WriteFile(p, []byte("a much longer body"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := c.ReadFile(p); string(b) != "a much longer body" {
		t.Fatalf("size change missed: %q", b)
	}

	// Same size, newer mtime.
	later := mustTime(t).AddDate(5, 0, 0)
	if err := os.WriteFile(p, []byte("A MUCH LONGER BODY"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	if b, _ := c.ReadFile(p); string(b) != "A MUCH LONGER BODY" {
		t.Fatalf("mtime change missed: %q", b)
	}
}

func TestCacheMissingFile(t *testing.T) {
	t.Parallel()
	p := put(t, t.TempDir(), "f.txt", "x")
	var c Cache
	if _, err := c.ReadFile(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadFile(p); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want ErrNotExist", err)
	}
	c.mu.RLock()
	_, kept := c.m[p]
	c.mu.RUnlock()
	if kept {
		t.Fatal("entry for a deleted file should be dropped")
	}
}

func TestCacheConcurrent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	paths := []string{put(t, dir, "a", "A"), put(t, dir, "b", "B")}
	var c Cache
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			for range 50 {
				b, err := c.ReadFile(paths[i%2])
				if err != nil || len(b) != 1 {
					t.Errorf("read = (%q, %v)", b, err)
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestCacheRelativePathFollowsWorkingDirectory(t *testing.T) {
	// Not parallel: t.Chdir changes the process working directory.
	dirA := t.TempDir()
	dirB := t.TempDir()
	put(t, dirA, "f.txt", "AAAA")
	put(t, dirB, "f.txt", "BBBB")
	mtime := mustTime(t)
	for _, d := range []string{dirA, dirB} {
		if err := os.Chtimes(filepath.Join(d, "f.txt"), mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}

	var c Cache
	t.Chdir(dirA)
	if b, err := c.ReadFile("f.txt"); err != nil || string(b) != "AAAA" {
		t.Fatalf("in dirA = (%q, %v)", b, err)
	}
	// Same relative name, different file: must not be served from dirA's entry.
	t.Chdir(dirB)
	if b, err := c.ReadFile("f.txt"); err != nil || string(b) != "BBBB" {
		t.Fatalf("in dirB = (%q, %v), want BBBB", b, err)
	}
}
