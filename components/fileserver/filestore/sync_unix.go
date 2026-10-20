//go:build !windows

package filestore

import "os"

// syncDir 同步目录项，使刚发布或删除的名字在掉电后仍然存在。
func syncDir(root *os.Root, dir string) error {
	d, err := root.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}
