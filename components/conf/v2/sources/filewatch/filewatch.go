// Package filewatch reads a local file and signals when its directory entry
// changes. It watches the parent directory so atomic replacements are observed.
package filewatch

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apus-run/gala/components/conf/v2/internal/backoff"
	"github.com/apus-run/gala/components/conf/v2/sources/file"
	"github.com/fsnotify/fsnotify"
)

// Source reads and watches one absolute pathname. It may be shared when the
// supplied decoder supports concurrent use.
type Source struct {
	path string
	file *file.Source
}

// New captures path relative to the current working directory. Invalid
// arguments are reported by Load and Watch.
func New(path string, dec func([]byte, any) error) *Source {
	if path == "" || dec == nil {
		return &Source{}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return &Source{}
	}
	return &Source{path: abs, file: file.New(abs, dec)}
}

// WithMaxBytes applies file.Source's read limit to a copy of this source.
func (s *Source) WithMaxBytes(n int64) *Source {
	next := *s
	if n < 0 {
		next.file = nil
	} else if next.file != nil {
		next.file = next.file.WithMaxBytes(n)
	}
	return &next
}

// Load reads and decodes the file once, with the same semantics as file.New.
func (s *Source) Load(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.file == nil {
		return nil, errors.New("invalid file source")
	}
	return s.file.Load(ctx)
}

// Watch returns a buffered invalidation channel after registering the parent
// directory. The file may be absent. The first signal reconciles changes since
// Open. Changes to the named entry are coalesced; callers decide when to Reload.
// An interrupted watch retries until ctx ends. Cancellation releases OS
// resources and closes the channel. Changes only to an external symlink target
// require an event in the watched directory. Atomic ..data symlink swaps
// in projected Kubernetes volumes are observed.
func (s *Source) Watch(ctx context.Context) (<-chan struct{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.file == nil {
		return nil, errors.New("invalid file source")
	}
	dir := filepath.Dir(s.path)
	w, err := watchDir(dir)
	if err != nil {
		return nil, err
	}
	signals := make(chan struct{}, 1)
	// Capture presence before signaling. A removal may be reported under a
	// different spelling after the entry is already gone.
	previous, _ := os.Stat(s.path)
	signals <- struct{}{}
	go func() {
		defer close(signals)
		for {
			s.listen(ctx, w, dir, signals, previous)
			w.Close()
			if ctx.Err() != nil {
				return
			}
			// Lost events or a removed directory require a full read. Signal
			// again after registering the replacement to cover the gap.
			signal(signals)
			var retry backoff.Delay
			for {
				if !retry.Wait(ctx) {
					return
				}
				w, err = watchDir(dir)
				if err == nil {
					break
				}
			}
			previous, _ = os.Stat(s.path)
			signal(signals)
		}
	}()
	return signals, nil
}

func watchDir(dir string) (*fsnotify.Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, errors.New("watch setup failed")
	}
	if err = w.Add(dir); err == nil {
		var info fs.FileInfo
		info, err = os.Stat(dir)
		if err == nil && info.IsDir() {
			return w, nil
		}
	}
	w.Close()
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fs.ErrNotExist
	case errors.Is(err, fs.ErrPermission):
		return nil, fs.ErrPermission
	default:
		return nil, errors.New("watch directory unavailable")
	}
}

func (s *Source) listen(ctx context.Context, w *fsnotify.Watcher, dir string, signals chan<- struct{}, previous fs.FileInfo) {
	// Some directory backends coalesce an atomic symlink swap into no named
	// event. Reconcile identity only for Kubernetes projected paths.
	var ticker *time.Ticker
	var check <-chan time.Time
	enableCheck := func() {
		if ticker == nil && projectedPath(s.path) {
			ticker = time.NewTicker(time.Second)
			check = ticker.C
		}
	}
	enableCheck()
	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-check:
			current, _ := os.Stat(s.path)
			if !sameEntry(previous, current) {
				previous = current
				signal(signals)
			}
		case <-w.Errors:
			return
		case event, ok := <-w.Events:
			if !ok || event.Name == dir && (event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename)) {
				return
			}
			current, _ := os.Stat(s.path)
			changed := event.Name == s.path || !sameEntry(previous, current)
			if ticker == nil && (changed || filepath.Base(event.Name) == "..data") {
				enableCheck()
			}
			if ticker != nil && filepath.Base(event.Name) == "..data" {
				changed = true
			}
			if !changed && current != nil {
				// Follow links so a ConfigMap ..data swap changes file identity.
				reported, _ := os.Stat(event.Name)
				changed = os.SameFile(current, reported)
			}
			previous = current
			if changed {
				signal(signals)
			}
		}
	}
}

// Kubernetes links each top-level entry to ..data/<entry>. The requested
// file may also be inside one of these projected directories.
func projectedPath(path string) bool {
	for {
		if target, err := os.Readlink(path); err == nil && strings.HasPrefix(filepath.ToSlash(target), "..data/") {
			return true
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}

func sameEntry(a, b fs.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b)
}

func signal(ch chan<- struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
