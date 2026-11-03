package fileutils

import "os"

// ReplaceFile rewrites the file at path with fn(path, content). The file is
// only written when the content actually changes, so unchanged files keep
// their modification time.
// ReplaceFile 用 fn(path, content) 重写 path 处的文件；内容真正变化才写入，
// 因此未变化的文件保持其修改时间。
//
// A plain text substitution is a one-liner | 纯文本替换一行即可:
//
//	ReplaceFile(p, func(_, s string) string { return strings.ReplaceAll(s, "old", "new") })
func ReplaceFile(path string, fn func(path, content string) string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	old := string(data)
	if updated := fn(path, old); updated != old {
		return WriteFile(path, updated)
	}
	return nil
}

// ReplaceDir applies ReplaceFile to every file selected by Scan(root, opt...).
// Directories are never passed to fn.
// ReplaceDir 对 Scan(root, opt...) 选中的每个文件应用 ReplaceFile；
// 目录不会传给 fn。
func ReplaceDir(root string, fn func(path, content string) string, opt ...ScanOptions) error {
	var o ScanOptions
	if len(opt) > 0 {
		o = opt[0]
	}
	o.FilesOnly = true
	files, err := Scan(root, o)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := ReplaceFile(f, fn); err != nil {
			return err
		}
	}
	return nil
}
