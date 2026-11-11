// Package document decodes one configuration document without exposing input.
package document

import (
	"bytes"
	"errors"
)

// Decode reads one nonempty object using dec. Decoder diagnostics may contain
// secrets, so failures are replaced with fixed errors.
func Decode(raw []byte, dec func([]byte, any) error) (map[string]any, error) {
	if dec == nil {
		return nil, errors.New("decoder is nil")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("empty document")
	}
	var root any
	if err := dec(raw, &root); err != nil {
		return nil, errors.New("decode failed")
	}
	m, ok := root.(map[string]any)
	if !ok || m == nil {
		return nil, errors.New("root is not an object")
	}
	return m, nil
}
