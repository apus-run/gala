package api

import "testing"

func TestParseRange(t *testing.T) {
	tests := []struct {
		header string
		size   int64
		start  int64
		length int64
		ok     bool
	}{
		{"bytes=0-9", 100, 0, 10, true},
		{"bytes=100-", 100, 100, -1, true},
		{"bytes=101-", 100, 101, -1, true},
		{"bytes=-5", 100, 95, 5, true},
		{"bytes=-500", 100, 0, 100, true},
		{"bytes=10-5", 100, 0, 0, false},
		{"bytes=0-1,3-4", 100, 0, 0, false},
		{"bytes=abc", 100, 0, 0, false},
		{"bytes=-0", 100, 0, 0, false},
		{"bytes=0-0", 0, 0, 1, true},
		{"bytes=0-", 0, 0, -1, true},
	}
	for _, tt := range tests {
		start, length, ok := parseRange(tt.header, tt.size)
		if ok != tt.ok || start != tt.start || length != tt.length {
			t.Errorf("parseRange(%q, %d) = (%d, %d, %v), want (%d, %d, %v)",
				tt.header, tt.size, start, length, ok, tt.start, tt.length, tt.ok)
		}
	}
}
