// Package fileutils provides file-system helpers that go beyond what os and
// path/filepath already offer.
//
// The package follows one rule: a function exists only if it adds behavior to a
// single standard-library call (creating parent directories, predicates the
// standard library lacks, scanning, copying, glob matching, size formatting).
// Plain aliases such as Open, Stat, Chmod, Rename, Abs or Join are deliberately
// absent; use os and path/filepath directly. Errors are returned as produced by
// the standard library, which already carries the operation and the path.
//
// The package has no global state, no background goroutines and no dependencies
// outside the standard library.
package fileutils

import (
	"os"
	"path/filepath"
	"strings"
)

// Exists reports whether path exists. Symbolic links are followed.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDir reports whether path exists and is a directory.
func IsDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// IsFile reports whether path exists and is not a directory.
func IsFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// IsEmpty reports whether path is an empty directory or a zero-size file.
// It also returns true if path does not exist or cannot be inspected.
func IsEmpty(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return true
	}
	if !fi.IsDir() {
		return fi.Size() == 0
	}
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer func() { _ = f.Close() }()
	_, err = f.Readdirnames(1) // io.EOF when the directory has no entries
	return err != nil
}

// IsReadable reports whether path can be opened for reading.
func IsReadable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// IsWritable reports whether path can be written. For a directory it checks
// that a file can be created inside it; for a file, that it can be opened for
// writing (the file is not truncated). A missing path is not writable.
func IsWritable(path string) bool {
	if IsDir(path) {
		f, err := os.CreateTemp(path, ".writable-*")
		if err != nil {
			return false
		}
		name := f.Name()
		_ = f.Close()
		_ = os.Remove(name)
		return true
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// RealPath returns the absolute form of path, or "" if it does not exist.
func RealPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil || !Exists(abs) {
		return ""
	}
	return abs
}

// Name returns the last element of path without its extension.
//
//	Name("/var/www/file.js") // "file"
func Name(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// ExtName is like filepath.Ext but without the leading dot.
//
//	ExtName("main.go") // "go"
func ExtName(path string) string {
	return strings.TrimPrefix(filepath.Ext(path), ".")
}
