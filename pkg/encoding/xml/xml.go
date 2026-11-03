// Package xml registers the XML codec | xml 注册 XML 编解码器.
//
// # Notes | 说明
//
// Importing this package registers the codec under [Name]. Encoding and
// decoding go straight to encoding/xml with its defaults; there is no option
// to customize the marshalling here.
//
// 导入本包即注册 [Name] 对应的编解码器。编解码直接委托给标准库 encoding/xml
// 的默认行为，本包不提供任何定制选项。
package xml

import (
	"encoding/xml"

	"github.com/apus-run/gala/pkg/encoding"
)

// Name is the name registered for the xml codec.
// Name 是本包注册的编解码器名称.
const Name = "xml"

func init() {
	encoding.RegisterCodec(codec{})
}

// codec is a Codec implementation with xml.
// codec 是基于标准库 encoding/xml 的编解码器实现.
type codec struct{}

func (codec) Marshal(v any) ([]byte, error) {
	return xml.Marshal(v)
}

func (codec) Unmarshal(data []byte, v any) error {
	return xml.Unmarshal(data, v)
}

func (codec) Name() string {
	return Name
}
