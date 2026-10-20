// Package filestore 提供 fileserver.Storage 的本地文件系统实现。
//
// 所有访问都经由构造时打开的 os.Root，Key 不会被拼接成全局路径，符号链接与 ".."
// 不能越出根目录。Key 映射到 data/ 之下，临时文件放在 .fs-tmp/，两个命名空间不相交。
// 普通对象先写入 .fs-tmp 中的临时文件再发布：覆盖写使用原子 rename，
// WithIfNotExists 使用 link（目标已存在即返回 ErrConflict）。
//
// 耐久性与平台承诺见 Durability。Linux ext4/xfs 是主验收目标；Windows 没有目录同步；
// NFS、SMB、FUSE 不在承诺范围内。
package filestore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/apus-run/gala/components/fileserver"
)

// Durability 选择发布时的耐久性策略。
//
// 验证边界：SyncFile / SyncFileAndDir 的收益只在掉电或内核崩溃时体现，进程内测试
// （包括 SIGKILL 崩溃测试）无法验证 fsync——page cache 不随进程消失。这里能保证的
// 是"策略被正确接线"，不是"数据一定落盘"。需要验收耐久性时，应在目标文件系统上
// 用块设备层工具（如 dm-log-writes）单独验证，而不是依赖单元测试。
//
// SyncFileAndDir 的接线包括：New 同步根目录，使 data/ 与 .fs-tmp/ 的目录项落盘；
// 新建嵌套目录时，在其他调用观察到该目录之前同步其父目录；发布或删除文件名之后同步叶子目录。
type Durability int

const (
	// SyncNone 不调用 Sync：最快，掉电可能丢失或截断内容。
	SyncNone Durability = iota
	// SyncFile 在发布前同步文件内容。
	SyncFile
	// SyncFileAndDir 在发布前同步文件，并在发布后同步父目录（默认）。
	SyncFileAndDir
)

// Option 配置 Store。它只能由本包构造，不能在外部伪造配置字段。
type Option func(*config)

// WithDurability 设置耐久性策略，默认 SyncFileAndDir。
func WithDurability(d Durability) Option {
	return func(c *config) { c.durability = d }
}

// WithFileMode 设置发布后对象文件的权限，默认 0o644。权限在发布前精确设置，不受 umask 影响。
func WithFileMode(m fs.FileMode) Option {
	return func(c *config) { c.fileMode = m }
}

// WithDirMode 设置自动创建目录的权限，默认 0o755。目录权限在创建时受进程 umask 影响。
func WithDirMode(m fs.FileMode) Option {
	return func(c *config) { c.dirMode = m }
}

// WithMaxObjectSize 限制单个对象的字节数，超过返回 ErrTooLarge。0 表示不限制。
func WithMaxObjectSize(n int64) Option {
	return func(c *config) { c.maxObject = n }
}

// WithBufferSize 设置拷贝缓冲大小，默认 256 KiB，上限 4 MiB。它决定单次上传或下载的内存上界。
func WithBufferSize(n int) Option {
	return func(c *config) { c.bufferSize = n }
}

type config struct {
	durability Durability
	fileMode   fs.FileMode
	dirMode    fs.FileMode
	maxObject  int64
	bufferSize int

	// hook 仅供测试在关键步骤注入失败，生产代码中为 nil。
	hook func(point string) error
}

func (c config) validate() error {
	switch {
	case c.durability < SyncNone || c.durability > SyncFileAndDir:
		return invalidArgument("unknown durability")
	case c.fileMode.Perm() != c.fileMode:
		return invalidArgument("file mode must be permission bits only")
	case c.dirMode.Perm() != c.dirMode:
		return invalidArgument("directory mode must be permission bits only")
	case c.maxObject < 0:
		return invalidArgument("max object size must not be negative")
	case c.bufferSize <= 0 || c.bufferSize > maxBufferSize:
		return invalidArgument("buffer size must be in (0, 4 MiB]")
	}
	return nil
}

