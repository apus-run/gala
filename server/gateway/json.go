package gateway

import (
	"io"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	jsonContentType string = "application/json"
)

// paralusJSON is the paralus object to json marshaller
type paralusJSON struct {
	jsonpb runtime.JSONPb
}

// NewParalusJSON returns new grpc gateway paralus json marshaller
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
func (m *paralusJSON) ContentType(_ interface{}) string {
	return jsonContentType
}

// Marshal marshals "v" into byte sequence.
func (m *paralusJSON) Marshal(v interface{}) ([]byte, error) {
	return m.jsonpb.Marshal(v)
}

// Unmarshal unmarshals "data" into "v".
// "v" must be a pointer value.
func (m *paralusJSON) Unmarshal(data []byte, v interface{}) error {
	return m.jsonpb.Unmarshal(data, v)
}

// NewDecoder returns a Decoder which reads byte sequence from "r".
func (m *paralusJSON) NewDecoder(r io.Reader) runtime.Decoder {
	return m.jsonpb.NewDecoder(r)
}

// NewEncoder returns an Encoder which writes bytes sequence into "w".
func (m *paralusJSON) NewEncoder(w io.Writer) runtime.Encoder {
	return m.jsonpb.NewEncoder(w)
}

// Delimiter for newline encoded JSON streams.
func (m *paralusJSON) Delimiter() []byte {
	return []byte("\n")
}
