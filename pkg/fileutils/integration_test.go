//go:build integration

package fileutils_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/apus-run/gala/pkg/fileutils"
)

// writeTree creates a template project under root and returns root.
func writeTree(t *testing.T, root string) string {
	t.Helper()
	files := map[string]string{
		"index.html":           "<h1>v1</h1>",
		"partials/nav.html":    "<nav>v1</nav>",
		"partials/footer.html": "<footer>v1</footer>",
		"assets/app.css":       "body{}",
		"notes.txt":            "v1 untouched",
	}
	for name, body := range files {
		if err := fileutils.WriteFile(filepath.Join(root, name), body); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestIntegrationTemplatePipeline copies a template tree, rewrites only the
// HTML files, and checks that every stage sees the previous stage's result.
func TestIntegrationTemplatePipeline(t *testing.T) {
	base := t.TempDir()
	src := writeTree(t, filepath.Join(base, "tpl"))
	out := filepath.Join(base, "site", "v2")

	if err := fileutils.Copy(src, out, fileutils.CopyOption{Sync: true}); err != nil {
		t.Fatalf("copy: %v", err)
	}

	err := fileutils.ReplaceDir(out, func(_, s string) string {
		return strings.ReplaceAll(s, "v1", "v2")
	}, fileutils.ScanOptions{Patterns: []string{"*.html"}, Recursive: true})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}

	htmls, err := fileutils.Scan(out, fileutils.ScanOptions{
		Patterns:  []string{"*.html"},
		Recursive: true,
		FilesOnly: true,
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(htmls) != 3 {
		t.Fatalf("html files = %d, want 3: %v", len(htmls), htmls)
	}
	for _, p := range htmls {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "v1") {
			t.Errorf("%s still contains v1: %q", p, b)
		}
	}

	// Non-matching files are copied verbatim, not rewritten.
	notes, err := os.ReadFile(filepath.Join(out, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(notes) != "v1 untouched" {
		t.Fatalf("notes.txt = %q, want untouched", notes)
	}

	// The source tree is never modified by the pipeline.
	orig, err := os.ReadFile(filepath.Join(src, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(orig) != "<h1>v1</h1>" {
		t.Fatalf("source index.html = %q, want unchanged", orig)
	}
}

// TestIntegrationCacheFollowsRewrites checks that Cache keeps serving the
// current content while a file is rewritten through the package's own writers.
func TestIntegrationCacheFollowsRewrites(t *testing.T) {
	p := filepath.Join(t.TempDir(), "conf", "app.yaml")
	if err := fileutils.WriteFile(p, "name: a"); err != nil {
		t.Fatal(err)
	}

	var cache fileutils.Cache
	read := func() string {
		t.Helper()
		b, err := cache.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	if got := read(); got != "name: a" {
		t.Fatalf("initial = %q", got)
	}
	if got := read(); got != "name: a" {
		t.Fatalf("second read = %q", got)
	}

	// Growing the file is always noticed.
	if err := fileutils.AppendFile(p, "\nport: 8080"); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "name: a\nport: 8080" {
		t.Fatalf("after append = %q", got)
	}

	// An in-place rewrite of the same length needs a later mtime to be seen.
	if err := fileutils.ReplaceFile(p, func(_, s string) string {
		return strings.Replace(s, "8080", "9090", 1)
	}); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "name: a\nport: 9090" {
		t.Fatalf("after replace = %q", got)
	}

	// Deleting the file surfaces ErrNotExist rather than stale content.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.ReadFile(p); err == nil {
		t.Fatal("read of deleted file returned no error")
	}
}

// TestIntegrationLinesLargeFile streams a file far larger than bufio's default
// buffer and checks every line arrives intact, including a very long one.
func TestIntegrationLinesLargeFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.log")
	const n = 20000
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "line-%05d\n", i)
	}
	long := strings.Repeat("x", 200<<10) // 200 KiB, beyond bufio.MaxScanTokenSize
	b.WriteString(long)
	if err := fileutils.WriteFile(p, b.String()); err != nil {
		t.Fatal(err)
	}

	count, last := 0, ""
	for line, err := range fileutils.Lines(p) {
		if err != nil {
			t.Fatal(err)
		}
		count++
		last = line
		if count <= n && line != fmt.Sprintf("line-%05d", count-1) {
			t.Fatalf("line %d = %q", count-1, line)
		}
	}
	if count != n+1 {
		t.Fatalf("lines = %d, want %d", count, n+1)
	}
	if last != long {
		t.Fatalf("final long line lost: len = %d, want %d", len(last), len(long))
	}
}

// TestIntegrationScanFilterAcrossTree combines Scan, Filter and MatchGlob over
// a nested tree and checks the selection matches what a manual walk gives.
func TestIntegrationScanFilterAcrossTree(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{
		"src/main.go",
		"src/internal/store/db.go",
		"src/internal/store/db_test.go",
		"src/api/handler.go",
		"docs/readme.md",
	} {
		if err := fileutils.WriteFile(filepath.Join(root, p), "x"); err != nil {
			t.Fatal(err)
		}
	}

	got, err := fileutils.Scan(root, fileutils.ScanOptions{
		Patterns:  []string{"*.go"},
		Recursive: true,
		FilesOnly: true,
		Filter: func(p string, _ fs.DirEntry) bool {
			ok, _ := fileutils.MatchGlob("**/internal/**", filepath.ToSlash(p))
			return !ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	var rel []string
	for _, p := range got {
		r, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatal(err)
		}
		rel = append(rel, filepath.ToSlash(r))
	}
	want := []string{"src/api/handler.go", "src/main.go"}
	if strings.Join(rel, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", rel, want)
	}
}

// TestIntegrationCopyPreservesPermissions copies a tree containing files with
// distinct modes and checks each copied file keeps its source mode.
func TestIntegrationCopyPreservesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	src := t.TempDir()
	modes := map[string]fs.FileMode{"run.sh": 0o755, "secret": 0o600, "plain": 0o644}
	for name, mode := range modes {
		p := filepath.Join(src, name)
		if err := fileutils.WriteFile(p, "x"); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}

	dst := filepath.Join(t.TempDir(), "copy")
	if err := fileutils.Copy(src, dst); err != nil {
		t.Fatal(err)
	}
	for name, mode := range modes {
		fi, err := os.Stat(filepath.Join(dst, name))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != mode {
			t.Errorf("%s mode = %v, want %v", name, fi.Mode().Perm(), mode)
		}
	}
}

// TestIntegrationSearchFindsInstalledFile checks Search over a real directory
// list: the explicit dirs win, and a missing name lists every directory tried.
func TestIntegrationSearchFindsInstalledFile(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	if err := fileutils.WriteFile(filepath.Join(second, "app.yaml"), "x"); err != nil {
		t.Fatal(err)
	}

	got, err := fileutils.Search("app.yaml", first, second)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(second, "app.yaml") {
		t.Fatalf("Search = %q, want the copy in the second dir", got)
	}

	_, err = fileutils.Search("nope.yaml", first, second)
	if err == nil {
		t.Fatal("missing file must fail")
	}
	for _, d := range []string{first, second} {
		if !strings.Contains(err.Error(), d) {
			t.Errorf("error %q does not list %q", err, d)
		}
	}
}
