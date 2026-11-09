// Package toml registers the TOML codec with encoding.
package toml

import (
	"github.com/pelletier/go-toml/v2"

	"github.com/apus-run/gala/pkg/encoding"
)

const Name = "toml"

type codec struct{}

func init()                                      { encoding.RegisterCodec(codec{}) }
func (codec) Name() string                       { return Name }
func (codec) Marshal(v any) ([]byte, error)      { return toml.Marshal(v) }
func (codec) Unmarshal(data []byte, v any) error { return toml.Unmarshal(data, v) }
