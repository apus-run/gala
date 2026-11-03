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
// Cache 把文件内容保存在内存里，只在大小或修改时间变化时重读文件。
// 没有 TTL、没有 watcher、没有后台 goroutine：文件未变化时每次 ReadFile
// 只花一次 stat 调用的代价，再无其他开销。
//
// The zero value is ready to use and safe for concurrent use. Intended for a
// small, long-lived set of files such as templates or configuration. A write
// that keeps the size and lands within the file system's timestamp granularity
// of the previous one may go unnoticed.
// 零值即可用，且并发安全。适合少量长期存在的文件，例如模板或配置。
// 若一次写入保持文件大小不变、且落在文件系统时间戳精度之内，可能不被察觉。
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
// ReadFile 返回 path 的内容；返回的切片与缓存共享，不得修改。
func (c *Cache) ReadFile(path string) ([]byte, error) {
	// Keyed by absolute path, so the same relative name read from another
	// directory after a chdir does not get this file's entry.
	// 以绝对路径为键，chdir 之后从其他目录读同名相对路径时，
	// 不会命中本文件的缓存条目。
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
