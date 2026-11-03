// Package proto registers the protobuf binary codec | proto 注册 protobuf 二进制编解码器.
//
// # Notes | 说明
//
// Importing this package registers the codec under [Name]; it is the default
// codec for Transport. It only operates on google.golang.org/protobuf
// messages: values that are not proto.Message (or a pointer that eventually
// resolves to one) fail with an error, and nil messages are rejected.
//
// 导入本包即注册 [Name] 对应的编解码器，它是 Transport 的默认编解码器。
// 仅支持 google.golang.org/protobuf 消息：非 proto.Message（或无法解引用到
// proto.Message）的值会报错，nil 消息会被拒绝。
package proto

import (
	"errors"
	"fmt"
	"reflect"

	"google.golang.org/protobuf/proto"

	"github.com/apus-run/gala/pkg/encoding"
)

// Name is the name registered for the proto codec.
// Name 是本包注册的编解码器名称.
const Name = "proto"

func init() {
	encoding.RegisterCodec(codec{})
}

// codec is a Codec implementation with protobuf. It is the default codec for Transport.
// codec 是基于 protobuf 的编解码器实现，也是 Transport 的默认编解码器.
type codec struct{}

func (codec) Marshal(v any) ([]byte, error) {
	pm, err := getProtoMessage(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal: %w", err)
	}
	return proto.Marshal(pm)
}

func (codec) Unmarshal(data []byte, v any) error {
	pm, err := getProtoMessage(v)
	if err != nil {
		return err
	}
	return proto.Unmarshal(data, pm)
}

func (codec) Name() string {
	return Name
}

func getProtoMessage(v any) (proto.Message, error) {
	if v == nil {
		return nil, errors.New("not proto message")
	}
	if msg, ok := v.(proto.Message); ok {
		if isNilPointer(msg) {
			return nil, errors.New("nil proto message")
		}
		return msg, nil
	}
	val := reflect.ValueOf(v)
	if !val.IsValid() || val.Kind() != reflect.Pointer {
		return nil, errors.New("not proto message")
	}
	if val.IsNil() {
		return nil, errors.New("nil proto message")
	}

	val = val.Elem()
	return getProtoMessage(val.Interface())
}

func isNilPointer(v any) bool {
	val := reflect.ValueOf(v)
	return val.Kind() == reflect.Pointer && val.IsNil()
}
