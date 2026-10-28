// Package repo 定义 service 依赖的持久化接口，并提供基于 gala db.Provider 的实现。
package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/apus-run/gala/components/db"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/model"
)

// 查询未命中/唯一键冲突的哨兵；service 层把它们映射为对外错误。
var (
	ErrNotFound  = errors.New("repo: not found")
	ErrDuplicate = errors.New("repo: duplicate")
)

// Repository 是文件托管所需的全部持久化操作。
type Repository interface {
	// FileMeta
	FileByHash(ctx context.Context, hash string) (*model.FileMeta, error)
	// CreateFileAndLinkUser 在一个事务里创建 FileMeta、UserFile，并删除 staging 登记。
	// 任一写入失败都会回滚，不会留下无用户的 FileMeta。
	CreateFileAndLinkUser(ctx context.Context, m *model.FileMeta, uf *model.UserFile, stagingKey string) error
	// LinkUserAndIncRef 在一个事务里创建 UserFile 并递增引用计数；
	// 同 (user,hash) 已存在时返回 ErrDuplicate，且不递增。
	LinkUserAndIncRef(ctx context.Context, uf *model.UserFile) error
	// DeleteUserFileAndDecRef 在一个事务里删除 UserFile、递减引用计数。
	// 计数归零时先把 StorageKey 写入 staging_objects 作为待删除 outbox，再删除 FileMeta。
	DeleteUserFileAndDecRef(ctx context.Context, userID uint64, hash string) (storageKey string, remaining int, err error)

	// UserFile
	UserFile(ctx context.Context, userID uint64, hash string) (*model.UserFile, error)
	RenameUserFile(ctx context.Context, userID uint64, hash, name string) error
	// ListUserFiles 返回 userID 在 folder 目录下的文件（"" 为根），按时间倒序分页。
	ListUserFiles(ctx context.Context, userID uint64, folder string, offset, limit int) ([]model.FileListItem, int64, error)
	// ListAllFiles 分页返回全部登记文件（公开列表）。
	ListAllFiles(ctx context.Context, offset, limit int) ([]model.PublicFileItem, int64, error)

	// UserFolder：用户显式创建的文件夹，允许为空。
	CreateFolder(ctx context.Context, f *model.UserFolder) error // 已存在返回 ErrDuplicate
	Folders(ctx context.Context, userID uint64) ([]model.UserFolder, error)
	DeleteFolder(ctx context.Context, userID uint64, path string) error
	// FolderEmpty 报告 folder 是否既没有文件也没有子文件夹。
	FolderEmpty(ctx context.Context, userID uint64, path string) (bool, error)

	// UploadSession
	CreateSession(ctx context.Context, s *model.UploadSession) error
	Session(ctx context.Context, id string) (*model.UploadSession, error)
	SetSessionBit(ctx context.Context, id string, index int) error
	DeleteSession(ctx context.Context, id string) error
	ExpiredSessions(ctx context.Context, before time.Time) ([]*model.UploadSession, error)

	// StagingObject
	TrackStaging(ctx context.Context, key string) error
	DropStaging(ctx context.Context, key string) error
	ExpiredStaging(ctx context.Context, before time.Time) ([]string, error)
}

var _ Repository = (*FileRepo)(nil)

// FileRepo 基于 db.Provider 实现 Repository。
// sqlite 是单写者，读改写事务天然串行；换 MySQL 时需为
// DeleteUserFileAndDecRef、SetSessionBit 的读改写加 SELECT ... FOR UPDATE。
type FileRepo struct {
	db db.Provider
}

func New(p db.Provider) *FileRepo { return &FileRepo{db: p} }

// translate 把 GORM 错误映射为包内哨兵。
func translate(err error) error {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return ErrNotFound
	case errors.Is(err, gorm.ErrDuplicatedKey):
		return ErrDuplicate
	default:
		return err
	}
}

