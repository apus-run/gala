package fileutils

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	ownerRWX fs.FileMode = 0o700
	ownerX   fs.FileMode = 0o100
)

// CopyOption tunes Copy.
type CopyOption struct {
	// Sync flushes every copied file to stable storage before moving on.
	Sync bool

	// Mode is the permission given to copied files and created directories
	// (directories keep at least the owner's search bit). Zero keeps the
	// permission of the source.
	Mode fs.FileMode
}

// Copy copies the file or directory src to dst, creating missing parents.
//
//	Copy("/a/f", "/b/g")     // file to a new or existing file
//	Copy("/a/f", "/b")       // /b is an existing directory: copies to /b/f
//	Copy("/a/d", "/b/d2")    // directory tree, merged into d2 if it exists
//	Copy("/a/d", "/b/file")  // error: /b/file exists and is not a directory
//
// Symbolic links and special files found inside a directory are skipped. Copying
// a path onto itself is a no-op; copying a directory into itself is an error.
func Copy(src, dst string, opt ...CopyOption) error {
	var o CopyOption
	if len(opt) > 0 {
		o = opt[0]
	}
	if src == "" || dst == "" {
		return errors.New("fileutils: copy: source and destination must not be empty")
	}
	si, err := os.Stat(src)
	if err != nil {
		return err
	}
	if di, err := os.Stat(dst); err == nil {
		switch {
		case si.IsDir() && !di.IsDir():
			return fmt.Errorf("fileutils: cannot copy directory %q to file %q", src, dst)
		case !si.IsDir() && di.IsDir():
			dst = filepath.Join(dst, filepath.Base(src))
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if si.IsDir() {
		return copyDir(src, dst, si, o)
	}
	return copyFile(src, dst, si, o)
}

func copyFile(src, dst string, si fs.FileInfo, o CopyOption) (err error) {
	if di, e := os.Stat(dst); e == nil && os.SameFile(si, di) {
		return nil // copying a file onto itself would truncate it
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()

	_, err = io.Copy(out, in)
	if err == nil && o.Sync {
		err = out.Sync()
	}
	if err == nil {
		err = out.Chmod(permOf(si, o))
	}
	return err
}

func copyDir(src, dst string, si fs.FileInfo, o CopyOption) error {
	if isInside(src, dst) {
		return fmt.Errorf("fileutils: cannot copy directory %q into itself (%q)", src, dst)
	}
	perm := permOf(si, o)
	_, statErr := os.Stat(dst)
	created := errors.Is(statErr, fs.ErrNotExist)
	// Created owner-writable so a read-only source can still be filled in.
	if err := os.MkdirAll(dst, perm|ownerRWX); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		t := e.Type()
		if !t.IsDir() && !t.IsRegular() {
			continue // symlinks and special files
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		s, d := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		if t.IsDir() {
			err = copyDir(s, d, info, o)
		} else {
			err = copyFile(s, d, info, o)
		}
		if err != nil {
			return err
		}
	}
	if created {
		// Without the owner search bit the directory cannot be entered at all.
		return os.Chmod(dst, perm|ownerX)
	}
	return nil
}

func permOf(si fs.FileInfo, o CopyOption) fs.FileMode {
	if o.Mode != 0 {
		return o.Mode
	}
	return si.Mode().Perm()
}

// isInside reports whether dst is src or lies beneath it.
func isInside(src, dst string) bool {
	s, err1 := filepath.Abs(src)
	d, err2 := filepath.Abs(dst)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(s, d)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
