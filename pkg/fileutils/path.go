// Package fileutils provides file-system helpers that go beyond os and
// path/filepath | 超出 os 与 path/filepath 能力的文件系统辅助函数.
//
// # Notes | 说明
//
// A function exists here only when it adds behavior to a standard-library
// call: creating missing parent directories, predicates the standard library
// lacks, tree copying, globstar matching, scanning, caching, size formatting.
// Plain aliases such as Open, Stat, Chmod, Rename or Join are deliberately
// absent; use os and path/filepath directly. Errors are returned as the
// standard library produced them. No global state, no background goroutines,
// no dependencies outside the standard library.
// 只有在标准库调用之上增加了行为的函数才会出现在这里：创建缺失父目录、
// 标准库没有的谓词、目录树复制、globstar 匹配、扫描、缓存、大小格式化。
// 纯别名（Open、Stat、Chmod、Rename、Join）刻意不提供，直接用 os 与
// path/filepath。错误按标准库原样返回。无全局状态、无后台 goroutine、仅
// 依赖标准库。
//
// # Features | 功能
//
// - Predicates: Exists, IsDir, IsFile, IsEmpty, IsReadable, IsWritable | 路径谓词一族
// - Create/WriteFile/AppendFile create missing parents | 写入前自动创建父目录
// - Lines iterates a file without loading it whole | 逐行流式迭代大文件
// - Copy copies files or directory trees, skipping symlinks | 复制文件/目录树，跳过符号链接
// - Scan with patterns, recursion and filter; MatchGlob adds "**" | 目录扫描与 globstar 匹配
// - ReplaceFile/ReplaceDir write back only on change | 内容变化才写回
// - Cache re-reads only when size or mtime changed; Search across dirs | 变化才重读；跨目录查找
// - ParseSize/FormatSize for human-readable sizes | 大小的解析与格式化
//
// # Example | 示例
//
//	_ = fileutils.WriteFile("conf/app.yaml", "key: value") // creates conf/
//	goFiles, err := fileutils.Scan(".",
//		fileutils.ScanOptions{Patterns: []string{"*.go"}, Recursive: true})
package fileutils

import (
	"os"
	"path/filepath"
	"strings"
)

// Exists reports whether path exists. Symbolic links are followed.
// Exists 判断路径是否存在，符号链接会被跟随。
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDir reports whether path exists and is a directory.
// IsDir 判断路径是否存在且为目录。
func IsDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// IsFile reports whether path exists and is not a directory.
// IsFile 判断路径是否存在且不是目录。
func IsFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// IsEmpty reports whether path is an empty directory or a zero-size file.
// It also returns true if path does not exist or cannot be inspected.
// IsEmpty 判断路径是空目录或零大小文件；
// 路径不存在或无法检查时同样返回 true。
func IsEmpty(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return true
	}
	if !fi.IsDir() {
		return fi.Size() == 0
	}
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer func() { _ = f.Close() }()
	// io.EOF when the directory has no entries
	// 目录没有条目时返回 io.EOF
	_, err = f.Readdirnames(1)
	return err != nil
}

// IsReadable reports whether path can be opened for reading.
// IsReadable 判断路径能否以读方式打开。
func IsReadable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// IsWritable reports whether path can be written. For a directory it checks
// that a file can be created inside it; for a file, that it can be opened for
// writing (the file is not truncated). A missing path is not writable.
// IsWritable 判断路径能否写入。目录检查能否在其中创建文件；文件检查能否以写
// 方式打开（不会截断文件）。路径不存在则不可写。
func IsWritable(path string) bool {
	if IsDir(path) {
		f, err := os.CreateTemp(path, ".writable-*")
		if err != nil {
			return false
		}
		name := f.Name()
		_ = f.Close()
		_ = os.Remove(name)
		return true
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// RealPath returns the absolute form of path, or "" if it does not exist.
// RealPath 返回路径的绝对形式，不存在时返回空串。
func RealPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil || !Exists(abs) {
		return ""
	}
	return abs
}

// Name returns the last element of path without its extension.
// Name 返回路径最后一段的文件名，不含扩展名。
//
//	Name("/var/www/file.js") // "file"
func Name(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// ExtName is like filepath.Ext but without the leading dot.
// ExtName 类似 filepath.Ext，但不含前导点。
//
//	ExtName("main.go") // "go"
func ExtName(path string) string {
	return strings.TrimPrefix(filepath.Ext(path), ".")
}