func (r *FileRepo) CreateFileAndLinkUser(ctx context.Context, m *model.FileMeta, uf *model.UserFile, stagingKey string) error {
	err := r.db.TX(ctx, func(ctx context.Context) error {
		tx := r.db.DB(ctx)
		if err := tx.Create(m).Error; err != nil {
			return err
		}
		if err := tx.Create(uf).Error; err != nil {
			return err
		}
		return tx.Delete(&model.StagingObject{}, "storage_key = ?", stagingKey).Error
	})
	return translate(err)
}

func (r *FileRepo) LinkUserAndIncRef(ctx context.Context, uf *model.UserFile) error {
	err := r.db.TX(ctx, func(ctx context.Context) error {
		tx := r.db.DB(ctx)
		if err := tx.Create(uf).Error; err != nil {
			return err
		}
		res := tx.Model(&model.FileMeta{}).Where("file_hash = ?", uf.FileHash).
			Update("ref_count", gorm.Expr("ref_count + 1"))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
	return translate(err)
}

func (r *FileRepo) DeleteUserFileAndDecRef(ctx context.Context, userID uint64, hash string) (string, int, error) {
	var (
		key       string
		remaining int
	)
	err := r.db.TX(ctx, func(ctx context.Context) error {
		tx := r.db.DB(ctx)
		res := tx.Where("user_id = ? AND file_hash = ?", userID, hash).Delete(&model.UserFile{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		m := &model.FileMeta{}
		if err := tx.Where("file_hash = ?", hash).First(m).Error; err != nil {
			return translate(err)
		}
		if m.RefCount > 1 {
			if err := tx.Model(&model.FileMeta{}).Where("id = ?", m.ID).
				Update("ref_count", gorm.Expr("ref_count - 1")).Error; err != nil {
				return err
			}
			remaining = m.RefCount - 1
			return nil
		}
		key = m.StorageKey
		// 先登记待删除 outbox，再删除 FileMeta；物理删除失败时 GC 仍能找到它。
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&model.StagingObject{StorageKey: key}).Error; err != nil {
			return err
		}
		return tx.Delete(m).Error
	})
	if err != nil {
		return "", 0, translate(err)
	}
	return key, remaining, nil
}

func (r *FileRepo) FileByHash(ctx context.Context, hash string) (*model.FileMeta, error) {
	m := &model.FileMeta{}
	if err := r.db.DB(ctx).Where("file_hash = ?", hash).First(m).Error; err != nil {
		return nil, translate(err)
	}
	return m, nil
}

func (r *FileRepo) UserFile(ctx context.Context, userID uint64, hash string) (*model.UserFile, error) {
	uf := &model.UserFile{}
	if err := r.db.DB(ctx).Where("user_id = ? AND file_hash = ?", userID, hash).First(uf).Error; err != nil {
		return nil, translate(err)
	}
	return uf, nil
}

func (r *FileRepo) RenameUserFile(ctx context.Context, userID uint64, hash, name string) error {
	res := r.db.DB(ctx).Model(&model.UserFile{}).
		Where("user_id = ? AND file_hash = ?", userID, hash).
		Update("file_name", name)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *FileRepo) ListUserFiles(ctx context.Context, userID uint64, folder string, offset, limit int) ([]model.FileListItem, int64, error) {
	g := r.db.DB(ctx)
	var total int64
	if err := g.Model(&model.UserFile{}).
		Where("user_id = ? AND folder = ?", userID, folder).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.FileListItem
	err := g.Table("user_files AS uf").
		Select("uf.file_hash, uf.file_name, fm.file_size, uf.created_at").
		Joins("JOIN file_meta AS fm ON fm.file_hash = uf.file_hash").
		Where("uf.user_id = ? AND uf.folder = ?", userID, folder).
		Order("uf.created_at DESC").
		Offset(offset).Limit(limit).
		Scan(&items).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list user files: %w", err)
	}
	return items, total, nil
}

func (r *FileRepo) CreateFolder(ctx context.Context, f *model.UserFolder) error {
	return translate(r.db.DB(ctx).Create(f).Error)
}

func (r *FileRepo) Folders(ctx context.Context, userID uint64) ([]model.UserFolder, error) {
	var out []model.UserFolder
	err := r.db.DB(ctx).Where("user_id = ?", userID).Order("path ASC").Find(&out).Error
	return out, err
}

func (r *FileRepo) DeleteFolder(ctx context.Context, userID uint64, path string) error {
	res := r.db.DB(ctx).Where("user_id = ? AND path = ?", userID, path).
		Delete(&model.UserFolder{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *FileRepo) FolderEmpty(ctx context.Context, userID uint64, path string) (bool, error) {
	g := r.db.DB(ctx)
	prefix := path + "/%"
	var files, folders int64
	// 文件：目录内与更深处的都算占用。
	if err := g.Model(&model.UserFile{}).
		Where("user_id = ? AND (folder = ? OR folder LIKE ?)", userID, path, prefix).
		Count(&files).Error; err != nil {
		return false, err
	}
	// 子文件夹：只用前缀匹配——文件夹自身那一行不算“非空”。
	if err := g.Model(&model.UserFolder{}).
		Where("user_id = ? AND path LIKE ?", userID, prefix).
		Count(&folders).Error; err != nil {
		return false, err
	}
	return files == 0 && folders == 0, nil
}

func (r *FileRepo) ListAllFiles(ctx context.Context, offset, limit int) ([]model.PublicFileItem, int64, error) {
	g := r.db.DB(ctx)
	var total int64
	if err := g.Model(&model.FileMeta{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.PublicFileItem
	err := g.Table("file_meta AS fm").
		Select("fm.file_hash, fm.file_name, fm.file_size, fm.ref_count, fm.created_at, MIN(uf.user_id) AS uploader_id").
		Joins("LEFT JOIN user_files AS uf ON uf.file_hash = fm.file_hash").
		Group("fm.id").
		Order("fm.created_at DESC").
		Offset(offset).Limit(limit).
		Scan(&items).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list all files: %w", err)
	}
	return items, total, nil
}

func (r *FileRepo) CreateSession(ctx context.Context, s *model.UploadSession) error {
	return translate(r.db.DB(ctx).Create(s).Error)
}

func (r *FileRepo) Session(ctx context.Context, id string) (*model.UploadSession, error) {
	s := &model.UploadSession{}
	if err := r.db.DB(ctx).First(s, "id = ?", id).Error; err != nil {
		return nil, translate(err)
	}
	return s, nil
}

func (r *FileRepo) SetSessionBit(ctx context.Context, id string, index int) error {
	return r.db.TX(ctx, func(ctx context.Context) error {
		tx := r.db.DB(ctx)
		s := &model.UploadSession{}
		if err := tx.First(s, "id = ?", id).Error; err != nil {
			return translate(err)
		}
		s.UploadedMask[index/8] |= 1 << (uint(index) % 8)
		return tx.Model(s).Update("uploaded_mask", s.UploadedMask).Error
	})
}

func (r *FileRepo) DeleteSession(ctx context.Context, id string) error {
	return r.db.DB(ctx).Delete(&model.UploadSession{}, "id = ?", id).Error
}

func (r *FileRepo) ExpiredSessions(ctx context.Context, before time.Time) ([]*model.UploadSession, error) {
	var out []*model.UploadSession
	err := r.db.DB(ctx).Where("created_at < ?", before).Limit(100).Find(&out).Error
	return out, err
}

func (r *FileRepo) TrackStaging(ctx context.Context, key string) error {
	return translate(r.db.DB(ctx).Create(&model.StagingObject{StorageKey: key}).Error)
}

func (r *FileRepo) DropStaging(ctx context.Context, key string) error {
	return r.db.DB(ctx).Delete(&model.StagingObject{}, "storage_key = ?", key).Error
}

func (r *FileRepo) ExpiredStaging(ctx context.Context, before time.Time) ([]string, error) {
	var rows []model.StagingObject
	if err := r.db.DB(ctx).Where("created_at < ?", before).Limit(100).Find(&rows).Error; err != nil {
		return nil, err
	}
	keys := make([]string, len(rows))
	for i, row := range rows {
		keys[i] = row.StorageKey
	}
	return keys, nil
}
