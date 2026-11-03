package fileutils

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// SelfPath returns the absolute path of the running executable, or "" if it
// cannot be determined.
// SelfPath 返回当前可执行文件的绝对路径，无法确定时返回空串。
func SelfPath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	return p
}

// Search looks for the relative file name in dirs, in the given order, then in
// the working directory, then in the directory of the executable. It returns
// the absolute path of the first match.
// Search 按给定顺序在 dirs 中查找相对文件名，然后是工作目录、可执行文件
// 所在目录，返回第一个命中的绝对路径。
//
// An absolute name is only checked for existence. When nothing is found the
// error lists every directory tried and wraps fs.ErrNotExist.
// 绝对路径只检查是否存在。找不到时错误列出所有尝试过的目录，
// 并包装 fs.ErrNotExist。
func Search(name string, dirs ...string) (string, error) {
	if filepath.IsAbs(name) {
		if Exists(name) {
			return filepath.Clean(name), nil
		}
		return "", fmt.Errorf("fileutils: %q: %w", name, fs.ErrNotExist)
	}

	candidates := slices.Clone(dirs)
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if self := SelfPath(); self != "" {
		candidates = append(candidates, filepath.Dir(self))
	}

	tried := make([]string, 0, len(candidates))
	for _, dir := range candidates {
		abs, err := filepath.Abs(dir)
		if err != nil || slices.Contains(tried, abs) {
			continue
		}
		if p := filepath.Join(abs, name); Exists(p) {
			return p, nil
		}
		tried = append(tried, abs)
	}
	return "", fmt.Errorf("fileutils: cannot find %q in [%s]: %w", name, strings.Join(tried, ", "), fs.ErrNotExist)
}
