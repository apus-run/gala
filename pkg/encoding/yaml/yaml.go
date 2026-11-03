// Package yaml registers the YAML codec | yaml 注册 YAML 编解码器.
//
// # Notes | 说明
//
// Importing this package registers the codec under [Name]. Encoding and
// decoding go straight to gopkg.in/yaml.v3 with its defaults; there is no
// option to customize the marshalling here.
//
// 导入本包即注册 [Name] 对应的编解码器。编解码直接委托给 gopkg.in/yaml.v3
// 的默认行为，本包不提供任何定制选项。
package yaml

import (
	"gopkg.in/yaml.v3"

	"github.com/apus-run/gala/pkg/encoding"
)

// Name is the name registered for the yaml codec.
// Name 是本包注册的编解码器名称.
const Name = "yaml"

func init() {
	encoding.RegisterCodec(codec{})
}

// codec is a Codec implementation with yaml.
// codec 是基于 gopkg.in/yaml.v3 的编解码器实现.
type codec struct{}

func (codec) Marshal(v any) ([]byte, error) {
	return yaml.Marshal(v)
}

func (codec) Unmarshal(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}

func (codec) Name() string {
	return Name
}
