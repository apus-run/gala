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
	"github.com/apus-run/gala/components/conf/v2/internal/fileerr"
)

// Source borrows its filesystem and decoder. It is safe to share when both
// dependencies support concurrent use.
type Source struct {
	read     func(string) ([]byte, error)
	open     func(string) (fs.File, error)
	maxBytes int64
	name     string
	decode   func([]byte, any) error
}

// New reads path with os.ReadFile and uses dec on each Load.
func New(path string, dec func([]byte, any) error) *Source {
	return &Source{read: os.ReadFile, open: func(name string) (fs.File, error) { return os.Open(name) }, name: path, decode: dec}
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
	s.open = fsys.Open
	s.read = func(name string) ([]byte, error) { return fs.ReadFile(fsys, name) }
	return s
}

// WithMaxBytes returns an independently configured copy. A positive limit
// bounds bytes read before decoding. Zero uses the ordinary unbounded read.
func (s *Source) WithMaxBytes(n int64) *Source {
	next := *s
	next.maxBytes = n
	return &next
}

// ErrTooLarge means the bounded reader encountered an extra byte.
var ErrTooLarge = errors.New("file exceeds configured byte limit")

// SyntaxError identifies a JSON syntax error without retaining input text.
type SyntaxError = document.SyntaxError

// JSON decodes exactly one value. Syntax diagnostics contain only location.
func JSON(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(v); err != nil {
		var syntax *json.SyntaxError
		switch {
		case errors.As(err, &syntax):
			return location(data, int(syntax.Offset-1))
		case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
			return location(data, len(data))
		default:
			return errors.New("decode failed")
		}
	}
	for i := int(d.InputOffset()); i < len(data); i++ {
		switch data[i] {
		case ' ', '\t', '\n', '\r':
		default:
			return location(data, i)
		}
	}
	return nil
}

func location(data []byte, index int) *SyntaxError {
	prefix := data[:max(0, min(index, len(data)))]
	return &SyntaxError{Line: 1 + bytes.Count(prefix, []byte{'\n'}), Column: len(prefix) - bytes.LastIndexByte(prefix, '\n')}
}

func (s *Source) readFile() ([]byte, error) {
	if s.maxBytes == 0 {
		return s.read(s.name)
	}
	f, err := s.open(s.name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, s.maxBytes))
	if err != nil || int64(len(raw)) < s.maxBytes {
		return raw, err
	}
	var extra [1]byte
	if _, err = io.ReadFull(f, extra[:]); err == io.EOF {
		return raw, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, ErrTooLarge
}

// Load returns an owned object. File sentinels are preserved without exposing
// OS paths or decoder input.
func (s *Source) Load(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.read == nil || s.decode == nil || s.name == "" || s.maxBytes < 0 {
		return nil, errors.New("invalid file source")
	}
	raw, err := s.readFile()
	if cause := ctx.Err(); cause != nil {
		return nil, cause
	}
	switch {
	case err == nil:
		return document.Decode(raw, s.decode)
	case errors.Is(err, context.Canceled):
		return nil, context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return nil, context.DeadlineExceeded
	case errors.Is(err, ErrTooLarge):
		return nil, ErrTooLarge
	case fileerr.OnlyMissing(err):
		return nil, fs.ErrNotExist
	case errors.Is(err, fs.ErrPermission):
		return nil, fs.ErrPermission
	default:
		return nil, errors.New("read failed")
	}
}
