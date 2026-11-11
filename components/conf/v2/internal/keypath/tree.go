// Package keypath assembles string leaves into nested configuration objects.
package keypath

import (
	"errors"
	"strings"
)

// Put inserts a leaf and rejects empty segments, duplicate leaves and prefix
// collisions. Callers discard the candidate tree on error.
func Put(tree map[string]any, path, sep, value string) error {
	node, key, err := Leaf(tree, path, sep)
	if err == nil {
		node[key] = value
	}
	return err
}

// Leaf creates intermediate objects and locates an unoccupied leaf.
// The caller assigns the returned entry before inserting another path.
func Leaf(tree map[string]any, path, sep string) (map[string]any, string, error) {
	for {
		part, rest, more := strings.Cut(path, sep)
		if part == "" {
			return nil, "", errors.New("empty path segment")
		}
		prev, found := tree[part]
		if !more {
			if found {
				return nil, "", errors.New("conflicting paths")
			}
			return tree, part, nil
		}
		if !found {
			next := map[string]any{}
			tree[part] = next
			tree = next
		} else {
			next, ok := prev.(map[string]any)
			if !ok {
				return nil, "", errors.New("conflicting paths")
			}
			tree = next
		}
		path = rest
	}
}
