package fileutils

import (
	"path"
	"path/filepath"
	"strings"
)

// MatchGlob reports whether name matches the shell pattern. It extends
// filepath.Match with "**" (globstar), as in bash and .gitignore.
// MatchGlob 判断 name 是否匹配 shell 模式，在 filepath.Match 之上扩展了
// "**"（globstar），与 bash 和 .gitignore 一致。
//
// "**" matches zero or more whole path segments, and only when it is a
// complete segment ("a/**/b", "**/a", "a/**"). Elsewhere ("a**b") it is just
// two ordinary "*" and stays within one segment. Without any "**" the call is
// exactly filepath.Match. A malformed pattern returns filepath.ErrBadPattern.
// "**" 匹配零个或多个完整路径段，且只有当它是完整的一段时才生效
// （"a/**/b"、"**/a"、"a/**"）。其他位置（"a**b"）只是两个普通的 "*"，
// 不跨段。模式里没有 "**" 时调用等价于 filepath.Match。
// 模式非法时返回 filepath.ErrBadPattern。
//
// Examples | 示例:
//
//	MatchGlob("src/**/*.go", "src/a/b/main.go") // true
//	MatchGlob("src/**/*.go", "src/main.go")     // true, "**" matches nothing
//	MatchGlob("a**b", "a/b")                    // false
func MatchGlob(pattern, name string) (bool, error) {
	if !strings.Contains(pattern, globstar) {
		return filepath.Match(pattern, name)
	}
	pat, nam := segments(pattern), segments(name)
	// report a malformed pattern even if it is never reached
	// 即使匹配永远走不到这一段，也要报告非法模式
	for _, seg := range pat {
		if seg == globstar {
			continue
		}
		if _, err := path.Match(seg, ""); err != nil {
			// path.Match's only possible error is its own ErrBadPattern; report
			// the filepath one so callers see the same error on both code paths.
			// path.Match 唯一可能的错误是它自己的 ErrBadPattern；这里上报
			// filepath 的那个，让两条代码路径给调用方一致的错误。
			return false, filepath.ErrBadPattern
		}
	}

	// Linear-time wildcard matching with backtracking to the most recent "**".
	// 线性时间的通配符匹配，回溯到最近的一个 "**"。
	p, n, star, mark := 0, 0, -1, 0
	for n < len(nam) {
		if p < len(pat) {
			if pat[p] == globstar {
				star, mark = p, n
				p++
				continue
			}
			if ok, _ := path.Match(pat[p], nam[n]); ok { // pattern already validated | 模式已提前校验
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
// segments 把路径按斜杠切段，接受平台分隔符，并先做路径清理。
func segments(s string) []string {
	return strings.Split(path.Clean(filepath.ToSlash(s)), "/")
}
