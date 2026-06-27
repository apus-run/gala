// Package proto defines the protobuf codec. Importing this package will
// register the codec.
package proto

import (
	"errors"
	"fmt"
	"reflect"

	"google.golang.org/protobuf/proto"

	"github.com/apus-run/gala/plugins/encoding"
)

// Name is the name registered for the proto compressor.
const Name = "proto"

func init() {
	encoding.RegisterCodec(codec{})
}

// codec is a Codec implementation with protobuf. It is the default codec for Transport.
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
