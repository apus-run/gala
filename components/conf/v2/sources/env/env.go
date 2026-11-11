// Package env maps environment variable names to dotted configuration paths.
package env

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"unicode"

	"github.com/apus-run/gala/components/conf/v2/internal/keypath"
)

// Source loads a fresh configuration layer. Its zero value yields an empty layer.
type Source struct {
	load func(context.Context) (map[string]any, error)
}

type binding struct {
	name string
	path string
}

// New captures bindings and reads the process environment on each Load.
func New(bindings map[string]string) *Source { return NewWithLookup(bindings, os.LookupEnv) }

// NewWithLookup captures the bindings in name order. Load reports invalid
// paths before calling lookup. A nil lookup uses os.LookupEnv.
func NewWithLookup(bindings map[string]string, lookup func(string) (string, bool)) *Source {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	ordered, err := prepareBindings(bindings)
	return &Source{load: func(ctx context.Context) (map[string]any, error) {
		if err != nil {
			return nil, err
		}
		return readBindings(ctx, ordered, lookup)
	}}
}

// Scan calls environ on every load and lets mapping select variables and
// their paths. Both callbacks are required and must be safe if shared.
func Scan(environ func() []string, mapping func(string) (string, bool)) *Source {
	return &Source{load: func(ctx context.Context) (map[string]any, error) {
		if environ == nil || mapping == nil {
			return nil, errors.New("invalid scan callbacks")
		}
		bindings := make(map[string]string)
		values := make(map[string]string)
		for _, entry := range environ() {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			name, value, ok := strings.Cut(entry, "=")
			if !ok {
				continue
			}
			if path, use := mapping(name); use {
				if _, dup := bindings[name]; dup {
					return nil, errors.New("duplicate environment variable")
				}
				bindings[name], values[name] = path, value
			}
		}
		ordered, err := prepareBindings(bindings)
		if err != nil {
			return nil, err
		}
		return readBindings(ctx, ordered, func(name string) (string, bool) {
			value, ok := values[name]
			return value, ok
		})
	}}
}

// Load returns a private tree. Missing and empty values are absent.
func (s *Source) Load(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.load == nil {
		return map[string]any{}, nil
	}
	return s.load(ctx)
}

func prepareBindings(bindings map[string]string) ([]binding, error) {
	ordered := make([]binding, 0, len(bindings))
	tree := make(map[string]any)
	for _, name := range slices.Sorted(maps.Keys(bindings)) {
		path := bindings[name]
		if err := keypath.Put(tree, path, ".", ""); err != nil {
			if strings.IndexFunc(name, unicode.IsControl) >= 0 {
				return nil, err
			}
			return nil, fmt.Errorf("environment variable %s: %w", name, err)
		}
		ordered = append(ordered, binding{name: name, path: path})
	}
	return ordered, nil
}

func readBindings(ctx context.Context, bindings []binding, lookup func(string) (string, bool)) (map[string]any, error) {
	tree := make(map[string]any)
	for _, b := range bindings {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if value, ok := lookup(b.name); ok && value != "" {
			if err := keypath.Put(tree, b.path, ".", value); err != nil {
				return nil, err
			}
		}
	}
	return tree, nil
}
