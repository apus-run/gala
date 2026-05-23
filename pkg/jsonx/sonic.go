package json

import (
	"io"

	"github.com/bytedance/sonic"
)

var stdConfig = sonic.ConfigStd

func Marshal(v any) ([]byte, error) {
	return stdConfig.Marshal(v)
}

func MarshalString(v any) (string, error) {
	return stdConfig.MarshalToString(v)
}

func MarshalStringIgnoreErr(v any) string {
	res, _ := stdConfig.MarshalToString(v)
	return res
}

func MarshalIndent(v any) ([]byte, error) {
	return stdConfig.MarshalIndent(v, "", "\t")
}

func Unmarshal(data []byte, v any) error {
	return stdConfig.Unmarshal(data, v)
}

func Decode(reader io.Reader, v any) error {
	return stdConfig.NewDecoder(reader).Decode(v)
}

func Valid(data []byte) bool {
	return stdConfig.Valid(data)
}

func Jsonify(data any) string {
	dump, _ := sonic.MarshalString(data)
	return dump
}
