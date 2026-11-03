package form

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// EncodeValues encode a message into url values.
// EncodeValues 将消息编码为 url.Values.
//
// proto.Message values are encoded field by field (empty messages yield an
// empty Values); other values are passed to the go-playground/form encoder.
// proto.Message 逐字段编码（nil 或空指针得到空的 Values）；其余值交给
// go-playground/form 编码器处理。
func EncodeValues(msg any) (url.Values, error) {
	if msg == nil || (reflect.ValueOf(msg).Kind() == reflect.Pointer && reflect.ValueOf(msg).IsNil()) {
		return url.Values{}, nil
	}
	if v, ok := msg.(proto.Message); ok {
		u := make(url.Values)
		err := encodeByField(u, "", v.ProtoReflect())
		return u, err
	}
	return encoder.Encode(msg)
}

func encodeByField(u url.Values, path string, m protoreflect.Message) (finalErr error) {
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		var (
			key     string
			newPath string
		)
		if fd.HasJSONName() {
			key = fd.JSONName()
		} else {
			key = fd.TextName()
		}
		if path == "" {
			newPath = key
		} else {
			newPath = path + "." + key
		}
		if of := fd.ContainingOneof(); of != nil {
			if f := m.WhichOneof(of); f != nil && f != fd {
				return true
			}
		}
		switch {
		case fd.IsList():
			if v.List().Len() > 0 {
				list, err := encodeRepeatedField(fd, v.List())
				if err != nil {
					finalErr = err
				}
				for _, item := range list {
					u.Add(newPath, item)
				}
			}
		case fd.IsMap():
			if v.Map().Len() > 0 {
				m, err := encodeMapField(fd, v.Map())
				if err != nil {
					finalErr = err
				}
				for k, value := range m {
					u.Set(newPath+"["+k+"]", value)
				}
			}
		case (fd.Kind() == protoreflect.MessageKind) || (fd.Kind() == protoreflect.GroupKind):
			value, err := encodeMessage(fd.Message(), v)
			if err == nil {
				u.Set(newPath, value)
				return true
			}
			if err = encodeByField(u, newPath, v.Message()); err != nil {
				finalErr = err
			}
		default:
			value, err := EncodeField(fd, v)
			if err != nil {
				finalErr = err
			}
			u.Set(newPath, value)
		}
		return true
	})
	return
}

func encodeRepeatedField(fieldDescriptor protoreflect.FieldDescriptor, list protoreflect.List) ([]string, error) {
	var values []string
	for i := 0; i < list.Len(); i++ {
		value, err := EncodeField(fieldDescriptor, list.Get(i))
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func encodeMapField(fieldDescriptor protoreflect.FieldDescriptor, mp protoreflect.Map) (map[string]string, error) {
	m := make(map[string]string)
	mp.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
		key, err := EncodeField(fieldDescriptor.MapKey(), k.Value())
		if err != nil {
			return false
		}
		value, err := EncodeField(fieldDescriptor.MapValue(), v)
		if err != nil {
			return false
		}
		m[key] = value
		return true
	})

	return m, nil
}

// EncodeField encode proto message filed
// EncodeField 编码 proto 消息的单个字段.
//
// Enums encode as their value name, bytes as base64 (URL-safe), and
// well-known messages via [encodeMessage].
// 枚举编码为其值名，bytes 编码为 base64（URL 安全），well-known 消息
// 经 [encodeMessage] 处理。
func EncodeField(fieldDescriptor protoreflect.FieldDescriptor, value protoreflect.Value) (string, error) {
	switch fieldDescriptor.Kind() {
	case protoreflect.BoolKind:
		return strconv.FormatBool(value.Bool()), nil
	case protoreflect.EnumKind:
		if fieldDescriptor.Enum().FullName() == "google.protobuf.NullValue" {
			return nullStr, nil
		}
		desc := fieldDescriptor.Enum().Values().ByNumber(value.Enum())
		return string(desc.Name()), nil
	case protoreflect.BytesKind:
		return base64.URLEncoding.EncodeToString(value.Bytes()), nil
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return encodeMessage(fieldDescriptor.Message(), value)
	default:
		return value.String(), nil
	}
}

// encodeMessage marshals a well-known message into its scalar string form.
// encodeMessage 将 well-known 消息编码为其标量字符串形式.
//
// Supported: Timestamp, Duration, BytesValue, the scalar wrappers and
// FieldMask; any other message type returns an error.
// 支持 Timestamp、Duration、BytesValue、各标量 wrapper 与 FieldMask；
// 其余消息类型返回错误。
func encodeMessage(msgDescriptor protoreflect.MessageDescriptor, value protoreflect.Value) (string, error) {
	switch msgDescriptor.FullName() {
	case timestampMessageFullname:
		return marshalTimestamp(value.Message())
	case durationMessageFullname:
		return marshalDuration(value.Message())
	case bytesMessageFullname:
		return marshalBytes(value.Message())
	case "google.protobuf.DoubleValue", "google.protobuf.FloatValue", "google.protobuf.Int64Value", "google.protobuf.Int32Value",
		"google.protobuf.UInt64Value", "google.protobuf.UInt32Value", "google.protobuf.BoolValue", "google.protobuf.StringValue":
		fd := msgDescriptor.Fields()
		v := value.Message().Get(fd.ByName("value"))
		return fmt.Sprint(v.Interface()), nil
	case fieldMaskFullName:
		m, ok := value.Message().Interface().(*fieldmaskpb.FieldMask)
		if !ok || m == nil {
			return "", nil
		}
		for i, v := range m.Paths {
			m.Paths[i] = jsonCamelCase(v)
		}
		return strings.Join(m.Paths, ","), nil
	default:
		return "", fmt.Errorf("unsupported message type: %q", string(msgDescriptor.FullName()))
	}
}

// EncodeFieldMask return field mask name=paths
// EncodeFieldMask 将 FieldMask 字段编码为 "name=paths" 查询串.
//
// Encoding stops at the first FieldMask field; the result is empty when the
// message has none or the paths fail to encode.
// 找到第一个 FieldMask 字段即停止；消息不含该字段或路径编码失败时结果为空。
func EncodeFieldMask(m protoreflect.Message) (query string) {
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() == protoreflect.MessageKind {
			if msg := fd.Message(); msg.FullName() == fieldMaskFullName {
				value, err := encodeMessage(msg, v)
				if err != nil {
					return false
				}
				if fd.HasJSONName() {
					query = fd.JSONName() + "=" + value
				} else {
					query = fd.TextName() + "=" + value
				}
				return false
			}
		}
		return true
	})
	return
}

// jsonCamelCase converts a snake_case identifier to a camelCase identifier,
// according to the protobuf JSON specification.
// references: https://github.com/protocolbuffers/protobuf-go/blob/master/encoding/protojson/well_known_types.go#L842
// jsonCamelCase 按 protobuf JSON 规范把 snake_case 标识符转为 camelCase 标识符.
// 参考：https://github.com/protocolbuffers/protobuf-go/blob/master/encoding/protojson/well_known_types.go#L842
func jsonCamelCase(s string) string {
	var builder strings.Builder
	builder.Grow(len(s))

	wasUnderscore := false
	// proto identifiers are always ASCIIS | proto 标识符恒为 ASCII
	for i := range len(s) {
		c := s[i]
		if c != '_' {
			if wasUnderscore && isASCIILower(c) {
				// convert to uppercase | 转为大写
				c -= 'a' - 'A'
			}
			builder.WriteByte(c)
		}
		wasUnderscore = c == '_'
	}

	return builder.String()
}

func isASCIILower(c byte) bool {
	return 'a' <= c && c <= 'z'
}
