package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/internal/iox"
)

// uploadPrefix 是普通上传临时文件的名字前缀。
const uploadPrefix = "up-"

// tempFile 是临时区中的一个文件。发布成功后 published 为 true，discard 不再删除它。
type tempFile struct {
	root      *os.Root
	name      string
	file      *os.File
	published bool
}

// discard 关闭仍然打开的文件，并删除未发布的临时文件。删除失败的残留交给 CleanupTemp。
func (t *tempFile) discard() error {
	if t.file != nil {
		_ = t.file.Close()
		t.file = nil
	}
	if t.published {
		return nil
	}
	if err := t.root.Remove(t.name); err != nil && !isNotExist(err) {
		return err
	}
	return nil
}

// Upload 实现 fileserver.Uploader。
//
// 步骤：校验 → 创建父目录 → 写入临时文件（可选的长度与超长检查）→ 设置权限并同步
// → 发布（rename 或 link）→ 同步父目录。提交前的任何失败都不改变旧对象。
// fsync 不能被 Context 打断：它返回时若 Context 已取消，对象不会发布。
func (s *Store) Upload(ctx context.Context, key string, r io.Reader, opts ...fileserver.UploadOpt) error {
	const op = "Upload"
	if err := fileserver.ValidateKey(key); err != nil {
		return opErr(op, key, err)
	}
	uo, err := fileserver.NewUploadOptions(opts...)
	if err != nil {
		return opErr(op, key, err)
	}
	if err := checkSupported(uo); err != nil {
		return opErr(op, key, err)
	}
	if uo.IfNotExists() {
		if err := s.rejectConditionalCreate(); err != nil {
			return opErr(op, key, err)
		}
	}
	if r == nil {
		return opErr(op, key, invalidArgument("reader is nil"))
	}
	written, err := s.upload(ctx, op, key, r, uo)
	if err != nil {
		return err
	}
	// Progress 契约要求回调不在 Store 的锁内执行；upload 返回时读锁已经释放。
	report(ctx, uo.Progress(), key, written, written)
	return nil
}

// upload 在 Store 的读锁内提交对象，返回写入的字节数。锁在返回前释放。
func (s *Store) upload(ctx context.Context, op, key string, r io.Reader, uo fileserver.UploadOptions) (int64, error) {
	release, err := s.begin(ctx, op, key)
	if err != nil {
		return 0, err
	}
	defer release()
	if size, ok := uo.Size(); ok && s.cfg.maxObject > 0 && size > s.cfg.maxObject {
		return 0, opErr(op, key, tooLarge(s.cfg.maxObject))
	}
	written, err := s.commitUpload(ctx, key, r, uo)
	if err != nil {
		return 0, opErr(op, key, err)
	}
	return written, nil
}

// checkSupported 在任何副作用之前拒绝本后端不能持久化的选项。
func checkSupported(uo fileserver.UploadOptions) error {
	if uo.ContentType() != "" {
		return fmt.Errorf("%w: the local store does not persist content type", fileserver.ErrUnsupported)
	}
	if uo.Metadata() != nil {
		return fmt.Errorf("%w: the local store does not persist metadata", fileserver.ErrUnsupported)
	}
	return nil
}