// Store 是本地文件系统上的对象存储，实现 Storage。
// 方法对不同 Key 的并发调用是安全的。Close 之后的调用返回 ErrClosed。
//
// 同一个 root 只应创建一个 Store：发布与 Remove 的互斥只在同一个 Store 实例内有效。
// 多个实例或多个进程共享同一 root 时，对象仍按 rename/link 原子发布（不会出现混合内容），
// 但 Remove 可能删掉另一个实例刚刚发布的版本。
type Store struct {
	root  *os.Root
	cfg   config
	links linkMode // 硬链接探测结果；WithIfNotExists 依赖它

	// mu 的读锁由每个方法持有；Close 取写锁，等待进行中的调用退出。
	// 返回的读取流不持有锁，它持有自己的文件句柄。
	mu     sync.RWMutex
	closed bool

	// keyMu 串行化同一 Key 的发布与删除。拷贝不持有它。
	// 条带足够把不同 Key 分开；冲突时只是多等一次发布，不会把两个 Key 的内容混在一起。
	keyMu [lockStripes]sync.Mutex

	// dirLocks 按条带串行化目录上的创建与父目录同步；dirMu 保护 dirSynced。
	// dirSynced 记录本进程已同步过的目录，超过 maxSyncedDirs 时整体清空。
	dirLocks  [lockStripes]sync.Mutex
	dirMu     sync.Mutex
	dirSynced map[string]struct{}
}

// linkMode 是构造时对硬链接的探测结果。
type linkMode int

const (
	linkOK linkMode = iota
	linkUnsupported
	linkDenied
)

// lockStripes 是 Key 锁与目录锁各自的条带数。冲突只会多等，不改变语义。
const lockStripes = 64

var _ fileserver.Storage = (*Store)(nil)

// New 在已存在的目录 root 上创建 Store。root 必须是目录，本函数不会自动创建它，
// 以免路径写错时悄悄建出新目录。root 本身可以是符号链接。
func New(root string, opts ...Option) (*Store, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	r, err := openRoot(root)
	if err != nil {
		return nil, err
	}
	s := &Store{root: r, cfg: cfg, dirSynced: make(map[string]struct{})}
	if err := s.prepare(); err != nil {
		_ = r.Close()
		return nil, err
	}
	return s, nil
}

// openRoot 打开已存在的目录 root。
func openRoot(root string) (*os.Root, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("filestore: root is not accessible: %w", innerErr(err))
	}
	if !info.IsDir() {
		return nil, errors.New("filestore: root is not a directory")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("filestore: open root: %w", innerErr(err))
	}
	return r, nil
}

// prepare 创建内部目录并探测硬链接。SyncFileAndDir 时同步根目录，使新建的目录项落盘。
func (s *Store) prepare() error {
	for _, dir := range []string{dataDirName, tempDirName} {
		if err := ensureDir(s.root, dir, s.cfg.dirMode); err != nil {
			return fmt.Errorf("filestore: prepare %s: %w", dir, innerErr(err))
		}
	}
	links, err := probeHardlink(s.root)
	if err != nil {
		return fmt.Errorf("filestore: probe hard links: %w", innerErr(err))
	}
	s.links = links
	if s.cfg.durability >= SyncFileAndDir {
		if err := s.syncDirAt("."); err != nil {
			return fmt.Errorf("filestore: sync root: %w", innerErr(err))
		}
	}
	return nil
}

// Close 关闭根句柄。它会等待进行中的方法调用退出。重复调用返回 nil。
// Close 之后，方法返回 ErrClosed；已经打开的 Read 流仍可读取直到调用方关闭它。
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.root.Close()
}

// begin 为一次方法调用持有读锁，并检查 Store 未关闭、Context 未取消。返回的函数必须在调用结束时执行；
// 返回错误时锁已经释放。
func (s *Store) begin(ctx context.Context, op, key string) (func(), error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, opErr(op, key, ErrClosed)
	}
	if err := ctx.Err(); err != nil {
		s.mu.RUnlock()
		return nil, opErr(op, key, err)
	}
	return s.mu.RUnlock, nil
}

// ensureDir 确保 root 内存在目录 name。
//
// 内部目录必须是真实目录，不能是符号链接：否则 .fs-tmp 可以指向 data，
// CleanupTemp 会把数据区里的对象当成临时文件删除。
func ensureDir(r *os.Root, name string, perm fs.FileMode) error {
	if err := r.MkdirAll(name, perm); err != nil {
		return err
	}
	fi, err := r.Lstat(name)
	if err != nil {
		return err
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s must not be a symbolic link", name)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", name)
	}
	return nil
}

// hardlinkFS 是探测硬链接需要的文件系统操作。*os.Root 满足它，测试可以替换为注入故障的实现。
type hardlinkFS interface {
	OpenFile(name string, flag int, perm fs.FileMode) (*os.File, error)
	Link(oldname, newname string) error
	Remove(name string) error
}

