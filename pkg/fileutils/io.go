package fileutils

import (
	"bufio"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"
)

const (
	dirPerm  = 0o755
	filePerm = 0o644
)

// Content is the set of types accepted as file content.
type Content interface {
	~string | ~[]byte
}

// Create creates or truncates the file at path, creating any missing parent
// directories first.
func Create(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, filePerm)
}

// WriteFile writes data to path, replacing any existing content. Missing parent
// directories are created. The mode of an existing file is left untouched.
func WriteFile[T Content](path string, data T) error {
	return write(path, data, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
}

// AppendFile appends data to path, creating the file and any missing parent
// directories if necessary.
func AppendFile[T Content](path string, data T) error {
	return write(path, data, os.O_WRONLY|os.O_CREATE|os.O_APPEND)
}

func write[T Content](path string, data T, flag int) error {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return err
	}
	f, err := os.OpenFile(path, flag, filePerm)
	if err != nil {
		return err
	}
	// data may be a named string type, so it cannot go to WriteString directly;
	// for []byte this conversion is free, which is the bulk-data case.
	_, err = f.Write([]byte(data)) //nolint:gocritic // see comment above
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Lines iterates over the lines of the file at path without loading it all
// into memory. Line terminators ("\n" or "\r\n") are stripped, and a final line
// without a terminator is still yielded. Lines may be arbitrarily long.
//
// The error, if any, is yielded once, as the last element, after which the
// iteration ends. Breaking out of the loop early closes the file.
//
//	for line, err := range fileutils.Lines(path) {
//		if err != nil {
//			return err
//		}
//		...
//	}
func Lines(path string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		f, err := os.Open(path)
		if err != nil {
			yield("", err)
			return
		}
		defer func() { _ = f.Close() }()

		r := bufio.NewReader(f)
		for {
			line, err := r.ReadString('\n')
			if line != "" {
				line = strings.TrimSuffix(line, "\n")
				line = strings.TrimSuffix(line, "\r")
				if !yield(line, nil) {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					yield("", err)
				}
				return
			}
		}
	}
}
