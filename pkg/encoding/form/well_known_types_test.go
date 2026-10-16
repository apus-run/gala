package form

import (
	"bytes"
	"encoding/base64"
	"testing"
	"time"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestMarshalTimeStamp(t *testing.T) {
	tests := []struct {
		input  *timestamppb.Timestamp
		expect string
	}{
		{
			input:  timestamppb.New(time.Date(2022, 1, 2, 3, 4, 5, 6, time.UTC)),
			expect: "2022-01-02T03:04:05.000000006Z",
		},
		{
			input:  timestamppb.New(time.Date(2022, 13, 1, 13, 61, 61, 100, time.UTC)),
			expect: "2023-01-01T14:02:01.000000100Z",
		},
		{
			input:  timestamppb.New(time.Date(2022, 1, 2, 3, 4, 5, 6, time.FixedZone("UTC+8", 8*60*60))),
			expect: "2022-01-01T19:04:05.000000006Z",
		},
	}
	for _, v := range tests {
		got, err := marshalTimestamp(v.input.ProtoReflect())
		if err != nil {
			t.Fatal(err)
		}
		if want := v.expect; got != want {
			t.Errorf("expect %v, got %v", want, got)
		}
	}
}

func TestParseMessageNullReturnsInvalidValue(t *testing.T) {
	value, err := parseMessage((&timestamppb.Timestamp{}).ProtoReflect().Descriptor(), nullStr)
	if err != nil {
		t.Fatal(err)
	}
	if value.IsValid() {
		t.Fatalf("parseMessage(null).IsValid() = true, want false")
	}
}

func TestCodecUnmarshalRejectsNilTarget(t *testing.T) {
	c := codec{encoder: encoder, decoder: decoder}

	if err := c.Unmarshal([]byte("name=gala"), nil); err == nil {
		t.Fatal("Unmarshal(nil) error = nil, want error")
	}

	var dst *struct {
		Name string `json:"name"`
	}
	if err := c.Unmarshal([]byte("name=gala"), dst); err == nil {
		t.Fatal("Unmarshal(typed nil pointer) error = nil, want error")
	}
}

func TestCodecBytesRoundTripUsesURLBase64(t *testing.T) {
	c := codec{encoder: encoder, decoder: decoder}
	input := &anypb.Any{
		Value: []byte{0xfb, 0xff},
	}

	data, err := c.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}

	var output anypb.Any
	if err := c.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.Value, input.Value; !bytes.Equal(got, want) {
		t.Fatalf("round trip bytes = %v, want %v", got, want)
	}
}

func TestMarshalDuration(t *testing.T) {
	tests := []struct {
		input  *durationpb.Duration
		expect string
	}{
		{
			input:  durationpb.New(time.Duration(1<<63 - 1)),
			expect: "2562047h47m16.854775807s",
		},
		{
			input:  durationpb.New(time.Duration(-1 << 63)),
			expect: "-2562047h47m16.854775808s",
		},
		{
			input:  durationpb.New(100 * time.Second),
			expect: "1m40s",
		},
		{
			input:  durationpb.New(-100 * time.Second),
			expect: "-1m40s",
		},
	}
	for _, v := range tests {
		got, err := marshalDuration(v.input.ProtoReflect())
		if err != nil {
			t.Fatal(err)
		}
		if want := v.expect; got != want {
			t.Errorf("expect %s, got %s", want, got)
		}
	}
}

func TestMarshalBytes(t *testing.T) {
	tests := []struct {
		input  protoreflect.Message
		expect string
	}{
		{
			input:  wrapperspb.Bytes([]byte("abc123!?$*&()'-=@~")).ProtoReflect(),
			expect: base64.StdEncoding.EncodeToString([]byte("abc123!?$*&()'-=@~")),
		},
		{
			input:  wrapperspb.Bytes([]byte("kratos")).ProtoReflect(),
			expect: base64.StdEncoding.EncodeToString([]byte("kratos")),
		},
	}
	for _, v := range tests {
		got, err := marshalBytes(v.input)
		if err != nil {
			t.Fatal(err)
		}
		if want := v.expect; got != want {
			t.Errorf("expect %v, got %v", want, got)
		}
	}
}
