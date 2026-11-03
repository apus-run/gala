package fileutils

import (
	"bufio"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"
)

const (
	dirPerm  = 0o755
	filePerm = 0o644
)

// Content is the set of types accepted as file content | Content 是可作为文件内容的类型集合.
type Content interface {
	~string | ~[]byte
}

// Create creates or truncates the file at path, creating any missing parent
// directories first.
// Create 创建或截断 path 处的文件，缺失的父目录会先被创建。
func Create(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, filePerm)
}

// WriteFile writes data to path, replacing any existing content. Missing parent
// directories are created. The mode of an existing file is left untouched.
// WriteFile 把 data 写入 path 并替换已有内容，缺失的父目录会被创建；
// 已存在文件的权限保持不变。
func WriteFile[T Content](path string, data T) error {
	return write(path, data, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
}

// AppendFile appends data to path, creating the file and any missing parent
// directories if necessary.
// AppendFile 把 data 追加到 path，必要时创建文件与缺失的父目录。
func AppendFile[T Content](path string, data T) error {
	return write(path, data, os.O_WRONLY|os.O_CREATE|os.O_APPEND)
}

func write[T Content](path string, data T, flag int) error {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return err
	}
	f, err := os.OpenFile(path, flag, filePerm)
	if err != nil {
		return err
	}
	// data may be a named string type, so it cannot go to WriteString directly;
	// for []byte this conversion is free, which is the bulk-data case.
	// data 可能是命名 string 类型，不能直接交给 WriteString；
	// 对 []byte 来说这个转换是零开销的，而它正是大数据量的场景。
	_, err = f.Write([]byte(data)) //nolint:gocritic // see comment above
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Lines iterates over the lines of the file at path without loading it all
// into memory. Line terminators ("\n" or "\r\n") are stripped, and a final line
// without a terminator is still yielded. Lines may be arbitrarily long.
// Lines 逐行迭代 path 处的文件，不会整体载入内存。行尾符（"\n" 或 "\r\n"）
// 会被去掉，最后一行没有行尾符时也会产出。行可以任意长。
//
// The error, if any, is yielded once, as the last element, after which the
// iteration ends. Breaking out of the loop early closes the file.
// 若有错误，会作为最后一个元素恰好产出一次，随后迭代结束；
// 提前跳出循环会关闭文件。
//
//	for line, err := range fileutils.Lines(path) {
//		if err != nil {
//			return err
//		}
//		...
//	}
func Lines(path string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		f, err := os.Open(path)
		if err != nil {
			yield("", err)
			return
		}
		defer func() { _ = f.Close() }()

		r := bufio.NewReader(f)
		for {
			line, err := r.ReadString('\n')
			if line != "" {
				line = strings.TrimSuffix(line, "\n")
				line = strings.TrimSuffix(line, "\r")
				if !yield(line, nil) {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					yield("", err)
				}
				return
			}
		}
	}
}
