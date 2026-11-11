// Package etcd loads one document or a directory of string leaves from etcd.
// Sources borrow their client and never close it.
package etcd

import (
	"context"
	"errors"
	"io/fs"
	"strings"

	"github.com/apus-run/gala/components/conf/v2/internal/document"
	"github.com/apus-run/gala/components/conf/v2/internal/keypath"
	"github.com/apus-run/gala/components/conf/v2/internal/remote"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// Client is the read capability required for both keys and prefixes.
type Client interface {
	Get(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error)
}

// Source reads the target captured at construction. A concurrently shared
// client and decoder must support concurrent use.
type Source struct {
	client Client
	key    string
	prefix bool
	decode func([]byte, any) error
}

// New reads key as a document using dec. Invalid arguments fail on use.
func New(client Client, key string, dec func([]byte, any) error) *Source {
	return &Source{client: client, key: key, decode: dec}
}

// NewPrefix reads all leaves in prefix with one range request. A leading
// slash is literal and the directory separator is added if omitted.
func NewPrefix(client Client, prefix string) *Source {
	return &Source{client: client, key: remote.Directory(prefix), prefix: true}
}

func (s *Source) valid() bool {
	return s != nil && !remote.Nil(s.client) && s.key != "" && (s.prefix || s.decode != nil)
}

// Load returns a fresh layer. Missing targets match fs.ErrNotExist.
func (s *Source) Load(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.valid() {
		return nil, errors.New("invalid etcd source")
	}
	var opts []clientv3.OpOption
	if s.prefix {
		opts = append(opts, clientv3.WithPrefix())
	}
	r, err := s.client.Get(ctx, s.key, opts...)
	if err := remote.ReadError(ctx, err); err != nil {
		return nil, err
	}
	if r == nil || r.More {
		return nil, errors.New("incomplete etcd response")
	}
	if len(r.Kvs) == 0 {
		return nil, fs.ErrNotExist
	}
	if !s.prefix {
		if len(r.Kvs) != 1 || r.Kvs[0] == nil || string(r.Kvs[0].Key) != s.key {
			return nil, errors.New("invalid etcd response")
		}
		return document.Decode(r.Kvs[0].Value, s.decode)
	}
	tree := map[string]any{}
	for _, kv := range r.Kvs {
		if kv == nil {
			return nil, errors.New("invalid etcd response")
		}
		path, ok := strings.CutPrefix(string(kv.Key), s.key)
		if !ok {
			return nil, errors.New("key outside prefix")
		}
		if err := keypath.Put(tree, path, "/", string(kv.Value)); err != nil {
			return nil, err
		}
	}
	return tree, nil
}
