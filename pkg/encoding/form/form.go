// Package form registers the x-www-form-urlencoded codec | form 注册
// x-www-form-urlencoded 编解码器.
//
// # Notes | 说明
//
// Importing this package registers the codec under [Name]. proto.Message
// values are encoded and decoded field by field via [EncodeValues] and
// [DecodeValues]: nested fields are joined with ".", map entries are written
// as "key[sub]", and well-known types (Timestamp, Duration, wrappers, Bytes,
// FieldMask, Struct, Value) get dedicated handling. Every other value goes
// through go-playground/form using the struct tag name "json". Marshal drops
// parameters whose value list is empty. Only urlencoded form encoding is
// supported, not multipart/form-data.
//
// 导入本包即注册 [Name] 对应的编解码器。proto.Message 经 [EncodeValues] 与
// [DecodeValues] 逐字段编解码：嵌套字段用 "." 连接，map 项写作 "key[sub]"，
// Timestamp、Duration、wrappers、Bytes、FieldMask、Struct、Value 等
// well-known 类型有专门处理；其余值走 go-playground/form，结构体标签名为
// "json"。Marshal 会丢弃取值为空的参数。仅支持 urlencoded 表单，
// 不支持 multipart/form-data。
package form

import (
	"errors"
	"net/url"
	"reflect"

	"github.com/go-playground/form/v4"
	"google.golang.org/protobuf/proto"

	"github.com/apus-run/gala/pkg/encoding"
)

const (
	// Name is the codec name registered by this package | 本包注册的编解码器名称.
	Name = "x-www-form-urlencoded"
	// nullStr is the literal standing in for a null value | 表示 null 值的字面量.
	nullStr = "null"
)

var (
	encoder = form.NewEncoder()
	decoder = form.NewDecoder()
)

// This variable can be replaced with -ldflags like below:
// go build "-ldflags=-X github.com/apus-run/gala/pkg/encoding/form.tagName=form"
//
// 此变量可在构建时用 -ldflags 覆盖，如下所示：
// go build "-ldflags=-X github.com/apus-run/gala/pkg/encoding/form.tagName=form"
var tagName = "json"

func init() {
	decoder.SetTagName(tagName)
	encoder.SetTagName(tagName)
	encoding.RegisterCodec(codec{encoder: encoder, decoder: decoder})
}

type codec struct {
	encoder *form.Encoder
	decoder *form.Decoder
}

// Marshal encodes v as a urlencoded form, as described in the package docs | 将 v 编码为 urlencoded 表单，行为见包文档.
func (c codec) Marshal(v any) ([]byte, error) {
	var vs url.Values
	var err error
	if m, ok := v.(proto.Message); ok {
		vs, err = EncodeValues(m)
		if err != nil {
			return nil, err
		}
	} else {
		vs, err = c.encoder.Encode(v)
		if err != nil {
			return nil, err
		}
	}
	for k, v := range vs {
		if len(v) == 0 {
			delete(vs, k)
		}
	}
	return []byte(vs.Encode()), nil
}

// Unmarshal decodes urlencoded form data into v, as described in the package docs | 将 urlencoded 表单数据解码到 v，行为见包文档.
func (c codec) Unmarshal(data []byte, v any) error {
	vs, err := url.ParseQuery(string(data))
	if err != nil {
		return err
	}

	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return errors.New("form: unmarshal target is nil")
	}
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			if !rv.CanSet() {
				return errors.New("form: unmarshal target is nil pointer")
			}
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		rv = rv.Elem()
	}
	if m, ok := v.(proto.Message); ok {
		return DecodeValues(m, vs)
	}
	if m, ok := rv.Interface().(proto.Message); ok {
		return DecodeValues(m, vs)
	}

	return c.decoder.Decode(v, vs)
}

// Name implements encoding.Codec | 实现 encoding.Codec.
func (codec) Name() string {
	return Name
}
