// Package protojson registers the protobuf JSON codec | protojson 注册 protobuf JSON 编解码器.
//
// # Notes | 说明
//
// Importing this package registers the codec under [Name]. Marshalling emits
// unpopulated fields ([MarshalOptions].EmitUnpopulated) and unmarshalling
// discards unknown fields ([UnmarshalOptions].DiscardUnknown); the options
// are package-level variables and can be adjusted before use. Unmarshal is a
// no-op on empty input. Only proto.Message values are accepted — anything
// else fails with an error.
//
// 导入本包即注册 [Name] 对应的编解码器。编码时输出未赋值字段
// （[MarshalOptions].EmitUnpopulated），解码时忽略未知字段
// （[UnmarshalOptions].DiscardUnknown）；两个选项是包级变量，可按需调整。
// Unmarshal 对空输入不做任何处理。仅接受 proto.Message 值，
// 其他类型报错。
package protojson

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/apus-run/gala/pkg/encoding"
)

// Name is the name registered for the protojson codec.
// Name 是本包注册的编解码器名称.
const Name = "protojson"

var (
	// MarshalOptions is a configurable JSON format marshaller.
	// MarshalOptions 是可配置的 JSON 格式编码器.
	MarshalOptions = protojson.MarshalOptions{
		EmitUnpopulated: true,
	}
	// UnmarshalOptions is a configurable JSON format parser.
	// UnmarshalOptions 是可配置的 JSON 格式解析器.
	UnmarshalOptions = protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}
)

func init() {
	encoding.RegisterCodec(codec{})
}

// codec is a Codec implementation with protojson.
// codec 是基于 protojson 的编解码器实现.
type codec struct{}

func (codec) Marshal(v any) ([]byte, error) {
	m, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("failed to marshal, message is %T, want proto.Message", v)
	}
	return MarshalOptions.Marshal(m)
}

func (codec) Unmarshal(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	m, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("failed to unmarshal, message is %T, want proto.Message", v)
	}
	return UnmarshalOptions.Unmarshal(data, m)
}

func (codec) Name() string {
	return Name
}
