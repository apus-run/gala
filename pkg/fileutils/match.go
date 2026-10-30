package fileutils

import (
	"path"
	"path/filepath"
	"strings"
)

// MatchGlob reports whether name matches the shell pattern. It extends
// filepath.Match with "**" (globstar), as in bash and .gitignore.
//
// "**" matches zero or more whole path segments, and only when it is a
// complete segment ("a/**/b", "**/a", "a/**"). Elsewhere ("a**b") it is just
// two ordinary "*" and stays within one segment. Without any "**" the call is
// exactly filepath.Match. A malformed pattern returns filepath.ErrBadPattern.
//
// Examples:
//
//	MatchGlob("src/**/*.go", "src/a/b/main.go") // true
//	MatchGlob("src/**/*.go", "src/main.go")     // true, "**" matches nothing
//	MatchGlob("a**b", "a/b")                    // false
func MatchGlob(pattern, name string) (bool, error) {
	if !strings.Contains(pattern, globstar) {
		return filepath.Match(pattern, name)
	}
	pat, nam := segments(pattern), segments(name)
	for _, seg := range pat { // report a malformed pattern even if it is never reached
		if seg == globstar {
			continue
		}
		if _, err := path.Match(seg, ""); err != nil {
			// path.Match's only possible error is its own ErrBadPattern; report
			// the filepath one so callers see the same error on both code paths.
			return false, filepath.ErrBadPattern
		}
	}

	// Linear-time wildcard matching with backtracking to the most recent "**".
	p, n, star, mark := 0, 0, -1, 0
	for n < len(nam) {
		if p < len(pat) {
			if pat[p] == globstar {
				star, mark = p, n
				p++
				continue
			}
			if ok, _ := path.Match(pat[p], nam[n]); ok { // pattern already validated
				p++
				n++
				continue
			}
		}
		if star < 0 {
			return false, nil
		}
		mark++
		n, p = mark, star+1
	}
	for p < len(pat) && pat[p] == globstar {
		p++
	}
	return p == len(pat), nil
}

const globstar = "**"

// segments splits a path into slash-separated segments, accepting the
// platform separator and cleaning the path first.
func segments(s string) []string {
	return strings.Split(path.Clean(filepath.ToSlash(s)), "/")
}
