package fileutils

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMatchGlob(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern, name string
		want          bool
	}{
		// no globstar: identical to filepath.Match
		{"*.go", "main.go", true},
		{"*.go", "dir/main.go", false},
		{"[abc]", "a", true},
		{"[^abc]", "a", false},
		{"?", "x", true},

		// globstar as a whole segment
		{"**", "any/path/file.go", true},
		{"**", "file.go", true},
		{"src/**/*.go", "src/foo/bar/main.go", true},
		{"src/**/*.go", "src/main.go", true},
		{"src/**/*.go", "src/main.txt", false},
		{"src/**/*.go", "lib/main.go", false},
		{"a/**", "a", true},
		{"a/**", "a/b/c", true},
		{"a/**", "b/c", false},
		{"**/a", "a", true},
		{"**/a", "x/y/a", true},
		{"**/a", "x/y/b", false},
		{"a/**/b", "a/b", true},
		{"a/**/b", "a/x/y/b", true},
		{"a/**/b", "a/x/y", false},
		{"a/**/b/**/c", "a/b/c", true},
		{"a/**/b/**/c", "a/x/b/y/c", true},
		{"a/**/b/**/c", "a/x/y/c", false},
		{"**/*.go", "main.go", true},
		{"/abs/**/f", "/abs/x/f", true},

		// "**" inside a segment is just two "*"
		{"a**b", "axxb", true},
		{"a**b", "a/b", false},
		{"**a", "xa", true},
		{"**a", "x/a", false},
	}
	for _, c := range cases {
		got, err := MatchGlob(c.pattern, c.name)
		if err != nil {
			t.Errorf("MatchGlob(%q, %q) unexpected error: %v", c.pattern, c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

func TestMatchGlobBadPattern(t *testing.T) {
	t.Parallel()
	for _, p := range []string{"[", "[abc", "a/[/b", "**/[", "[/**"} {
		if _, err := MatchGlob(p, "a/b"); !errors.Is(err, filepath.ErrBadPattern) {
			t.Errorf("MatchGlob(%q) error = %v, want ErrBadPattern", p, err)
		}
	}
}

// Naive recursive globstar matchers are exponential here; this must stay fast.
func TestMatchGlobIsLinearish(t *testing.T) {
	t.Parallel()
	name := strings.Repeat("a/", 60) + "c"
	pattern := "**/a/**/a/**/a/**/a/**/a/**/b"
	start := time.Now()
	got, err := MatchGlob(pattern, name)
	if err != nil || got {
		t.Fatalf("got (%v, %v), want (false, nil)", got, err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("took %v, matching is not polynomial", d)
	}
}
