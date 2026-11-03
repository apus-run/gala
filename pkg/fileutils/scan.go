package fileutils

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ScanOptions controls Scan. The zero value lists every direct child.
// ScanOptions 控制 Scan 的行为。零值列出所有直接子项。
type ScanOptions struct {
	// Patterns are base-name globs in filepath.Match syntax, for example
	// "*.go". An entry is kept if it matches any of them; empty keeps all.
	// Patterns 是按 filepath.Match 语法写的基名 glob，例如 "*.go"。
	// 条目命中任意一个即保留；为空表示全部保留。
	Patterns []string

	// Recursive descends into sub-directories. Symbolic links are never
	// followed, so a link to a directory is not descended into.
	// Recursive 递归进入子目录。符号链接永不跟随，
	// 指向目录的链接也不会被深入。
	Recursive bool

	// FilesOnly leaves directories out of the result (they are still
	// descended into when Recursive is set).
	// FilesOnly 让目录不出现在结果里（Recursive 打开时仍会深入目录）。
	FilesOnly bool

	// Filter, if set, is consulted last; return false to drop the entry.
	// It subsumes any custom matching, e.g. MatchGlob against a relative path.
	// Filter 若设置则最后询问；返回 false 丢弃该条目。
	// 它可以涵盖任何自定义匹配，例如对相对路径用 MatchGlob。
	Filter func(path string, d fs.DirEntry) bool
}

func (o *ScanOptions) accept(path string, d fs.DirEntry) bool {
	if o.FilesOnly && d.IsDir() {
		return false
	}
	if len(o.Patterns) > 0 && !slices.ContainsFunc(o.Patterns, func(p string) bool {
		ok, _ := filepath.Match(p, d.Name()) // patterns are validated by Scan | 模式已由 Scan 校验
		return ok
	}) {
		return false
	}
	return o.Filter == nil || o.Filter(path, d)
}

// Scan lists the entries under root as absolute paths in lexical order. root
// itself is not included. The first error encountered, such as an unreadable
// directory or a malformed pattern, aborts the scan.
// Scan 按字典序列出 root 下的条目（绝对路径），root 本身不在结果里。
// 遇到的第一个错误——如目录不可读或模式非法——会中止扫描。
func Scan(root string, opt ...ScanOptions) ([]string, error) {
	var o ScanOptions
	if len(opt) > 0 {
		o = opt[0]
	}
	for _, p := range o.Patterns {
		if _, err := filepath.Match(p, ""); err != nil {
			return nil, err
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	var list []string
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if o.accept(p, e) {
				list = append(list, p)
			}
			if o.Recursive && e.IsDir() {
				if err := walk(p); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	slices.Sort(list)
	return list, nil
}

// SortFiles sorts paths in place: directories first, then files, each group in
// lexical order. Every path is stat-ed once.
// SortFiles 原地排序路径：目录在前、文件在后，各自组内按字典序。
// 每个路径做一次 stat。
func SortFiles(paths []string) {
	isDir := make(map[string]bool, len(paths))
	for _, p := range paths {
		isDir[p] = IsDir(p)
	}
	slices.SortStableFunc(paths, func(a, b string) int {
		if da, db := isDir[a], isDir[b]; da != db {
			if da {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	})
}
