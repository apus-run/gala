// Package fileservertest 提供 fileserver 的进程内 Fake、共享契约测试套件与故障注入工具，
// 供本模块的驱动测试、业务方测试以及第三方后端复用。
//
// 第三方后端实现 fileserver.Storage 后，调用 RunStorageSuite 即可自测。
package fileservertest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"sync"
	"time"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/internal/iox"
)

// MemStore 是进程内的 Fake，实现 Storage 与全部可选语义。
// 它只用于测试：没有持久化，也不模拟耐久性与并发下的故障。零值不可用，请用 NewMemStore 创建。
type MemStore struct {
	mu      sync.Mutex
	objects map[string]memObject
}

var _ fileserver.Storage = (*MemStore)(nil)

type memObject struct {
	data        []byte // 写入后不再原地修改，读者可以安全地持有快照
	contentType string
	metadata    map[string]string
	modTime     time.Time
}

// NewMemStore 创建一个空的 Fake。
func NewMemStore() *MemStore {
	return &MemStore{objects: make(map[string]memObject)}
}

// Upload 实现 Uploader。
func (m *MemStore) Upload(ctx context.Context, key string, r io.Reader, opts ...fileserver.UploadOpt) error {
	const op = "Upload"
	if err := validateEntry(ctx, key); err != nil {
		return opError(op, key, err)
	}
	uo, err := fileserver.NewUploadOptions(opts...)
	if err != nil {
		return opError(op, key, err)
	}
	if r == nil {
		return opError(op, key, fmt.Errorf("%w: reader is nil", fileserver.ErrInvalidArgument))
	}
	data, err := readBody(ctx, r, uo)
	if err != nil {
		return opError(op, key, err)
	}

	m.mu.Lock()
	if _, exists := m.objects[key]; exists && uo.IfNotExists() {
		m.mu.Unlock()
		return opError(op, key, fmt.Errorf("%w: object already exists", fileserver.ErrConflict))
	}
	m.objects[key] = memObject{
		data:        data,
		contentType: uo.ContentType(),
		metadata:    uo.Metadata(),
		modTime:     time.Now(),
	}
	m.mu.Unlock()

	size := int64(len(data))
	report(ctx, uo.Progress(), key, size, size)
	return nil
}

// Download 实现 Downloader。
func (m *MemStore) Download(ctx context.Context, key string, w io.WriterAt, opts ...fileserver.DownloadOpt) error {
	const op = "Download"
	if err := validateEntry(ctx, key); err != nil {
		return opError(op, key, err)
	}
	do, err := fileserver.NewDownloadOptions(opts...)
	if err != nil {
		return opError(op, key, err)
	}
	if w == nil {
		return opError(op, key, fmt.Errorf("%w: writer is nil", fileserver.ErrInvalidArgument))
	}
	data, err := m.snapshot(key)
	if err != nil {
		return opError(op, key, err)
	}
	off, length := do.Range()
	start, n, err := iox.ResolveRange(int64(len(data)), off, length)
	if err != nil {
		return opError(op, key, err)
	}
	src := bytes.NewReader(data[start : start+n])
	written, err := iox.CopyN(ctx, io.NewOffsetWriter(w, start), src, n, make([]byte, 32<<10))
	if err != nil {
		return opError(op, key, err)
	}
	report(ctx, do.Progress(), key, written, n)
	return nil
}

// memReader 把 SectionReader 包装成 io.ReadCloser；Close 是空操作。
type memReader struct {
	*io.SectionReader
}

func (memReader) Close() error { return nil }

// Read 实现 Storage。返回的流绑定打开时的快照。
func (m *MemStore) Read(ctx context.Context, key string, opts ...fileserver.ReadOpt) (io.ReadCloser, error) {
	const op = "Read"
	if err := validateEntry(ctx, key); err != nil {
		return nil, opError(op, key, err)
	}
	ro, err := fileserver.NewReadOptions(opts...)
	if err != nil {
		return nil, opError(op, key, err)
	}
	data, err := m.snapshot(key)
	if err != nil {
		return nil, opError(op, key, err)
	}
	off, length := ro.Range()
	start, n, err := iox.ResolveRange(int64(len(data)), off, length)
	if err != nil {
		return nil, opError(op, key, err)
	}
	return memReader{io.NewSectionReader(bytes.NewReader(data), start, n)}, nil
}

// Stat 实现 Storage。
func (m *MemStore) Stat(ctx context.Context, key string) (*fileserver.ObjectInfo, error) {
	const op = "Stat"
	if err := validateEntry(ctx, key); err != nil {
		return nil, opError(op, key, err)
	}
	m.mu.Lock()
	obj, ok := m.objects[key]
	m.mu.Unlock()
	if !ok {
		return nil, opError(op, key, fileserver.ErrNotFound)
	}
	return &fileserver.ObjectInfo{
		Key:         key,
		Size:        int64(len(obj.data)),
		ModTime:     obj.modTime,
		ContentType: obj.contentType,
		Metadata:    maps.Clone(obj.metadata),
	}, nil
}

// Remove 实现 Storage。对象不存在时返回 nil。
func (m *MemStore) Remove(ctx context.Context, key string) error {
	const op = "Remove"
	if err := validateEntry(ctx, key); err != nil {
		return opError(op, key, err)
	}
	m.mu.Lock()
	delete(m.objects, key)
	m.mu.Unlock()
	return nil
}

func (m *MemStore) snapshot(key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	obj, ok := m.objects[key]
	if !ok {
		return nil, fileserver.ErrNotFound
	}
	return obj.data, nil
}

// readBody 读取整个输入。声明长度时最多读取 size+1 字节，以在不无界读取的前提下发现超长输入。
func readBody(ctx context.Context, r io.Reader, uo fileserver.UploadOptions) ([]byte, error) {
	size, hasSize := uo.Size()
	src := iox.Reader(ctx, r)
	if hasSize {
		src = io.LimitReader(src, size+1)
	}
	data, err := io.ReadAll(src)
	if err != nil {
		return nil, err
	}
	if hasSize && int64(len(data)) != size {
		return nil, fmt.Errorf("%w: declared %d bytes, got %d", fileserver.ErrSizeMismatch, size, len(data))
	}
	return data, nil
}

func validateEntry(ctx context.Context, key string) error {
	if err := fileserver.ValidateKey(key); err != nil {
		return err
	}
	return ctx.Err()
}

func report(ctx context.Context, p fileserver.Progress, key string, transferred, total int64) {
	if p != nil {
		p.Report(ctx, fileserver.ProgressInfo{Key: key, Transferred: transferred, Total: total})
	}
}

func opError(op, key string, err error) error {
	return &fileserver.Error{Op: op, Key: key, Err: err}
}
