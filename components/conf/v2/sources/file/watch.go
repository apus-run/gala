package file

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/fsnotify/fsnotify"
)

// Watch watches the parent directory so atomic replacement of the file is
// observed, including intermediate symlink switches in that directory.
// io/fs sources do not offer OS events; use configured polling.
func (s *Source) Watch(ctx context.Context) (conf.Watcher, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.useFS {
		return nil, conf.ErrWatchUnsupported
	}
	path, err := filepath.Abs(s.path)
	if err != nil {
		return nil, err
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := watcher.Add(filepath.Dir(path)); err != nil {
		watcher.Close()
		return nil, err
	}
	return &fileWatcher{watcher: watcher, path: path}, nil
}

type fileWatcher struct {
	watcher *fsnotify.Watcher
	path    string
}

func (w *fileWatcher) Next(ctx context.Context) error {
	var checks <-chan time.Time
	resolved, _ := filepath.EvalSymlinks(w.path)
	if resolved != w.path {
		// Some backends omit events when an intermediate link is replaced.
		// Reconcile periodically: comparing only link targets misses an ABA
		// switch when a concurrent reload already published the middle value.
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		checks = timer.C
	}
	for {
		var name string
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-checks:
			return nil
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return errors.New("file watcher closed")
			}
			return err
		case event, ok := <-w.watcher.Events:
			if !ok {
				return errors.New("file watcher closed")
			}
			name = filepath.Clean(event.Name)
			if name == filepath.Dir(w.path) && event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				return errors.New("watched directory replaced")
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
		}
		// A temporarily broken link resolves to ""; reloading then reports
		// the read failure and preserves the last valid configuration.
		target, _ := filepath.EvalSymlinks(w.path)
		if target != resolved || name == w.path || name == target {
			return nil
		}
	}
}
func (w *fileWatcher) Close() error { return w.watcher.Close() }
