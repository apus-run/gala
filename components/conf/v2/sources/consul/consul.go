// Package consul reads a document or directory from Consul KV.
// For ACL-protected prefixes use a non-anonymous token and forward Consul's
// query metadata, including LastContact and Results-Filtered-By-ACLs, intact.
// Complete prefix reads require a server supporting ACL filtering metadata.
package consul

import (
	"context"
	"errors"
	"io/fs"
	"strings"

	"github.com/apus-run/gala/components/conf/v2/internal/document"
	"github.com/apus-run/gala/components/conf/v2/internal/keypath"
	"github.com/apus-run/gala/components/conf/v2/internal/remote"
	"github.com/hashicorp/consul/api"
)

// Client provides single-document reads.
type Client interface {
	Get(string, *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error)
}

// Lister provides directory reads.
type Lister interface {
	List(string, *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error)
}

// Source borrows its client and decoder. Share only concurrency-safe dependencies.
type Source struct {
	key    string
	read   func(string, *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error)
	decode func([]byte, any) error
}

// New reads one document. Invalid arguments fail on use.
func New(client Client, key string, dec func([]byte, any) error) *Source {
	s := &Source{key: strings.TrimPrefix(key, "/"), decode: dec}
	if !remote.Nil(client) && dec != nil {
		s.read = func(key string, opts *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error) {
			pair, meta, err := client.Get(key, opts)
			if pair == nil {
				return nil, meta, err
			}
			return api.KVPairs{pair}, meta, err
		}
	}
	return s
}

// NewPrefix reads prefix as a directory. One leading slash is optional.
// An empty prefix or root directory is invalid.
func NewPrefix(client Lister, prefix string) *Source {
	key := strings.TrimPrefix(prefix, "/")
	if strings.HasPrefix(key, "/") {
		key = ""
	}
	s := &Source{key: remote.Directory(key)}
	if !remote.Nil(client) {
		s.read = client.List
	}
	return s
}

func (s *Source) valid() bool {
	return s != nil && s.key != "" && s.read != nil
}

// query is shared by loading and blocking watches, so neither can silently
// accept partial ACL results or use a less consistent view of the keyspace.
func (s *Source) query(ctx context.Context, index uint64) (api.KVPairs, uint64, error) {
	opts := (&api.QueryOptions{RequireConsistent: true, WaitIndex: index}).WithContext(ctx)
	pairs, meta, err := s.read(s.key, opts)
	if err := remote.ReadError(ctx, err); err != nil {
		return nil, 0, err
	}
	if meta == nil {
		return nil, 0, errors.New("missing query metadata")
	}
	if meta.ResultsFilteredByACLs {
		return nil, 0, fs.ErrPermission
	}
	return pairs, meta.LastIndex, nil
}

// Load does one consistent read. Missing targets match fs.ErrNotExist.
// Filtered prefix responses match fs.ErrPermission.
func (s *Source) Load(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.valid() {
		return nil, errors.New("invalid consul source")
	}
	pairs, _, err := s.query(ctx, 0)
	if err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, fs.ErrNotExist
	}
	if s.decode != nil {
		if pairs[0].Key != s.key {
			return nil, errors.New("invalid consul response")
		}
		return document.Decode(pairs[0].Value, s.decode)
	}
	tree := map[string]any{}
	for _, p := range pairs {
		if p == nil {
			return nil, errors.New("invalid consul response")
		}
		path, ok := strings.CutPrefix(p.Key, s.key)
		if !ok {
			return nil, errors.New("key outside prefix")
		}
		if strings.HasSuffix(p.Key, "/") && len(p.Value) == 0 {
			continue
		}
		if err := keypath.Put(tree, path, "/", string(p.Value)); err != nil {
			return nil, err
		}
	}
	if len(tree) == 0 {
		return nil, fs.ErrNotExist
	}
	return tree, nil
}
