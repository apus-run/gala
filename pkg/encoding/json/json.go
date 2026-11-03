// Package json registers the JSON codec | json 注册 JSON 编解码器.
//
// # Notes | 说明
//
// Importing this package registers the codec under [Name]. It uses
// encoding/json/v2 defaults; values implementing json.Marshaler or
// json.Unmarshaler are dispatched straight to those methods. Unmarshal is a
// no-op on empty input. Unknown field handling and other options are the
// v2 defaults and are not configurable here.
//
// 导入本包即注册 [Name] 对应的编解码器。使用 encoding/json/v2 默认行为；
// 实现 json.Marshaler 或 json.Unmarshaler 的值直接走对应方法。
// Unmarshal 对空输入不做任何处理。未知字段等选项均为 v2 默认值，
// 本包不提供配置。
package json

import (
	"encoding/json/v2"

	"github.com/apus-run/gala/pkg/encoding"
)

// Name is the name registered for the json codec.
// Name 是本包注册的编解码器名称.
const Name = "json"

func init() {
	encoding.RegisterCodec(codec{})
}

// codec uses JSON v2 defaults, with direct calls to custom JSON methods.
// codec 使用 JSON v2 默认行为，实现自定义 JSON 方法的值直接调用对应方法.
type codec struct{}

func (codec) Marshal(v any) ([]byte, error) {
	switch m := v.(type) {
	case json.Marshaler:
		return m.MarshalJSON()
	default:
		return json.Marshal(m)
	}
}

func (codec) Unmarshal(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	switch m := v.(type) {
	case json.Unmarshaler:
		return m.UnmarshalJSON(data)
	default:
		return json.Unmarshal(data, m)
	}
}

func (codec) Name() string {
	return Name
}
