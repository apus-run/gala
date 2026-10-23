package filestore

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/internal/iox"
)

// Download 实现 fileserver.Downloader。对象的第 i 个字节写到 w 的偏移 i 处，范围下载亦然。
// 失败时 w 中可能已有部分内容；Download 不截断也不关闭 w。
func (s *Store) Download(ctx context.Context, key string, w io.WriterAt, opts ...fileserver.DownloadOpt) error {
	const op = "Download"
	if err := fileserver.ValidateKey(key); err != nil {
		return opErr(op, key, err)
	}
	do, err := fileserver.NewDownloadOptions(opts...)
	if err != nil {
		return opErr(op, key, err)
	}
	if w == nil {
		return opErr(op, key, invalidArgument("writer is nil"))
	}
	n, err := s.download(ctx, op, key, w, do)
	if err != nil {
		return err
	}
	// Progress 契约要求回调不在 Store 的锁内执行；download 返回时读锁已经释放。
	report(ctx, do.Progress(), key, n, n)
	return nil
}

// download 在 Store 的读锁内把范围内的字节写入 w，返回写入的字节数。锁在返回前释放。
func (s *Store) download(ctx context.Context, op, key string, w io.WriterAt, do fileserver.DownloadOptions) (int64, error) {
	release, err := s.begin(ctx, op, key)
	if err != nil {
		return 0, err
	}
	defer release()

	f, fi, err := s.openObject(key)
	if err != nil {
		return 0, opErr(op, key, err)
	}
	defer func() { _ = f.Close() }()

	off, length := do.Range()
	start, n, err := iox.ResolveRange(fi.Size(), off, length)
	if err != nil {
		return 0, opErr(op, key, err)
	}
	if n == 0 {
		return 0, nil
	}
	buf := make([]byte, min(int64(s.cfg.bufferSize), n))
	written, err := iox.CopyN(ctx, io.NewOffsetWriter(w, start), io.NewSectionReader(f, start, n), n, buf)
	if err != nil {
		return 0, opErr(op, key, err)
	}
	if written != n {
		return 0, opErr(op, key, fmt.Errorf("%w: object changed during download", fileserver.ErrConflict))
	}
	return n, nil
}

// Read 实现 fileserver.Storage。返回的流绑定打开时的文件，调用方必须关闭它。
// 流不依赖调用时的 Context，方法返回后它不会被取消。
func (s *Store) Read(ctx context.Context, key string, opts ...fileserver.ReadOpt) (io.ReadCloser, error) {
	result, err := s.open(ctx, "Read", key, opts...)
	if err != nil {
		return nil, err
	}
	return result.Body, nil
}

var _ fileserver.Opener = (*Store)(nil)

// Open 返回同一文件句柄上的内容与属性，后续覆盖或删除 Key 不改变已打开的视图。
func (s *Store) Open(ctx context.Context, key string, opts ...fileserver.ReadOpt) (*fileserver.OpenResult, error) {
	return s.open(ctx, "Open", key, opts...)
}

func (s *Store) open(ctx context.Context, op, key string, opts ...fileserver.ReadOpt) (*fileserver.OpenResult, error) {
	if err := fileserver.ValidateKey(key); err != nil {
		return nil, opErr(op, key, err)
	}
	ro, err := fileserver.NewReadOptions(opts...)
	if err != nil {
		return nil, opErr(op, key, err)
	}
	release, err := s.begin(ctx, op, key)
	if err != nil {
		return nil, err
	}
	defer release()

	f, fi, err := s.openObject(key)
	if err != nil {
		return nil, opErr(op, key, err)
	}
	off, length := ro.Range()
	start, n, err := iox.ResolveRange(fi.Size(), off, length)
	if err != nil {
		_ = f.Close()
		return nil, opErr(op, key, err)
	}
	return &fileserver.OpenResult{
		Body:   &objectReader{SectionReader: io.NewSectionReader(f, start, n), file: f},
		Info:   fileserver.ObjectInfo{Key: key, Size: fi.Size(), ModTime: fi.ModTime()},
		Offset: start,
		Length: n,
	}, nil
}

// Stat 实现 fileserver.Storage。ContentType 为空，Metadata 为 nil：本后端不持久化这两项。
func (s *Store) Stat(ctx context.Context, key string) (*fileserver.ObjectInfo, error) {
	const op = "Stat"
	if err := fileserver.ValidateKey(key); err != nil {
		return nil, opErr(op, key, err)
	}
	release, err := s.begin(ctx, op, key)
	if err != nil {
		return nil, err
	}
	defer release()

	fi, err := s.root.Stat(keyPath(key))
	if err != nil {
		return nil, opErr(op, key, classify(err))
	}
	if !fi.Mode().IsRegular() {
		return nil, opErr(op, key, notFound("not a regular file"))
	}
	return &fileserver.ObjectInfo{
		Key:     key,
		Size:    fi.Size(),
		ModTime: fi.ModTime(),
	}, nil
}

// Remove 实现 fileserver.Storage。对象不存在、或 Key 指向目录与设备等非常规文件时返回 nil，
// 且不改动这些条目。返回前按耐久策略同步父目录。
func (s *Store) Remove(ctx context.Context, key string) error {
	const op = "Remove"
	if err := fileserver.ValidateKey(key); err != nil {
		return opErr(op, key, err)
	}
	release, err := s.begin(ctx, op, key)
	if err != nil {
		return err
	}
	defer release()

	// 与 publish 共用同一把 Key 锁：Stat 与 Remove 之间不能插入一次发布，
	// 否则这里会删掉刚刚换上去的新对象。
	unlock := s.lockKey(key)
	defer unlock()

	name := keyPath(key)
	fi, err := s.root.Stat(name)
	switch {
	case isNotExist(err):
		return nil
	case err != nil:
		return opErr(op, key, classify(err))
	case !fi.Mode().IsRegular():
		return nil
	}
	if err := s.at("remove.after-stat"); err != nil {
		return opErr(op, key, err)
	}
	if err := s.root.Remove(name); err != nil && !isNotExist(err) {
		return opErr(op, key, classify(err))
	}
	if err := s.syncParent(key); err != nil {
		return opErr(op, key, indeterminate(err))
	}
	return nil
}

// openObject 打开 key 指向的普通文件并返回该句柄的属性。非常规文件视为不存在。
// 使用非阻塞标志打开，避免 FIFO 在没有写者时阻塞调用方。
func (s *Store) openObject(key string) (*os.File, os.FileInfo, error) {
	f, err := s.root.OpenFile(keyPath(key), os.O_RDONLY|nonblockFlag, 0)
	if err != nil {
		return nil, nil, classify(err)
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, classify(err)
	}
	if !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, notFound("not a regular file")
	}
	return f, fi, nil
}

// objectReader 把一个范围读取器与它的文件句柄绑定，Close 关闭文件。
// SectionReader 另外提供 ReadAt（可选能力，不属于 Read 的契约）：偏移相对于本次 Read 的范围起点，且不移动顺序读取的位置。
type objectReader struct {
	*io.SectionReader
	file *os.File
}

// Close 关闭底层文件。
func (r *objectReader) Close() error {
	return r.file.Close()
}
