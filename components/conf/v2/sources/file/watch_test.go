package file_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/file"
)

func TestWatchIndirectSymlinkSwitch(t *testing.T) {
	path, switchTarget := linkedConfig(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	source := file.New(path)
	w, err := source.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, version := range []string{"v2", "v1"} {
		switchTarget(version)
		if err := w.Next(ctx); err != nil {
			t.Fatalf("indirect link switch to %s was not reported: %v", version, err)
		}
		data, err := source.Load(ctx)
		if err != nil || data["name"] != version {
			t.Fatalf("load after switch: %v, %v", data, err)
		}
	}
}

func TestAutomaticReloadAfterIndirectSymlinkSwitch(t *testing.T) {
	path, switchTarget := linkedConfig(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	c, err := conf.Open[shape](ctx, conf.WithSources(file.New(path)), conf.WithWatch())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	events, unsubscribe := c.Subscribe()
	defer unsubscribe()
	old := c.Current()
	// Two switches ensure startup reconciliation alone cannot pass this test.
	for _, version := range []string{"v2", "v1"} {
		switchTarget(version)
		for c.Current().Name != version {
			select {
			case <-ctx.Done():
				t.Fatalf("configuration did not converge to %s: %v", version, ctx.Err())
			case event := <-events:
				if event.Err != nil {
					t.Fatalf("link switch caused a watcher failure: %v", event.Err)
				}
			}
		}
	}
	if old.Name != "v1" {
		t.Fatal("link switch mutated the old snapshot")
	}
	// The resolved file lives outside the watched parent directory. Its path
	// stays the same, so resolution comparison alone cannot detect this write.
	write(t, filepath.Join(filepath.Dir(path), "v1", "app.json"), `{"name":"updated"}`)
	for c.Current().Name != "updated" {
		select {
		case <-ctx.Done():
			t.Fatal("symlink target content update was not detected")
		case event := <-events:
			if event.Err != nil {
				t.Fatal(event.Err)
			}
		}
	}
}

// app.json stays unchanged while an intermediate link is atomically switched.
func linkedConfig(t *testing.T) (string, func(string)) {
	t.Helper()
	root := t.TempDir()
	for _, version := range []string{"v1", "v2"} {
		dir := filepath.Join(root, version)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(dir, "app.json"), `{"name":"`+version+`"}`)
	}
	link := func(target, path string) {
		t.Helper()
		if err := os.Symlink(target, path); err != nil {
			if runtime.GOOS == "windows" {
				t.Skipf("symbolic links unavailable: %v", err)
			}
			t.Fatal(err)
		}
	}
	link("v1", filepath.Join(root, "current"))
	path := filepath.Join(root, "app.json")
	link("current/app.json", path)
	return path, func(version string) {
		t.Helper()
		next := filepath.Join(root, "next")
		link(version, next)
		if err := os.Rename(next, filepath.Join(root, "current")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWatchReportsReplacedParentDirectory(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	root := t.TempDir()
	dir := filepath.Join(root, "config")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app.json")
	write(t, path, `{"name":"old"}`)
	w, err := file.New(path).Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := os.Rename(dir, filepath.Join(root, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, path, `{"name":"new"}`)
	for {
		err := w.Next(ctx)
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("parent replacement silently disabled monitoring")
		}
		if err != nil {
			return // Core must reopen the watcher and reconcile the missed update.
		}
	}
}

func TestWatchAtomicReplacementAndRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "app.json")
	write(t, path, `{"name":"old"}`)
	c, err := conf.Open[shape](ctx, conf.WithSources(file.New(path)), conf.WithWatch())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	events, unsubscribe := c.Subscribe()
	defer unsubscribe()
	old := c.Current()
	replacement := path + ".tmp"
	write(t, replacement, `{"name":"new"}`)
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	for c.Current().Name != "new" {
		select {
		case <-ctx.Done():
			t.Fatal("file replacement not detected")
		case <-events:
		}
	}
	write(t, path, `{"name":`)
	failed := false
	for !failed {
		select {
		case <-ctx.Done():
			t.Fatal("decode failure not reported")
		case event := <-events:
			failed = event.Err != nil
		}
	}
	if c.Current().Name != "new" || old.Name != "old" {
		t.Fatal("failed update changed snapshot")
	}
	write(t, path, `{"name":"recovered"}`)
	for c.Current().Name != "recovered" {
		select {
		case <-ctx.Done():
			t.Fatal("recovery not detected")
		case <-events:
		}
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}
