// Package consul loads one document from a Consul KV key using a borrowed client.
package consul

import (
	"context"
	"reflect"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/internal/diagnostic"
	"github.com/apus-run/gala/components/conf/v2/internal/document"
	"github.com/apus-run/gala/pkg/encoding"
	"github.com/hashicorp/consul/api"
)

// Client is implemented by *api.KV; pass client.KV() to New.
type Client interface {
	Get(string, *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error)
}

// Source reads one complete document on each Load.
type Source struct {
	client      Client
	key, format string
}

// New borrows client. Invalid parameters are reported by Load.
func New(client Client, key, format string) *Source {
	return &Source{client: client, key: key, format: format}
}

// Load reads once and decodes using the explicitly named registered codec.
func (s *Source) Load(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.client == nil || (reflect.ValueOf(s.client).Kind() == reflect.Pointer && reflect.ValueOf(s.client).IsNil()) || s.key == "" || s.format == "" {
		return nil, conf.SourceError(conf.StageConfig, "", "", nil)
	}
	codec := encoding.GetCodec(s.format)
	if codec == nil {
		return nil, conf.SourceError(conf.StageDecode, "", "", diagnostic.CodecMissing)
	}
	raw, err := s.ReadBytes(ctx)
	if err != nil {
		return nil, err
	}
	return document.Decode(codec, raw)
}

// NewReader borrows a client for use with sources/document and a codec instance.
func NewReader(client Client, key string) *Source { return New(client, key, "") }

// ReadBytes retrieves one key without selecting a codec.
func (s *Source) ReadBytes(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.client == nil || (reflect.ValueOf(s.client).Kind() == reflect.Pointer && reflect.ValueOf(s.client).IsNil()) || s.key == "" {
		return nil, conf.SourceError(conf.StageConfig, "", "", nil)
	}
	pair, _, err := s.client.Get(s.key, (&api.QueryOptions{}).WithContext(ctx))
	if cause := ctx.Err(); cause != nil {
		return nil, cause
	}
	if err != nil {
		return nil, conf.SourceError(conf.StageRead, "", "", err)
	}
	if pair == nil {
		return nil, conf.SourceError(conf.StageRead, "", "", nil)
	}
	return pair.Value, nil
}
