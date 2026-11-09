package toml_test

import (
	"github.com/apus-run/gala/pkg/encoding"
	_ "github.com/apus-run/gala/pkg/encoding/toml"
	"testing"
)

func TestCodecRoundTrip(t *testing.T) {
	codec := encoding.GetCodec("toml")
	if codec == nil {
		t.Fatal("codec not registered")
	}
	value := map[string]any{"port": int64(8080)}
	raw, err := codec.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := codec.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.(map[string]any)["port"] != int64(8080) {
		t.Fatalf("round trip: %#v", decoded)
	}
	if err := codec.Unmarshal([]byte("port = ["), &decoded); err == nil {
		t.Fatal("invalid TOML accepted")
	}
}
