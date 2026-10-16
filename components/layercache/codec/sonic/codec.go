// Package sonic provides the optional Sonic JSON codec without a global registry.
package sonic

import "github.com/bytedance/sonic"

type Codec struct{}

func (Codec) Marshal(v any) ([]byte, error)   { return sonic.Marshal(v) }
func (Codec) Unmarshal(b []byte, v any) error { return sonic.Unmarshal(b, v) }
