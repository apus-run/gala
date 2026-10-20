//go:build windows

package filestore

import "os"

// syncDir 在 Windows 上为空操作：NTFS 不提供目录同步，SyncFileAndDir 退化为 SyncFile。
func syncDir(_ *os.Root, _ string) error {
	return nil
}
