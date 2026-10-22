package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/model"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/repo"
	"github.com/apus-run/gala/pkg/errorsx"
)

const (
	// DefaultChunkSize 是分块上传的默认块大小（4 MiB）。
	DefaultChunkSize = int64(4 << 20)
	// MaxChunkSize 是单块上限（32 MiB）。
	MaxChunkSize = int64(32 << 20)
	// MinChunkedFileSize 是启用分块上传的最小文件大小（10 MiB）。
	MinChunkedFileSize = int64(10 << 20)
	// SessionTTL 是断点续传会话与其孤儿对象的保留期；GC 周期性清理。
	SessionTTL = 24 * time.Hour
)

// ChunkInitResult 是初始化分块上传的结果。
type ChunkInitResult struct {
	UploadID    string `json:"upload_id"`
	ChunkSize   int64  `json:"chunk_size"`
	TotalChunks int    `json:"total_chunks"`
}

// ChunkProgress 是断点续传进度，UploadedBits 供客户端续传缺失分块。
type ChunkProgress struct {
	UploadID      string `json:"upload_id"`
	TotalChunks   int    `json:"total_chunks"`
	UploadedCount int    `json:"uploaded_count"`
	UploadedBits  []bool `json:"uploaded_bits"`
}

// ChunkService 是断点续传业务。会话状态在数据库（不依赖 Redis），
// 分块是普通对象，合并按序流式进行，全程不整文件落本地盘。
type ChunkService interface {
	// Init 建立分块上传会话；replaceOf 非空表示完成后替换该哈希的文件（保留显示名与目录）。
	Init(ctx context.Context, userID uint64, folder, name, contentType string, size, chunkSize int64, replaceOf string) (*ChunkInitResult, error)
	// UploadChunk 幂等：重复上传同一分块直接成功，不覆盖已有数据。
	UploadChunk(ctx context.Context, userID uint64, uploadID string, index int, size int64, r io.Reader) error
	Progress(ctx context.Context, userID uint64, uploadID string) (*ChunkProgress, error)
	// Complete 校验分块齐全后合并、计算哈希并走与普通上传相同的登记（含秒传）。
	Complete(ctx context.Context, userID uint64, uploadID string) (*UploadResult, error)
	Cancel(ctx context.Context, userID uint64, uploadID string) error
	// GC 清理过期会话与孤儿暂存对象，由 main 的定时器调用。
	GC(ctx context.Context)
}

type chunkSvc struct {
	files *fileSvc
	repo  repo.Repository
}

var _ ChunkService = (*chunkSvc)(nil)

// NewChunkService 构造断点续传业务。files 必须是 NewFileService 的返回值：
// 分块上传复用它的写入与登记路径。
func NewChunkService(files *fileSvc, r repo.Repository) *chunkSvc {
	return &chunkSvc{files: files, repo: r}
}

func (s *chunkSvc) Init(ctx context.Context, userID uint64, folder, name, contentType string, size, chunkSize int64, replaceOf string) (*ChunkInitResult, error) {
	if err := validateFolder(folder); err != nil {
		return nil, err
	}
	if err := validateName(name); err != nil {
		return nil, err
	}
	if replaceOf != "" {
		// 替换目标必须属于该用户；会话期间它可能被删，Complete 时再次校验。
		if _, err := s.repo.UserFile(ctx, userID, replaceOf); err != nil {
			return nil, mapRepoErr(err)
		}
	}
	if size < MinChunkedFileSize {
		return nil, badRequest(fmt.Sprintf("files smaller than %d bytes use the plain upload", MinChunkedFileSize))
	}
	if size > s.files.maxSize {
		return nil, tooLarge(s.files.maxSize)
	}
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if chunkSize > MaxChunkSize {
		return nil, badRequest(fmt.Sprintf("chunk size exceeds %d bytes", MaxChunkSize))
	}
	total := int((size + chunkSize - 1) / chunkSize)

	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	sess := &model.UploadSession{
		ID:           hex.EncodeToString(b[:]),
		UserID:       userID,
		FileName:     name,
		Folder:       folder,
		ReplaceOf:    replaceOf,
		FileSize:     size,
		ContentType:  contentType,
		ChunkSize:    chunkSize,
		TotalChunks:  total,
		UploadedMask: make([]byte, (total+7)/8),
	}
	if err := s.repo.CreateSession(ctx, sess); err != nil {
		return nil, err
	}
	return &ChunkInitResult{UploadID: sess.ID, ChunkSize: chunkSize, TotalChunks: total}, nil
}

func (s *chunkSvc) UploadChunk(ctx context.Context, userID uint64, uploadID string, index int, size int64, r io.Reader) error {
	if index < 0 || size < 0 {
		return badRequest("invalid chunk index or size")
	}
	sess, err := s.owned(ctx, userID, uploadID)
	if err != nil {
		return err
	}
	if index >= sess.TotalChunks {
		return badRequest("chunk index out of range")
	}
	if sess.Has(index) {
		return nil // 已收到：断点续传的幂等保证
	}
	key := chunkKey(uploadID, index)
	if err := s.files.put(ctx, key, r, size, ""); err != nil {
		s.files.bestEffortRemove(ctx, key)
		return storageErr(err)
	}
	return s.repo.SetSessionBit(ctx, uploadID, index)
}

func (s *chunkSvc) Progress(ctx context.Context, userID uint64, uploadID string) (*ChunkProgress, error) {
	sess, err := s.owned(ctx, userID, uploadID)
	if err != nil {
		return nil, err
	}
	bits := make([]bool, sess.TotalChunks)
	for i := range bits {
		bits[i] = sess.Has(i)
	}
	return &ChunkProgress{
		UploadID:      uploadID,
		TotalChunks:   sess.TotalChunks,
		UploadedCount: sess.Count(),
		UploadedBits:  bits,
	}, nil
}

