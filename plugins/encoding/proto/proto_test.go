package proto

import (
	"testing"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestCodecMarshalRejectsNonProtoMessage(t *testing.T) {
	_, err := codec{}.Marshal("not proto")
	if err == nil {
		t.Fatal("Marshal() error = nil, want error")
	}
}

func TestCodecMarshalRejectsNilProtoMessage(t *testing.T) {
	var msg *wrapperspb.StringValue

	_, err := codec{}.Marshal(msg)
	if err == nil {
		t.Fatal("Marshal() error = nil, want error")
	}
}

func TestCodecUnmarshalRejectsNilProtoMessage(t *testing.T) {
	var msg *wrapperspb.StringValue

	err := codec{}.Unmarshal(nil, msg)
	if err == nil {
		t.Fatal("Unmarshal() error = nil, want error")
	}
}