// commitUpload 把输入写入临时文件并发布为 key，返回写入的字节数。
// 发布之前的任何失败都会删除临时文件；删除失败用 errors.Join 附带在主错误上。
func (s *Store) commitUpload(ctx context.Context, key string, r io.Reader, uo fileserver.UploadOptions) (written int64, err error) {
	if err := s.ensureParent(key); err != nil {
		return 0, err
	}
	tmp, err := s.createUploadTemp()
	if err != nil {
		return 0, err
	}
	defer func() {
		if derr := tmp.discard(); derr != nil {
			err = errors.Join(err, derr)
		}
	}()

	size, hasSize := uo.Size()
	if written, err = s.copyInput(ctx, tmp.file, r, size, hasSize); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err = s.finishTemp(tmp, s.cfg.fileMode); err != nil {
		return 0, err
	}
	// fsync 本身不能被取消。同步结束时 Context 已经取消的话，不发布，调用方看到 ctx 的错误。
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	unlock := s.lockKey(key)
	defer unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err = s.at("upload.before-publish"); err != nil {
		return 0, err
	}
	if err = s.publish(tmp, key, uo.IfNotExists()); err != nil {
		return 0, err
	}
	if err = s.at("upload.before-dir-sync"); err != nil {
		return written, indeterminate(err)
	}
	if err = s.syncParent(key); err != nil {
		return written, indeterminate(err)
	}
	return written, nil
}

// rejectConditionalCreate 在读取输入之前拒绝当前文件系统做不到的条件创建。
func (s *Store) rejectConditionalCreate() error {
	switch s.links {
	case linkUnsupported:
		return fmt.Errorf("%w: file system does not support hard links", fileserver.ErrUnsupported)
	case linkDenied:
		return fmt.Errorf("%w: hard links are not permitted", fileserver.ErrPermission)
	default:
		return nil
	}
}

// copyInput 把 r 复制到 dst。声明长度时最多读取 size+1 字节，使超长输入在提交前被发现；
// 每次读取前检查 ctx；超过 MaxObjectSize 时返回 ErrTooLarge。
func (s *Store) copyInput(ctx context.Context, dst io.Writer, r io.Reader, size int64, hasSize bool) (int64, error) {
	bufLen := s.cfg.bufferSize
	if hasSize && size+1 < int64(bufLen) {
		bufLen = int(size + 1) // 声明长度时最多读取 size+1 字节，不需要分配整段缓冲
	}
	buf := make([]byte, bufLen)
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		chunk := buf
		if hasSize {
			remaining := size + 1 - n
			if remaining <= 0 {
				break
			}
			if remaining < int64(len(chunk)) {
				chunk = chunk[:remaining]
			}
		}
		m, rerr := iox.ReadSome(r, chunk)
		if m > 0 {
			if s.cfg.maxObject > 0 && n+int64(m) > s.cfg.maxObject {
				return n, tooLarge(s.cfg.maxObject)
			}
			if _, err := dst.Write(chunk[:m]); err != nil {
				return n, err
			}
			n += int64(m)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return n, rerr
		}
	}
	if hasSize && n != size {
		return n, fmt.Errorf("%w: declared %d bytes, got %d", fileserver.ErrSizeMismatch, size, n)
	}
	return n, nil
}

// finishTemp 按耐久策略同步，再设置对外权限，然后关闭临时文件。
// 权限留到同步之后：同步可能很久，这段时间临时文件保持 0600。关闭错误会被返回。
func (s *Store) finishTemp(t *tempFile, mode fs.FileMode) error {
	if s.cfg.durability >= SyncFile {
		if err := t.file.Sync(); err != nil {
			return err
		}
	}
	if err := s.at("upload.after-sync"); err != nil {
		return err
	}
	if err := t.file.Chmod(mode); err != nil {
		return err
	}
	err := t.file.Close()
	t.file = nil
	return err
}

// publish 把临时文件发布为 key。覆盖模式使用 Rename 原子替换；ifNotExists 使用 Link，
// 目标已存在时返回 ErrConflict，不退化为"先检查后改名"的竞争写法。
func (s *Store) publish(tmp *tempFile, key string, ifNotExists bool) error {
	dst := keyPath(key)
	if !ifNotExists {
		if err := s.root.Rename(tmp.name, dst); err != nil {
			if s.isDir(dst) {
				return conflict("target is a directory")
			}
			return classify(err)
		}
		tmp.published = true
		return nil
	}
	err := s.root.Link(tmp.name, dst)
	switch {
	case err == nil:
		tmp.published = true
		// 对象已经可见；临时名的残留由 CleanupTemp 回收，不影响本次成功。
		_ = s.root.Remove(tmp.name)
		return nil
	case errors.Is(err, fs.ErrExist):
		return conflict("object already exists")
	case isLinkUnsupported(err):
		return fmt.Errorf("%w: file system does not support hard links: %w", fileserver.ErrUnsupported, err)
	case isLinkDenied(err):
		return fmt.Errorf("%w: hard links are not permitted: %w", fileserver.ErrPermission, err)
	default:
		return classify(err)
	}
}

