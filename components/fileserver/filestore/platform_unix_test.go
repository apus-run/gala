//go:build unix

package filestore

import (
	"runtime"
	"syscall"
)

func mkfifo(path string) error {
	return syscall.Mkfifo(path, 0o600)
}

// supportsPermissionBits 报告 Unix 文件模式位是否有实际含义。
func supportsPermissionBits() bool {
	return runtime.GOOS != "windows"
}
