package sonic

import (
	"errors"
	"strings"
	"testing"
)

type item struct{ S string }

func TestRoundTrip(t *testing.T) {
	var c Codec
	b, err := c.Marshal(item{S: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var got item
	if err := c.Unmarshal(b, &got); err != nil || got.S != "x" {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
}

// 超大载荷在解码前就被拒绝，不会进入解析器。
func TestUnmarshalRejectsOversizedPayload(t *testing.T) {
	c := Codec{MaxBytes: 64}
	big := []byte(`{"S":"` + strings.Repeat("a", 200) + `"}`)
	var got item
	if err := c.Unmarshal(big, &got); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if _, err := c.Marshal(item{S: strings.Repeat("a", 200)}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Marshal err = %v, want ErrTooLarge", err)
	}
}

func TestNegativeLimitIsRejected(t *testing.T) {
	if _, err := (Codec{MaxBytes: -1}).Marshal(item{}); err == nil {
		t.Fatal("negative MaxBytes was accepted")
	}
}