// syncParent 按耐久策略同步 key 的父目录，使发布的名字在掉电后仍然存在。
func (s *Store) syncParent(key string) error {
	return s.syncDirAt(parentDir(key))
}

// ensureParent 逐级创建 key 的父目录，全部经由根句柄完成。
//
// 逐级创建而不是 MkdirAll，是为了让每一层的创建都在其父目录同步之后才对其他调用可见
// （见 mkdirSynced）。已有目录也同步其父目录，不依赖进程内缓存判断耐久性。
func (s *Store) ensureParent(key string) error {
	dir := parentDir(key)
	rel, ok := strings.CutPrefix(dir, dataDirName+string(filepath.Separator))
	if !ok {
		return nil // 顶层 Key：父目录就是数据区本身
	}
	current := dataDirName
	for _, name := range strings.Split(rel, string(filepath.Separator)) {
		parent := current
		current = filepath.Join(current, name)
		if err := s.mkdirSynced(parent, current); err != nil {
			return err
		}
	}
	return nil
}

// mkdirSynced 在 parent 的锁内创建 path。创建成功时，先同步 parent 再放锁，
// 这样其他调用不会使用同步失败后即将被回滚的目录。已有目录同样同步 parent。
func (s *Store) mkdirSynced(parent, path string) error {
	unlock := s.lockDir(parent)
	defer unlock()

	err := s.root.Mkdir(path, s.cfg.dirMode)
	switch {
	case err == nil:
		if err := s.syncDirAt(parent); err != nil {
			if rmErr := s.root.Remove(path); rmErr != nil && !isNotExist(rmErr) {
				return errors.Join(err, rmErr)
			}
			return err
		}
		return nil
	case errors.Is(err, fs.ErrExist):
		fi, statErr := s.root.Lstat(path)
		if statErr != nil {
			if isNotExist(statErr) {
				return conflict("a parent path component disappeared during creation")
			}
			return classify(statErr)
		}
		if !fi.IsDir() {
			return conflict("a parent path component is not a directory")
		}
		return s.syncDirAt(parent)
	default:
		if isNotDir(err) {
			return conflict("a parent path component is not a directory")
		}
		return classify(err)
	}
}

// createUploadTemp 在 New 已准备好的临时区创建文件。O_EXCL 保证不覆盖已有文件。
func (s *Store) createUploadTemp() (*tempFile, error) {
	for range maxTempAttempts {
		suffix, err := randomHex(16)
		if err != nil {
			return nil, err
		}
		name := filepath.Join(tempDirName, uploadPrefix+suffix)
		f, err := s.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, tempFileMode)
		if err == nil {
			return &tempFile{root: s.root, name: name, file: f}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, classify(err)
		}
	}
	return nil, errors.New("filestore: could not allocate a unique temporary name")
}

// isDir 报告 name 是否是目录（不跟随符号链接）。
func (s *Store) isDir(name string) bool {
	fi, err := s.root.Lstat(name)
	return err == nil && fi.IsDir()
}

func tooLarge(limit int64) error {
	return fmt.Errorf("%w: exceeds %d bytes", fileserver.ErrTooLarge, limit)
}

// indeterminate 表示对象已经可见，但耐久性未能确认。
func indeterminate(err error) error {
	return fmt.Errorf("%w: object is visible but durability is not confirmed: %w", fileserver.ErrIndeterminate, err)
}
