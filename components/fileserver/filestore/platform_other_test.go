//go:build !unix

package filestore

import "errors"

func mkfifo(string) error {
	return errors.New("FIFOs are not supported on this platform")
}

// supportsPermissionBits 报告平台是否有实际的 Unix 文件模式位。
func supportsPermissionBits() bool {
	return false
}
