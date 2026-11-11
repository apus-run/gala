// Package flagset loads explicitly set flags as dotted configuration paths.
package flagset

import (
	"context"
	"errors"
	"flag"

	"github.com/apus-run/gala/components/conf/v2/internal/keypath"
)

// Source borrows a flag set. Finish parsing and modifying it before loading.
type Source struct{ flags *flag.FlagSet }

// New creates a source. Load reports nil or unparsed flag sets.
func New(fs *flag.FlagSet) *Source { return &Source{flags: fs} }

// Load returns a fresh tree containing only explicitly set flags.
func (s *Source) Load(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.flags == nil || !s.flags.Parsed() {
		return nil, errors.New("parsed flag set required")
	}
	tree := map[string]any{}
	// Reserve all paths before requesting values from user-defined flag.Value.
	type binding struct {
		key  string
		node map[string]any
		flag *flag.Flag
	}
	var leaves []binding
	var err error
	s.flags.Visit(func(f *flag.Flag) {
		if err != nil {
			return
		}
		var node map[string]any
		var key string
		node, key, err = keypath.Leaf(tree, f.Name, ".")
		if err == nil {
			node[key] = ""
			leaves = append(leaves, binding{key, node, f})
		}
	})
	if err != nil {
		return nil, err
	}
	for _, leaf := range leaves {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		leaf.node[leaf.key] = leaf.flag.Value.String()
	}
	return tree, nil
}
