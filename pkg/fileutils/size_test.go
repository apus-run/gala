package fileutils

import (
	"math"
	"testing"
)

func TestFormatSize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0.00B"},
		{1023, "1023.00B"},
		{1024, "1.00K"},
		{1536, "1.50K"},
		{1 << 20, "1.00M"},
		{5 << 30, "5.00G"},
		{1 << 40, "1.00T"},
		{1 << 50, "1.00P"},
		{math.MaxInt64, "8.00E"},
		{-5, "-5.00B"},
	}
	for _, c := range cases {
		if got := FormatSize(c.in); got != c.want {
			t.Errorf("FormatSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseSize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"10", 10},
		{"10b", 10},
		{"1K", 1024},
		{"1.5K", 1536},
		{"1.5 kb", 1536},
		{"64 MB", 64 << 20},
		{"2GiB", 2 << 30},
		{"1kilobyte", 1024},
		{"  3 t ", 3 << 40},
		{"1P", 1 << 50},
		{"7E", 7 << 60},
		{".5K", 512},
	}
	for _, c := range cases {
		got, err := ParseSize(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseSize(%q) = (%d, %v), want %d", c.in, got, err, c.want)
		}
	}

	for _, in := range []string{"", "abc", "-1", "5x", "1.2.3K", "K", "8E", "9E"} {
		if v, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) = %d, want an error", in, v)
		}
	}
}

func TestSizeRoundTrip(t *testing.T) {
	t.Parallel()
	for _, n := range []int64{0, 512, 1024, 3 << 20, 7 << 30} {
		got, err := ParseSize(FormatSize(n))
		if err != nil || got != n {
			t.Errorf("round trip of %d = (%d, %v)", n, got, err)
		}
	}
}
