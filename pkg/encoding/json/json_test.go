package json

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/apus-run/gala/pkg/encoding"
)

func TestCodecRegistration(t *testing.T) {
	registered := encoding.GetCodec("json")
	if registered == nil {
		t.Fatal("JSON codec is not registered")
	}
	if _, ok := registered.(codec); !ok || registered.Name() != Name {
		t.Fatalf("unexpected registered codec: %#v", registered)
	}
}

func TestCodecMarshal(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value any
		want  string
	}{
		{"nil", nil, "null"},
		{"nil slice", []string(nil), "[]"},
		{"nil map", map[string]string(nil), "{}"},
		{"HTML", "<a>&", `"<a>&"`},
		{"object", struct {
			Name string `json:"name"`
		}{Name: "gala"}, `{"name":"gala"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (codec{}).Marshal(tt.value)
			if err != nil || string(got) != tt.want {
				t.Fatalf("Marshal() = %q, %v; want %q, nil", got, err, tt.want)
			}
		})
	}
	if _, err := (codec{}).Marshal(make(chan int)); err == nil {
		t.Fatal("Marshal accepted an unsupported type")
	}
}

func TestCodecRoundTrip(t *testing.T) {
	type message struct {
		Name   string            `json:"name"`
		Values []int             `json:"values"`
		Labels map[string]string `json:"labels"`
	}
	want := message{Name: "gala", Values: []int{1, 2}, Labels: map[string]string{"env": "test"}}
	data, err := (codec{}).Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got message
	if err := (codec{}).Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v; want %#v", got, want)
	}
}

func TestCodecUnmarshalEmpty(t *testing.T) {
	for _, data := range [][]byte{nil, {}} {
		value := map[string]int{"keep": 1}
		if err := (codec{}).Unmarshal(data, &value); err != nil || value["keep"] != 1 {
			t.Fatalf("empty input changed the destination: %v, %v", value, err)
		}
		custom := &customJSON{err: errors.New("must not be called")}
		if err := (codec{}).Unmarshal(data, custom); err != nil || custom.called {
			t.Fatalf("empty input called UnmarshalJSON: %v, %v", custom.called, err)
		}
		if err := (codec{}).Unmarshal(data, nil); err != nil {
			t.Fatalf("empty input with nil destination: %v", err)
		}
	}
}

func TestCodecUnmarshalInvalid(t *testing.T) {
	for _, data := range []string{" ", "{", `{} {}`, `{"name":1,"name":2}`} {
		var value any
		if err := (codec{}).Unmarshal([]byte(data), &value); err == nil {
			t.Errorf("Unmarshal accepted %q", data)
		}
	}
	if err := (codec{}).Unmarshal([]byte(`{}`), nil); err == nil {
		t.Fatal("Unmarshal accepted a nil destination")
	}
}

type customJSON struct {
	data   []byte
	err    error
	called bool
}

func (v *customJSON) MarshalJSON() ([]byte, error) {
	v.called = true
	return v.data, v.err
}

func (v *customJSON) UnmarshalJSON(data []byte) error {
	v.called = true
	v.data = data
	return v.err
}

func TestCodecCustomJSON(t *testing.T) {
	wantErr := errors.New("custom JSON error")
	for _, err := range []error{nil, wantErr} {
		// Custom methods receive and return raw bytes without validation,
		// whitespace normalization, or error wrapping by the codec.
		data := []byte(" invalid JSON \n")
		marshaler := &customJSON{data: data, err: err}
		got, gotErr := (codec{}).Marshal(marshaler)
		if !marshaler.called || !bytes.Equal(got, data) || gotErr != err {
			t.Fatalf("MarshalJSON was not passed through: %q, %v", got, gotErr)
		}
		unmarshaler := &customJSON{err: err}
		gotErr = (codec{}).Unmarshal(data, unmarshaler)
		if !unmarshaler.called || !bytes.Equal(unmarshaler.data, data) || gotErr != err {
			t.Fatalf("UnmarshalJSON was not passed through: %q, %v", unmarshaler.data, gotErr)
		}
	}
}
