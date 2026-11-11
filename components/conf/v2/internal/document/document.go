// Package document decodes one configuration document without exposing input.
package document

import (
	"bytes"
	"errors"
	"fmt"
)

// SyntaxError carries only the 1-based line and byte column.
type SyntaxError struct{ Line, Column int }

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("decode failed at line %d, column %d", e.Line, e.Column)
}

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
		var location *SyntaxError
		if errors.As(err, &location) && location != nil && location.Line > 0 && location.Column > 0 {
			return nil, &SyntaxError{Line: location.Line, Column: location.Column}
		}
		return nil, errors.New("decode failed")
	}
	m, ok := root.(map[string]any)
	if !ok || m == nil {
		return nil, errors.New("root is not an object")
	}
	return m, nil
}
