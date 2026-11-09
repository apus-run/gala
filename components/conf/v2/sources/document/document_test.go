package document_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/document"
)

type codec struct{}

func (codec) Name() string                    { return "unregistered-instance" }
func (codec) Marshal(v any) ([]byte, error)   { return json.Marshal(v) }
func (codec) Unmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
func TestReaderCodecComposition(t *testing.T) {
	calls := 0
	reader := document.ReaderFunc(func(context.Context) ([]byte, error) { calls++; return []byte(`{"port":8080}`), nil })
	type app struct {
		Port int `mapstructure:"port"`
	}
	c, err := conf.Open[app](t.Context(), conf.WithSources(document.New(reader, codec{})))
	if err != nil || c.Current().Port != 8080 || calls != 1 {
		t.Fatalf("composition: %v calls=%d", err, calls)
	}
	defer c.Close()
	_, err = conf.Open[app](t.Context(), conf.WithSources(document.New(reader, nil)))
	if err == nil || calls != 1 {
		t.Fatal("missing codec read the source")
	}
	var nilReader document.ReaderFunc
	_, err = conf.Open[app](t.Context(), conf.WithSources(document.New(nilReader, codec{})))
	if err == nil {
		t.Fatal("nil reader accepted")
	}
	_, err = document.New(reader, codec{}).Watch(t.Context())
	if !errors.Is(err, conf.ErrWatchUnsupported) {
		t.Fatalf("watch capability: %v", err)
	}
}
