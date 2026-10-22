// Package service 实现文件托管业务：上传去重（秒传）、下载、列表、重命名、
// 删除与断点续传。只依赖 fileserver.Storage 与 repo.Repository，HTTP 适配在 api 包。
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
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/smithy-go"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/model"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/repo"
	"github.com/apus-run/gala/pkg/errorsx"
)

// UploadResult 是一次上传的结果；IsDedup 为 true 表示内容已存在（秒传）。
type UploadResult struct {
	FileHash string `json:"file_hash"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
	IsDedup  bool   `json:"is_dedup"`
}

// FileDetail 是对外可见的文件元信息。内容寻址使 FileHash 同时是强 ETag 候选。
type FileDetail struct {
	FileHash    string    `json:"file_hash"`
	FileName    string    `json:"file_name"`
	FileSize    int64     `json:"file_size"`
	ContentType string    `json:"content_type"`
	CreatedAt   time.Time `json:"created_at"`
}

// FileService 是文件业务。Range 用 (off, length) 表达，length 为 -1 表示到末尾。
// StatByHash/OpenByHash/ListAllFiles 服务公开接口：哈希即能力凭证，不做归属检查。
type FileService interface {
	// Upload 流式上传：写入暂存对象的同时计算 SHA-256，再按哈希登记（内容已存在则秒传）。
	// folder 是用户视角的归属目录（"" 为根）。
	Upload(ctx context.Context, userID uint64, folder, name, contentType string, size int64, r io.Reader) (*UploadResult, error)
	// StatByHash 返回登记项的元信息（展示名是首个上传者给的名字）。
	StatByHash(ctx context.Context, hash string) (*FileDetail, error)
	// OpenByHash 打开内容流。off 超出大小返回 416；off 等于大小时返回空流。
	OpenByHash(ctx context.Context, hash string, off, length int64) (io.ReadCloser, error)
	// List 返回用户在 folder 目录下的文件（"" 为根）。
	List(ctx context.Context, userID uint64, folder string, page, pageSize int) ([]model.FileListItem, int64, error)
	// Replace 用新内容替换用户名下的 hash：保留显示名与目录，旧内容按引用计数移除。
	Replace(ctx context.Context, userID uint64, hash, contentType string, size int64, r io.Reader) (*UploadResult, error)
	// Folders / CreateFolder / DeleteFolder 管理用户的文件夹；只允许删除空文件夹。
	Folders(ctx context.Context, userID uint64) ([]model.UserFolder, error)
	CreateFolder(ctx context.Context, userID uint64, path string) error
	DeleteFolder(ctx context.Context, userID uint64, path string) error
	ListAllFiles(ctx context.Context, page, pageSize int) ([]model.PublicFileItem, int64, error)
	Rename(ctx context.Context, userID uint64, hash, name string) error
	// Remove 删除用户关联；引用计数归零时删除物理对象。
	Remove(ctx context.Context, userID uint64, hash string) error
}

type fileSvc struct {
	repo    repo.Repository
	storage fileserver.Storage
	maxSize int64
}

var _ FileService = (*fileSvc)(nil)

// NewFileService 构造文件业务。maxSize 为单文件上限（字节）。
func NewFileService(r repo.Repository, storage fileserver.Storage, maxSize int64) *fileSvc {
	return &fileSvc{repo: r, storage: storage, maxSize: maxSize}
}

func (s *fileSvc) Upload(ctx context.Context, userID uint64, folder, name, contentType string, size int64, r io.Reader) (*UploadResult, error) {
	if err := validateFolder(folder); err != nil {
		return nil, err
	}
	if err := validateName(name); err != nil {
		return nil, err
	}
	return s.upload(ctx, userID, folder, name, contentType, size, r)
}

// upload 校验大小后写暂存对象并登记；Upload 与 Replace 共用。
func (s *fileSvc) upload(ctx context.Context, userID uint64, folder, name, contentType string, size int64, r io.Reader) (*UploadResult, error) {
	if size < 0 {
		return nil, badRequest("file size must not be negative")
	}
	if size > s.maxSize {
		return nil, tooLarge(s.maxSize)
	}

	key, err := stagingKey()
	if err != nil {
		return nil, err
	}
	// 先登记暂存再写对象：崩溃后由 GC 按登记表回收孤儿（契约无 List）。
	if err := s.repo.TrackStaging(ctx, key); err != nil {
		return nil, err
	}
	h := sha256.New()
	if err := s.put(ctx, key, io.TeeReader(r, h), size, contentType); err != nil {
		s.cleanupFailed(ctx, key)
		return nil, storageErr(err)
	}
	return s.register(ctx, userID, folder, hex.EncodeToString(h.Sum(nil)), name, contentType, size, key)
}

// Replace 用新内容替换用户名下的 oldHash：保留原显示名与目录，指向新内容；
// 新内容走与普通上传相同的秒传登记，旧内容按引用计数移除。
// 替换为相同内容时是幂等的：不改变引用，直接返回秒传结果。
func (s *fileSvc) Replace(ctx context.Context, userID uint64, oldHash, contentType string, size int64, r io.Reader) (*UploadResult, error) {
	uf, err := s.repo.UserFile(ctx, userID, oldHash)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	res, err := s.upload(ctx, userID, uf.Folder, uf.FileName, contentType, size, r)
	if err != nil {
		return nil, err
	}
	if res.FileHash != oldHash {
		// 新内容已就位；旧引用清理失败只留冗余关联，不影响新内容可见。
		if err := s.Remove(ctx, userID, oldHash); err != nil {
			log.Printf("replace: unlink old %s: %v", oldHash, err)
		}
	}
	return res, nil
}

// register 把暂存对象登记为正式内容。哈希已存在（或并发同哈希写入撞唯一索引）时走秒传：
// 删除暂存对象、为该用户补一条关联并递增引用计数。登记成功时暂存 Key 即成为该内容的
// 正式存储位置。同一用户重复上传不再递增计数——引用以 (user,hash) 关联行为准。
func (s *fileSvc) register(ctx context.Context, userID uint64, folder, hash, name, contentType string, size int64, stagingKey string) (*UploadResult, error) {
	dedup := func() (*UploadResult, error) {
		if _, err := s.repo.FileByHash(ctx, hash); err != nil {
			return nil, mapRepoErr(err)
		}
		// 先建立/确认用户关联并递增引用；成功后才删重复的暂存对象。
		// 如果 FileMeta 在并发删除中消失，这里会失败，暂存对象仍然保留，不会被误删。
		uf := &model.UserFile{UserID: userID, FileHash: hash, FileName: name, Folder: folder}
		if err := s.repo.LinkUserAndIncRef(ctx, uf); err != nil && !errors.Is(err, repo.ErrDuplicate) {
			return nil, err
		}
		// 删除暂存对象；失败则保留登记，交给 GC 重试。
		if err := s.storage.Remove(ctx, stagingKey); err != nil && !errors.Is(err, fileserver.ErrNotFound) {
			log.Printf("dedup cleanup %s: %v (kept for GC)", stagingKey, err)
		} else if err := s.repo.DropStaging(ctx, stagingKey); err != nil {
			log.Printf("drop staging %s: %v (kept for GC)", stagingKey, err)
		}
		return &UploadResult{FileHash: hash, FileName: name, FileSize: size, IsDedup: true}, nil
	}

	if _, err := s.repo.FileByHash(ctx, hash); err == nil {
		return dedup()
	} else if !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}

	meta := &model.FileMeta{
		FileHash:    hash,
		FileName:    name,
		FileSize:    size,
		StorageKey:  stagingKey,
		ContentType: contentType,
		RefCount:    1,
	}
	uf := &model.UserFile{UserID: userID, FileHash: hash, FileName: name, Folder: folder}
	if err := s.repo.CreateFileAndLinkUser(ctx, meta, uf, stagingKey); err != nil {
		if errors.Is(err, repo.ErrDuplicate) {
			return dedup() // 并发上传同一内容：另一个请求先登记成功
		}
		return nil, err
	}
	return &UploadResult{FileHash: hash, FileName: name, FileSize: size, IsDedup: false}, nil
}

func (s *fileSvc) StatByHash(ctx context.Context, hash string) (*FileDetail, error) {
	fm, err := s.repo.FileByHash(ctx, hash)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &FileDetail{
		FileHash:    hash,
		FileName:    fm.FileName,
		FileSize:    fm.FileSize,
		ContentType: fm.ContentType,
		CreatedAt:   fm.CreatedAt,
	}, nil
}

func (s *fileSvc) OpenByHash(ctx context.Context, hash string, off, length int64) (io.ReadCloser, error) {
	fm, err := s.repo.FileByHash(ctx, hash)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if off < 0 || length < -1 {
		return nil, badRequest("invalid range")
	}
	if off > fm.FileSize {
		return nil, errorsx.New(http.StatusRequestedRangeNotSatisfiable, "RANGE_NOT_SATISFIABLE").
			WithMessage(fmt.Sprintf("offset %d is beyond size %d", off, fm.FileSize))
	}
	if off == fm.FileSize {
		return io.NopCloser(strings.NewReader("")), nil
	}
	var rc io.ReadCloser
	var rerr error
	if length == -1 {
		rc, rerr = s.storage.Read(ctx, fm.StorageKey)
	} else {
		rc, rerr = s.storage.Read(ctx, fm.StorageKey, fileserver.WithRange(off, length))
	}
	if rerr != nil {
		return nil, storageErr(rerr)
	}
	return rc, nil
}

func (s *fileSvc) List(ctx context.Context, userID uint64, folder string, page, pageSize int) ([]model.FileListItem, int64, error) {
	if err := validateFolder(folder); err != nil {
		return nil, 0, err
	}
	return listPage(page, pageSize, func(offset, limit int) ([]model.FileListItem, int64, error) {
		return s.repo.ListUserFiles(ctx, userID, folder, offset, limit)
	})
}

func (s *fileSvc) Folders(ctx context.Context, userID uint64) ([]model.UserFolder, error) {
	return s.repo.Folders(ctx, userID)
}

func (s *fileSvc) CreateFolder(ctx context.Context, userID uint64, path string) error {
	if err := validateFolder(path); err != nil {
		return err
	}
	if path == "" {
		return badRequest("folder path must not be empty")
	}
	err := s.repo.CreateFolder(ctx, &model.UserFolder{UserID: userID, Path: path})
	if errors.Is(err, repo.ErrDuplicate) {
		return badRequest("folder already exists")
	}
	return err
}

func (s *fileSvc) DeleteFolder(ctx context.Context, userID uint64, path string) error {
	if err := validateFolder(path); err != nil {
		return err
	}
	if path == "" {
		return badRequest("cannot delete the root folder")
	}
	empty, err := s.repo.FolderEmpty(ctx, userID, path)
	if err != nil {
		return err
	}
	if !empty {
		return badRequest("folder is not empty")
	}
	return mapRepoErr(s.repo.DeleteFolder(ctx, userID, path))
}

func (s *fileSvc) ListAllFiles(ctx context.Context, page, pageSize int) ([]model.PublicFileItem, int64, error) {
	return listPage(page, pageSize, func(offset, limit int) ([]model.PublicFileItem, int64, error) {
		return s.repo.ListAllFiles(ctx, offset, limit)
	})
}

// listPage 归一分页参数后委托给查询函数。
func listPage[T any](page, pageSize int, query func(offset, limit int) ([]T, int64, error)) ([]T, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}
	return query((page-1)*pageSize, pageSize)
}

func (s *fileSvc) Rename(ctx context.Context, userID uint64, hash, name string) error {
	if _, err := s.repo.UserFile(ctx, userID, hash); err != nil {
		return mapRepoErr(err)
	}
	if err := validateName(name); err != nil {
		return err
	}
	return mapRepoErr(s.repo.RenameUserFile(ctx, userID, hash, name))
}

func (s *fileSvc) Remove(ctx context.Context, userID uint64, hash string) error {
	key, remaining, err := s.repo.DeleteUserFileAndDecRef(ctx, userID, hash)
	if err != nil {
		return mapRepoErr(err)
	}
	if remaining > 0 {
		return nil
	}
	// DeleteUserFileAndDecRef 已把 StorageKey 写进 staging_objects 作为 outbox。
	// 物理删除失败时保留 outbox，由 GC 重试；接口仍按删除成功返回。
	if err := s.storage.Remove(ctx, key); err != nil && !errors.Is(err, fileserver.ErrNotFound) {
		log.Printf("delete %s queued: %v", key, err)
		return nil
	}
	if err := s.repo.DropStaging(ctx, key); err != nil {
		log.Printf("drop delete outbox %s: %v", key, err)
	}
	return nil
}

// put 写入一个对象。本地 filestore 不持久化 ContentType，契约保证该拒绝发生在
// 读取任何输入之前，因此重新传一次即可，业务不感知后端差异。
func (s *fileSvc) put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	err := s.storage.Upload(ctx, key, r,
		fileserver.WithSize(size), fileserver.WithContentType(contentType))
	if errors.Is(err, fileserver.ErrUnsupported) {
		err = s.storage.Upload(ctx, key, r, fileserver.WithSize(size))
	}
	return err
}

// cleanupFailed 回收一次失败上传的暂存对象与登记。
func (s *fileSvc) cleanupFailed(ctx context.Context, key string) {
	// 先删对象，成功后再清登记；删除失败时保留登记，GC 会重试。
	if err := s.storage.Remove(ctx, key); err != nil && !errors.Is(err, fileserver.ErrNotFound) {
		log.Printf("cleanup %s: %v (kept for GC)", key, err)
		return
	}
	if err := s.repo.DropStaging(ctx, key); err != nil {
		log.Printf("drop cleanup staging %s: %v (kept for GC)", key, err)
	}
}

// bestEffortRemove 删除对象并只记录失败：用于清理与回滚，不改变主流程的结果。
func (s *fileSvc) bestEffortRemove(ctx context.Context, key string) {
	if err := s.storage.Remove(ctx, key); err != nil && !errors.Is(err, fileserver.ErrNotFound) {
		log.Printf("remove %s: %v", key, err)
	}
}

// stagingKey 生成暂存对象 Key。哈希在读取中计算，对象先落在暂存名下；
// 登记成功后该 Key 即成为内容的正式存储位置，不产生第二次拷贝。
func stagingKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("staging key: %w", err)
	}
	return fmt.Sprintf("staging/%s/%x", time.Now().Format("2006/01/02"), b), nil
}

// validateName 校验展示名：非空、合法 UTF-8、≤255 字节、不含控制字符与路径分隔符。
func validateName(name string) error {
	if name == "" || len(name) > 255 || !utf8.ValidString(name) ||
		strings.ContainsAny(name, `\/`) ||
		strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 }) {
		return badRequest("invalid file name")
	}
	return nil
}

// maxFolderDepth 是目录路径的最大层级。
const maxFolderDepth = 6

// validateFolder 校验目录路径：复用 fileserver 的 Key 规则（段合法、无非法字符、
// 不含 ".."），另加总长与深度限制。空串表示根目录，总是合法。
func validateFolder(folder string) error {
	if folder == "" {
		return nil
	}
	if len(folder) > 512 || strings.Count(folder, "/") >= maxFolderDepth {
		return badRequest("folder path too long or too deep")
	}
	if err := fileserver.ValidateKey(folder); err != nil {
		return badRequest("folder path is invalid")
	}
	return nil
}

func badRequest(msg string) error {
	return errorsx.BadRequest(errorsx.StatusBadRequest).WithMessage(msg)
}

func tooLarge(limit int64) error {
	return errorsx.New(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE").
		WithMessage(fmt.Sprintf("file exceeds the %d byte limit", limit))
}

// mapRepoErr 把持久化哨兵映射为对外错误。
func mapRepoErr(err error) error {
	if errors.Is(err, repo.ErrNotFound) {
		return errorsx.NotFound(errorsx.StatusNotFound)
	}
	if errors.Is(err, repo.ErrDuplicate) {
		return errorsx.Conflict(errorsx.StatusConflict)
	}
	return err
}

// storageErr 把存储层哨兵映射为对外错误；HTTP 状态由 ginx 从 errorsx.Code 取。
func storageErr(err error) error {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchBucket" {
		return errorsx.New(http.StatusInternalServerError, errorsx.StatusInternalServer).
			WithMessage("S3 bucket does not exist; create it or set S3_CREATE_BUCKET=true").
			WithCause(err)
	}
	switch {
	case errors.Is(err, fileserver.ErrNotFound):
		return errorsx.NotFound(errorsx.StatusNotFound).WithCause(err)
	case errors.Is(err, fileserver.ErrTooLarge):
		return errorsx.New(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE").WithCause(err)
	case errors.Is(err, fileserver.ErrPermission):
		return errorsx.Forbidden(errorsx.StatusForbidden).WithCause(err)
	case errors.Is(err, fileserver.ErrThrottled):
		return errorsx.TooManyRequests(errorsx.StatusTooManyRequests).WithCause(err)
	case errors.Is(err, fileserver.ErrConflict):
		return errorsx.Conflict(errorsx.StatusConflict).WithCause(err)
	case errors.Is(err, fileserver.ErrInvalidKey), errors.Is(err, fileserver.ErrInvalidArgument),
		errors.Is(err, fileserver.ErrSizeMismatch), errors.Is(err, fileserver.ErrUnsupported):
		return errorsx.BadRequest(errorsx.StatusBadRequest).WithCause(err)
	default:
		return errorsx.New(http.StatusInternalServerError, errorsx.StatusInternalServer).
			WithMessage("storage operation failed").WithCause(err)
	}
}
