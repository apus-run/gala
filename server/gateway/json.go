package gateway

import (
	"io"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	jsonContentType string = "application/json"
)

// paralusJSON is the paralus object to json marshaller | paralusJSON 是 paralus 对象到 JSON 的序列化器.
type paralusJSON struct {
	jsonpb runtime.JSONPb
}

// NewParalusJSON returns new grpc gateway paralus json marshaller | NewParalusJSON 返回新的 grpc gateway paralus JSON 序列化器.
func NewParalusJSON() runtime.Marshaler {
	return &paralusJSON{
		jsonpb: runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				UseEnumNumbers: true,
			},
		},
	}
}

// ContentType returns the Content-Type which this marshaler is responsible for.
// ContentType 返回该序列化器负责的 Content-Type.
func (m *paralusJSON) ContentType(_ any) string {
	return jsonContentType
}

// Marshal marshals "v" into byte sequence | Marshal 把 v 序列化为字节序列.
func (m *paralusJSON) Marshal(v any) ([]byte, error) {
	return m.jsonpb.Marshal(v)
}

// Unmarshal unmarshals "data" into "v".
// "v" must be a pointer value.
// Unmarshal 把 data 反序列化进 v；v 必须是指针.
func (m *paralusJSON) Unmarshal(data []byte, v any) error {
	return m.jsonpb.Unmarshal(data, v)
}

// NewDecoder returns a Decoder which reads byte sequence from "r".
// NewDecoder 返回从 r 读取字节序列的 Decoder.
func (m *paralusJSON) NewDecoder(r io.Reader) runtime.Decoder {
	return m.jsonpb.NewDecoder(r)
}

// NewEncoder returns an Encoder which writes bytes sequence into "w".
// NewEncoder 返回向 w 写入字节序列的 Encoder.
func (m *paralusJSON) NewEncoder(w io.Writer) runtime.Encoder {
	return m.jsonpb.NewEncoder(w)
}

// Delimiter for newline encoded JSON streams | Delimiter 返回换行分隔 JSON 流的分隔符.
func (m *paralusJSON) Delimiter() []byte {
	return []byte("\n")
}
