package document

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	for _, tc := range []struct {
		input, reason string
		calls         int
	}{
		{"", "empty document", 0}, {" \n", "empty document", 0}, {"[]", "root is not an object", 1}, {"null", "root is not an object", 1}, {"1", "root is not an object", 1}, {"{", "decode failed", 1}, {"{}", "", 1},
	} {
		t.Run(tc.input, func(t *testing.T) {
			calls := 0
			m, err := Decode([]byte(tc.input), func(raw []byte, v any) error {
				calls++
				if err := json.Unmarshal(raw, v); err != nil {
					return errors.New("SECRET")
				}
				return nil
			})
			if calls != tc.calls {
				t.Fatalf("decoder called %d times", calls)
			}
			if tc.reason == "" {
				if err != nil || m == nil {
					t.Fatal(m, err)
				}
				return
			}
			if err == nil || err.Error() != tc.reason || strings.Contains(err.Error(), "SECRET") {
				t.Fatal(err)
			}
		})
	}
}