// probeHardlink 探测根目录所在的文件系统是否支持硬链接。
// WithIfNotExists 依赖 Link 的原子“目标已存在”语义；不支持或被拒绝时必须在读取输入前返回，
// 而不是写完临时文件后才失败。EPERM 按权限错误处理，不把它记成“文件系统不支持硬链接”。
func probeHardlink(r hardlinkFS) (linkMode, error) {
	for range maxTempAttempts {
		suffix, err := randomHex(16)
		if err != nil {
			return linkOK, err
		}
		src := filepath.Join(tempDirName, "probe-"+suffix)
		dst := src + "-link"
		f, err := r.OpenFile(src, os.O_RDWR|os.O_CREATE|os.O_EXCL, tempFileMode)
		if err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return linkOK, err
		}
		_ = f.Close()

		err = r.Link(src, dst)
		switch {
		case err == nil:
			_ = r.Remove(dst)
			_ = r.Remove(src)
			return linkOK, nil
		case errors.Is(err, fs.ErrExist):
			_ = r.Remove(src)
			continue
		case isLinkUnsupported(err):
			_ = r.Remove(src)
			return linkUnsupported, nil
		case isLinkDenied(err):
			_ = r.Remove(src)
			return linkDenied, nil
		default:
			_ = r.Remove(src)
			return linkOK, err
		}
	}
	return linkOK, errors.New("filestore: could not probe hard links")
}

// innerErr 去掉 *fs.PathError 中的路径，避免错误文本包含本地绝对路径。
func innerErr(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// keyPath 把已校验的 Key 转换为 os.Root 使用的相对路径。
// Key 只会落在数据区，不会与临时区或根目录下的其它条目重合。
func keyPath(key string) string {
	return filepath.Join(dataDirName, filepath.FromSlash(key))
}

// parentDir 返回 Key 的父目录（相对于根）。顶层 Key 的父目录是 "."。
func parentDir(key string) string {
	return filepath.Dir(keyPath(key))
}

// randomHex 返回 n 字节随机数的十六进制表示。
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("filestore: random source: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// report 把已确认的字节数报告给观察者；观察者为 nil 时不做任何事。
func report(ctx context.Context, p fileserver.Progress, key string, transferred, total int64) {
	if p != nil {
		p.Report(ctx, fileserver.ProgressInfo{Key: key, Transferred: transferred, Total: total})
	}
}

// syncDirAt 按耐久策略同步 dir 的目录项，使刚发布或删除的名字在掉电后仍然存在。
func (s *Store) syncDirAt(dir string) error {
	if s.cfg.durability < SyncFileAndDir {
		return nil
	}
	if err := s.at("sync:" + dir); err != nil {
		return err
	}
	return syncDir(s.root, dir)
}

// lockKey 串行化 key 的发布与删除。返回的函数释放锁。
func (s *Store) lockKey(key string) func() {
	mu := &s.keyMu[stripeOf(key)]
	mu.Lock()
	return mu.Unlock
}

// stripeOf 把名字映射到锁条带（FNV-1a）。
func stripeOf(name string) uint {
	var h uint32 = 2166136261
	for i := 0; i < len(name); i++ {
		h ^= uint32(name[i])
		h *= 16777619
	}
	return uint(h % lockStripes)
}

// lockDir 串行化 dir 上的创建与父目录同步。返回的函数释放锁。
// 不同目录可能共用一个条带，代价只是多等一次，不影响正确性。
func (s *Store) lockDir(dir string) func() {
	mu := &s.dirLocks[stripeOf(dir)]
	mu.Lock()
	return mu.Unlock
}

func (s *Store) dirKnownSynced(dir string) bool {
	s.dirMu.Lock()
	defer s.dirMu.Unlock()
	_, ok := s.dirSynced[dir]
	return ok
}

func (s *Store) markDirSynced(dir string) {
	s.dirMu.Lock()
	defer s.dirMu.Unlock()
	if len(s.dirSynced) >= maxSyncedDirs {
		clear(s.dirSynced)
	}
	s.dirSynced[dir] = struct{}{}
}

// at 在命名步骤处执行测试钩子。生产配置中钩子为 nil，返回 nil。
func (s *Store) at(point string) error {
	if s.cfg.hook != nil {
		return s.cfg.hook(point)
	}
	return nil
}
