package fileutils

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Cache keeps file contents in memory and re-reads a file only when its size
// or modification time has changed. There is no TTL, no watcher and no
// background goroutine: every ReadFile costs one stat call and nothing more
// while the file is unchanged.
//
// The zero value is ready to use and safe for concurrent use. Intended for a
// small, long-lived set of files such as templates or configuration. A write
// that keeps the size and lands within the file system's timestamp granularity
// of the previous one may go unnoticed.
type Cache struct {
	mu sync.RWMutex
	m  map[string]cacheEntry
}

type cacheEntry struct {
	mtime time.Time
	size  int64
	data  []byte
}

// ReadFile returns the content of path. The returned slice is shared with the
// cache and must not be modified.
func (c *Cache) ReadFile(path string) ([]byte, error) {
	// Keyed by absolute path, so the same relative name read from another
	// directory after a chdir does not get this file's entry.
	key, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(key)
	if err != nil {
		c.forget(key)
		return nil, err
	}

	c.mu.RLock()
	e, ok := c.m[key]
	c.mu.RUnlock()
	if ok && e.size == fi.Size() && e.mtime.Equal(fi.ModTime()) {
		return e.data, nil
	}

	data, err := os.ReadFile(key)
	if err != nil {
		c.forget(key)
		return nil, err
	}
	c.mu.Lock()
	if c.m == nil {
		c.m = make(map[string]cacheEntry)
	}
	c.m[key] = cacheEntry{mtime: fi.ModTime(), size: fi.Size(), data: data}
	c.mu.Unlock()
	return data, nil
}

func (c *Cache) forget(path string) {
	c.mu.Lock()
	delete(c.m, path)
	c.mu.Unlock()
}
