// Package document composes a byte reader with a codec instance. Neither the
// reader nor the codec has to be registered globally.
package document

import (
	"context"
	"reflect"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/internal/diagnostic"
	decoding "github.com/apus-run/gala/components/conf/v2/internal/document"
	"github.com/apus-run/gala/pkg/encoding"
)

// Reader returns one complete configuration document.
type Reader interface {
	ReadBytes(context.Context) ([]byte, error)
}

// ReaderFunc adapts a function to Reader.
type ReaderFunc func(context.Context) ([]byte, error)

func (f ReaderFunc) ReadBytes(ctx context.Context) ([]byte, error) { return f(ctx) }

// Source decodes a reader's documents using an instance-owned codec selection.
type Source struct {
	reader Reader
	codec  encoding.Codec
}

// New combines a reader and codec without registering either globally.
func New(reader Reader, codec encoding.Codec) *Source {
	return &Source{reader: reader, codec: codec}
}
func (s *Source) Load(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if nilValue(s.reader) {
		return nil, conf.SourceError(conf.StageConfig, "", "", nil)
	}
	if nilValue(s.codec) {
		return nil, conf.SourceError(conf.StageDecode, "", "", diagnostic.CodecMissing)
	}
	raw, err := s.reader.ReadBytes(ctx)
	if cause := ctx.Err(); cause != nil {
		return nil, cause
	}
	if err != nil {
		return nil, err
	}
	return decoding.Decode(s.codec, raw)
}
func (s *Source) Watch(ctx context.Context) (conf.Watcher, error) {
	if nilValue(s.reader) {
		return nil, conf.SourceError(conf.StageConfig, "", "", nil)
	}
	if source, ok := s.reader.(conf.Watchable); ok {
		return source.Watch(ctx)
	}
	return nil, conf.ErrWatchUnsupported
}
func nilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan, reflect.Interface:
		return rv.IsNil()
	}
	return false
}
