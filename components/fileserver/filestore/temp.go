package filestore

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"time"
)

// CleanupTemp 删除临时区中修改时间早于 olderThan 的残留文件：进程在发布前退出时留下的半成品。
// olderThan 必须为正，否则返回 ErrInvalidArgument；负值会把 cutoff 推到未来并删除活跃文件。
//
// 库不启动后台服务，由调用方按自己的节奏调用（cron 或启动时）。建议 olderThan 不小于 24 小时。
// 它只触碰 .fs-tmp，不触碰数据区。
// 单个条目失败不会中断整体，错误用 errors.Join 汇总；返回值是已删除的条目数。
func (s *Store) CleanupTemp(ctx context.Context, olderThan time.Duration) (int, error) {
	const op = "CleanupTemp"
	if olderThan <= 0 {
		return 0, opErr(op, "", invalidArgument("olderThan must be positive"))
	}
	release, err := s.begin(ctx, op, "")
	if err != nil {
		return 0, err
	}
	defer release()

	cutoff := time.Now().Add(-olderThan)
	entries, err := s.readDirEntries(tempDirName)
	if err != nil {
		return 0, opErr(op, "", err)
	}
	removed := 0
	var errs []error
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		name := filepath.Join(tempDirName, e.Name())
		info, err := e.Info()
		if err != nil {
			if !isNotExist(err) {
				errs = append(errs, classify(err))
			}
			continue
		}
		if !info.ModTime().Before(cutoff) || !info.Mode().IsRegular() {
			continue
		}
		if err := s.root.Remove(name); err != nil && !isNotExist(err) {
			errs = append(errs, classify(err))
			continue
		}
		removed++
	}
	if err := errors.Join(errs...); err != nil {
		return removed, opErr(op, "", err)
	}
	return removed, nil
}

// readDirEntries 读取 dir 下的全部条目，最多一次系统调用。
func (s *Store) readDirEntries(dir string) ([]fs.DirEntry, error) {
	f, err := s.root.Open(dir)
	if err != nil {
		return nil, classify(err)
	}
	defer func() { _ = f.Close() }()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, classify(err)
	}
	return entries, nil
}
