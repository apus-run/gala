// Package file reads a document from an OS path or fs.FS on every load.
package file

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"reflect"

	"github.com/apus-run/gala/components/conf/v2/internal/document"
)

// Source borrows its filesystem and decoder. It is safe to share when both
// dependencies support concurrent use.
type Source struct {
	read   func(string) ([]byte, error)
	name   string
	decode func([]byte, any) error
}

// New reads path with os.ReadFile and uses dec on each Load.
func New(path string, dec func([]byte, any) error) *Source {
	return &Source{read: os.ReadFile, name: path, decode: dec}
}

// FS reads name from fsys. Invalid parameters are reported by Load.
func FS(fsys fs.FS, name string, dec func([]byte, any) error) *Source {
	s := &Source{name: name, decode: dec}
	if fsys == nil {
		return s
	}
	v := reflect.ValueOf(fsys)
	switch v.Kind() {
	case reflect.Pointer, reflect.Func:
		if v.IsNil() {
			return s
		}
	}
	s.read = func(name string) ([]byte, error) { return fs.ReadFile(fsys, name) }
	return s
}

// JSON decodes exactly one JSON value, preserving numbers in interface values.
func JSON(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(v); err != nil {
		return errors.New("decode failed")
	}
	var extra json.RawMessage
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("decode failed")
	}
	return nil
}

// Load returns an owned object. File sentinels are preserved without exposing
// OS paths or decoder input.
func (s *Source) Load(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.read == nil || s.decode == nil || s.name == "" {
		return nil, errors.New("invalid file source")
	}
	raw, err := s.read(s.name)
	if cause := ctx.Err(); cause != nil {
		return nil, cause
	}
	switch {
	case err == nil:
		return document.Decode(raw, s.decode)
	case errors.Is(err, fs.ErrNotExist):
		return nil, fs.ErrNotExist
	case errors.Is(err, fs.ErrPermission):
		return nil, fs.ErrPermission
	default:
		return nil, errors.New("read failed")
	}
}
