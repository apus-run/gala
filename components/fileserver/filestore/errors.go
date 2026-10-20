package filestore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"syscall"

	"github.com/apus-run/gala/components/fileserver"
)

// ErrClosed 表示 Store 已经关闭。
var ErrClosed = os.ErrClosed

func opErr(op, key string, err error) error {
	return &fileserver.Error{Op: op, Key: key, Err: err}
}

// classify 把操作系统错误映射为哨兵类别，并保留原始原因。其余错误原样返回。
func classify(err error) error {
	switch {
	case err == nil:
		return nil
	case isNotExist(err):
		return fmt.Errorf("%w: %w", fileserver.ErrNotFound, err)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%w: %w", fileserver.ErrPermission, err)
	case isEscape(err):
		return fmt.Errorf("%w: %w", fileserver.ErrPermission, err)
	}
	return err
}

// isEscape 报告 os.Root 拒绝的越界访问：Key 指向根之外的符号链接，或其父目录是。
//
// Go 1.27 里这个哨兵仍只在 os 的 export_test.go 中叫做 ErrPathEscapes，公共 API 没有它，
// 因此按错误文本的结尾识别。安全语义与 ErrPermission 一致：后端拒绝执行。
func isEscape(err error) bool {
	return strings.HasSuffix(err.Error(), "path escapes from parent")
}

// isNotExist 报告路径不存在。路径中间的组件是普通文件时同样视为不存在。
func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || isNotDir(err)
}

func isNotDir(err error) bool {
	return errors.Is(err, syscall.ENOTDIR)
}

// isLinkUnsupported 报告文件系统是否不支持硬链接。
// EPERM 不在这里：它也可能是安全策略拒绝，探测结果单独记为 linkDenied。
func isLinkUnsupported(err error) bool {
	return errors.Is(err, errors.ErrUnsupported) ||
		errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EOPNOTSUPP)
}

// isLinkDenied 报告硬链接被权限或安全策略拒绝。
func isLinkDenied(err error) bool {
	return errors.Is(err, syscall.EPERM)
}

func notFound(msg string) error {
	return fmt.Errorf("%w: %s", fileserver.ErrNotFound, msg)
}

func conflict(msg string) error {
	return fmt.Errorf("%w: %s", fileserver.ErrConflict, msg)
}

func invalidArgument(msg string) error {
	return fmt.Errorf("%w: %s", fileserver.ErrInvalidArgument, msg)
}
