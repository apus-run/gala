package filewatch_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/file"
	"github.com/apus-run/gala/components/conf/v2/sources/filewatch"
)

func nextSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("watch closed unexpectedly")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for file event")
	}
}

func stopWatch(t *testing.T, cancel context.CancelFunc, ch <-chan struct{}) {
	t.Helper()
	cancel()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("watch did not close")
		}
	}
}

func write(t *testing.T, name, data string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestWatchReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.json")
	write(t, path, `{"Port":1,"Mode":"old"}`)
	src := filewatch.New(path, file.JSON)
	type app struct {
		Port int
		Mode string
	}
	override := conf.SourceFunc(func(context.Context) (map[string]any, error) { return map[string]any{"Port": 9}, nil })
	cfg, err := conf.Open(t.Context(), app{}, src, override)
	if err != nil {
		t.Fatal(err)
	}
	old := cfg.Current()
	write(t, path, `{"Port":2,"Mode":"before watch"}`)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch, err := src.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stopWatch(t, cancel, ch)

	check := func(mode string) {
		t.Helper()
		nextSignal(t, ch)
		if err := cfg.Reload(ctx); err != nil {
			t.Fatal(err)
		}
		if got := *cfg.Current(); got != (app{9, mode}) {
			t.Fatalf("snapshot: %+v", got)
		}
	}
	check("before watch")
	write(t, path, `{"Mode":"modified"}`)
	check("modified")
	// Rename over the existing file, as an editor saving atomically would.
	staged := filepath.Join(dir, "staged")
	write(t, staged, `{"Port":3,"Mode":"replacement"}`)
	if err := os.Rename(staged, path); err != nil {
		t.Fatal(err)
	}
	check("replacement")

	last := cfg.Current()
	write(t, path, "{")
	nextSignal(t, ch)
	if cfg.Current() != last {
		t.Fatal("Watch published without Reload")
	}
	if err := cfg.Reload(ctx); err == nil || cfg.Current() != last {
		t.Fatal("invalid document published")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	nextSignal(t, ch)
	if err := cfg.Reload(ctx); !errors.Is(err, fs.ErrNotExist) || cfg.Current() != last {
		t.Fatal(err)
	}
	write(t, path, `{"Mode":"recreated"}`)
	check("recreated")
	if *old != (app{9, "old"}) {
		t.Fatal("old snapshot changed")
	}
}

func TestWatchRegistrationAndCancellation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent.json")
	src := filewatch.New(path, file.JSON)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch, err := src.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	nextSignal(t, ch)
	write(t, filepath.Join(dir, "unrelated"), "x")
	select {
	case <-ch:
		t.Fatal("unrelated sibling triggered reload")
	case <-time.After(100 * time.Millisecond):
	}
	write(t, path, "{}")
	nextSignal(t, ch)
	stopWatch(t, cancel, ch)
	if ch, err := src.Watch(ctx); ch != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(ch, err)
	}
	missing := filewatch.New(filepath.Join(dir, "SECRET", "app.json"), file.JSON)
	if ch, err := missing.Watch(t.Context()); ch != nil || !errors.Is(err, fs.ErrNotExist) || strings.Contains(err.Error(), "SECRET") {
		t.Fatal(ch, err)
	}
	for _, s := range []*filewatch.Source{nil, {}, filewatch.New("", file.JSON), filewatch.New(path, nil)} {
		if ch, err := s.Watch(t.Context()); ch != nil || err == nil {
			t.Fatal("invalid source accepted")
		}
	}
}

func TestWatchDirectoryReplacement(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "config")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app.json")
	src := filewatch.New(path, file.JSON)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch, err := src.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stopWatch(t, cancel, ch)
	nextSignal(t, ch)
	if err := os.Rename(dir, filepath.Join(root, "old")); err != nil {
		t.Fatal(err)
	}
	nextSignal(t, ch)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, path, `{"version":"restored"}`)
	nextSignal(t, ch) // registration recovered despite no later writes
	m, err := src.Load(ctx)
	if err != nil || m["version"] != "restored" {
		t.Fatal(m, err)
	}
	write(t, path, `{"version":"next"}`)
	nextSignal(t, ch)
}

// Exercise the actual volume's name lookup rules, including when the target
// disappears and there is no directory entry left to compare.
func TestWatchFilenameCase(t *testing.T) {
	dir := t.TempDir()
	stored := filepath.Join(dir, "app.json")
	requested := filepath.Join(dir, "APP.json")
	write(t, stored, "{}")
	_, err := os.Lstat(requested)
	aliases := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if !aliases {
		write(t, requested, "{}")
	}

	src := filewatch.New(requested, file.JSON)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch, err := src.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stopWatch(t, cancel, ch)
	nextSignal(t, ch)

	expect := func() {
		t.Helper()
		if aliases {
			nextSignal(t, ch)
			return
		}
		select {
		case <-ch:
			t.Fatal("distinct sibling triggered reload")
		case <-time.After(100 * time.Millisecond):
		}
	}
	write(t, stored, `{"N":1}`)
	expect()
	tmp := filepath.Join(dir, "new")
	write(t, tmp, `{"N":2}`)
	if err := os.Rename(tmp, stored); err != nil {
		t.Fatal(err)
	}
	expect()
	if err := os.Remove(stored); err != nil {
		t.Fatal(err)
	}
	expect()
	write(t, stored, `{"N":3}`)
	expect()
	if !aliases {
		write(t, requested, `{"N":4}`)
		nextSignal(t, ch)
	}
}

func TestConfigMapRotation(t *testing.T) {
	dir := t.TempDir()
	for _, version := range []string{"v1", "v2"} {
		sub := filepath.Join(dir, version)
		if err := os.Mkdir(sub, 0700); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(sub, "app.json"), `{"Version":"`+version+`"}`)
	}
	if err := os.Symlink("v1", filepath.Join(dir, "..data")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	path := filepath.Join(dir, "app.json")
	if err := os.Symlink(filepath.Join("..data", "app.json"), path); err != nil {
		t.Fatal(err)
	}
	src := filewatch.New(path, file.JSON)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cfg, err := conf.Open(ctx, struct{ Version string }{}, src)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := src.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stopWatch(t, cancel, ch)
	nextSignal(t, ch)
	old := cfg.Current()
	if err := os.Symlink("v2", filepath.Join(dir, "..data_tmp")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "..data_tmp"), filepath.Join(dir, "..data")); err != nil {
		t.Fatal(err)
	}
	nextSignal(t, ch)
	if err := cfg.Reload(ctx); err != nil || cfg.Current().Version != "v2" || old.Version != "v1" {
		t.Fatal(cfg.Current(), err)
	}
}