func (s *chunkSvc) Complete(ctx context.Context, userID uint64, uploadID string) (*UploadResult, error) {
	sess, err := s.owned(ctx, userID, uploadID)
	if err != nil {
		return nil, err
	}
	if got := sess.Count(); got < sess.TotalChunks {
		return nil, badRequest(fmt.Sprintf("chunks incomplete: %d of %d", got, sess.TotalChunks))
	}

	seq := &chunkSeq{ctx: ctx, storage: s.files.storage, uploadID: uploadID, total: sess.TotalChunks}
	defer seq.Close()
	h := sha256.New()
	staging, err := stagingKey()
	if err != nil {
		return nil, err
	}
	if err := s.repo.TrackStaging(ctx, staging); err != nil {
		return nil, err
	}
	if err := s.files.put(ctx, staging, io.TeeReader(seq, h), sess.FileSize, sess.ContentType); err != nil {
		s.files.cleanupFailed(ctx, staging)
		return nil, storageErr(err)
	}

	// 先登记（可能秒传并删除暂存对象），再清分块与会话。
	newHash := hex.EncodeToString(h.Sum(nil))
	res, err := s.files.register(ctx, userID, sess.Folder, newHash, sess.FileName, sess.ContentType, sess.FileSize, staging)
	if err != nil {
		return nil, err
	}
	if sess.ReplaceOf != "" && newHash != sess.ReplaceOf {
		// 替换：新内容已就位，旧引用清理失败只留冗余关联。
		if err := s.files.Remove(ctx, userID, sess.ReplaceOf); err != nil {
			log.Printf("chunk replace: unlink old %s: %v", sess.ReplaceOf, err)
		}
	}
	if err := s.removeChunks(ctx, uploadID, sess.TotalChunks); err != nil {
		// 文件已经登记成功；保留 session，让 GC 重试分块清理。
		log.Printf("cleanup chunks %s: %v (session kept for GC)", uploadID, err)
		return res, nil
	}
	if err := s.repo.DeleteSession(ctx, uploadID); err != nil {
		log.Printf("delete session %s: %v (kept for GC)", uploadID, err)
	}
	return res, nil
}

func (s *chunkSvc) Cancel(ctx context.Context, userID uint64, uploadID string) error {
	sess, err := s.owned(ctx, userID, uploadID)
	if err != nil {
		return err
	}
	if err := s.removeChunks(ctx, uploadID, sess.TotalChunks); err != nil {
		return storageErr(err)
	}
	return s.repo.DeleteSession(ctx, uploadID)
}

func (s *chunkSvc) GC(ctx context.Context) {
	before := time.Now().Add(-SessionTTL)
	if sessions, err := s.repo.ExpiredSessions(ctx, before); err == nil {
		for _, sess := range sessions {
			if err := s.removeChunks(ctx, sess.ID, sess.TotalChunks); err != nil {
				log.Printf("gc chunks %s: %v (session kept)", sess.ID, err)
				continue
			}
			if err := s.repo.DeleteSession(ctx, sess.ID); err != nil {
				log.Printf("gc session %s: %v", sess.ID, err)
			}
		}
	}
	if keys, err := s.repo.ExpiredStaging(ctx, before); err == nil {
		for _, key := range keys {
			if err := s.files.storage.Remove(ctx, key); err != nil && !errors.Is(err, fileserver.ErrNotFound) {
				log.Printf("gc object %s: %v (kept)", key, err)
				continue
			}
			if err := s.repo.DropStaging(ctx, key); err != nil {
				log.Printf("gc staging row %s: %v", key, err)
			}
		}
	}
}

// removeChunks 删除一次会话的全部分块；任一块失败就保留 session 供 GC 重试。
func (s *chunkSvc) removeChunks(ctx context.Context, uploadID string, total int) error {
	for i := 0; i < total; i++ {
		key := chunkKey(uploadID, i)
		if err := s.files.storage.Remove(ctx, key); err != nil && !errors.Is(err, fileserver.ErrNotFound) {
			return fmt.Errorf("remove %s: %w", key, err)
		}
	}
	return nil
}

// owned 取会话并校验归属。
func (s *chunkSvc) owned(ctx context.Context, userID uint64, uploadID string) (*model.UploadSession, error) {
	sess, err := s.repo.Session(ctx, uploadID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if sess.UserID != userID {
		return nil, errorsx.Forbidden(errorsx.StatusForbidden)
	}
	return sess, nil
}

// chunkKey 是第 i 个分块的对象 Key。
func chunkKey(uploadID string, index int) string {
	return fmt.Sprintf("chunks/%s/%06d", uploadID, index)
}

// chunkSeq 把分块对象串成一个顺序 Reader：读完当前块再打开下一块，
// 合并时不需要同时持有全部分块的句柄，内存占用与分块数无关。
type chunkSeq struct {
	ctx      context.Context
	storage  fileserver.Storage
	uploadID string
	total    int
	next     int
	cur      io.ReadCloser
}

func (s *chunkSeq) Read(p []byte) (int, error) {
	for {
		if s.cur == nil {
			if s.next >= s.total {
				return 0, io.EOF
			}
			rc, err := s.storage.Read(s.ctx, chunkKey(s.uploadID, s.next))
			if err != nil {
				return 0, err
			}
			s.cur = rc
			s.next++
		}
		n, err := s.cur.Read(p)
		if n > 0 {
			return n, nil
		}
		if err == io.EOF {
			s.cur.Close()
			s.cur = nil
			continue
		}
		if err != nil {
			return 0, err
		}
	}
}

// Close 关闭当前打开的分块。
func (s *chunkSeq) Close() error {
	if s.cur != nil {
		return s.cur.Close()
	}
	return nil
}
