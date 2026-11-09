// Package document shares object decoding between document sources.
package document

import (
	"bytes"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/internal/diagnostic"
	"github.com/apus-run/gala/pkg/encoding"
)

// Decode rejects empty documents and non-object roots without exposing input.
func Decode(codec encoding.Codec, raw []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, conf.SourceError(conf.StageDecode, "", "", diagnostic.EmptyDocument)
	}
	var value any
	if err := codec.Unmarshal(raw, &value); err != nil {
		return nil, conf.SourceError(conf.StageDecode, "", "", diagnostic.InvalidDocument)
	}
	root, ok := value.(map[string]any)
	if !ok || root == nil {
		return nil, conf.SourceError(conf.StageDecode, "", "", diagnostic.NonObject)
	}
	return root, nil
}
