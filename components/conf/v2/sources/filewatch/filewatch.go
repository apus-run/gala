// Package filewatch reads a local file and signals when its directory entry
// changes. It watches the parent directory so atomic replacements are observed.
package filewatch

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

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
// are outside the watched directory entry.
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
	previous, _ := os.Lstat(s.path)
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
			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
				w, err = watchDir(dir)
				if err == nil {
					break
				}
			}
			previous, _ = os.Lstat(s.path)
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
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.Errors:
			return
		case event, ok := <-w.Events:
			if !ok || event.Name == dir && (event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename)) {
				return
			}
			current, _ := os.Lstat(s.path)
			changed := event.Name == s.path || !sameEntry(previous, current)
			if !changed && current != nil {
				// Name lookup is volume-specific. Lstat preserves the identity
				// of a symlink entry instead of following its target.
				reported, _ := os.Lstat(event.Name)
				changed = os.SameFile(current, reported)
			}
			previous = current
			if changed {
				signal(signals)
			}
		}
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
