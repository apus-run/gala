// Package model 定义文件托管服务的持久化模型与列表项。
//
// 设计：物理字节在 fileserver.Storage 中，Key 是不透明标识；数据库负责全部
// 元数据——内容寻址（哈希）、共享（引用计数）、归属（用户关联）与会话（断点续传）。
// 对象存储只当字节层，这使 filestore 与 S3 可以互换。
package model

import "time"

// FileMeta 是一个物理对象的登记项：以内容哈希为身份，被多个用户共享（引用计数）。
// FileName 是首个上传者给的名字，展示名以 UserFile.FileName 为准。
type FileMeta struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement"`
	FileHash    string    `gorm:"column:file_hash;uniqueIndex;size:64;not null"`
	FileName    string    `gorm:"column:file_name;size:255;not null"`
	FileSize    int64     `gorm:"column:file_size;not null"`
	StorageKey  string    `gorm:"column:storage_key;size:1024;uniqueIndex;not null"`
	ContentType string    `gorm:"column:content_type;size:255"`
	RefCount    int       `gorm:"column:ref_count;not null;default:1"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

func (FileMeta) TableName() string { return "file_meta" }

// UserFile 是用户与文件的关联；FileName 是该用户的显示名，可独立重命名。
// Folder 是该用户视角的归属目录（"" 为根，如 "照片/2026"）。
// (UserID, FileHash) 唯一：同一用户重复上传同一内容只是覆盖显示名。
type UserFile struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement"`
	UserID    uint64    `gorm:"column:user_id;uniqueIndex:uk_user_file;index:idx_user_folder,priority:1;not null"`
	FileHash  string    `gorm:"column:file_hash;uniqueIndex:uk_user_file;size:64;not null"`
	FileName  string    `gorm:"column:file_name;size:255;not null"`
	Folder    string    `gorm:"column:folder;size:1024;index:idx_user_folder,priority:2;not null;default:''"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (UserFile) TableName() string { return "user_files" }

// UserFolder 是用户显式创建的文件夹，让空文件夹也能存在。
// 层级编码在 Path 里（"照片/2026"），不单独存父节点；根目录不落表。
type UserFolder struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement"`
	UserID    uint64    `gorm:"column:user_id;uniqueIndex:uk_user_folder;not null"`
	Path      string    `gorm:"column:path;uniqueIndex:uk_user_folder;size:1024;not null"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (UserFolder) TableName() string { return "user_folders" }

// UploadSession 是一次断点续传会话。UploadedMask 按位记录已收到的分块，
// 查询进度与续传都不需要枚举存储端（契约没有 List）。
type UploadSession struct {
	ID           string    `gorm:"column:id;primaryKey;size:32"`
	UserID       uint64    `gorm:"column:user_id;index"`
	FileName     string    `gorm:"column:file_name;size:255;not null"`
	Folder       string    `gorm:"column:folder;size:1024;not null;default:''"`
	ReplaceOf    string    `gorm:"column:replace_of;size:64;not null;default:''"` // 非空表示完成后替换该哈希的文件
	FileSize     int64     `gorm:"column:file_size;not null"`
	ContentType  string    `gorm:"column:content_type;size:255"`
	ChunkSize    int64     `gorm:"column:chunk_size;not null"`
	TotalChunks  int       `gorm:"column:total_chunks;not null"`
	UploadedMask []byte    `gorm:"column:uploaded_mask;type:blob"`
	CreatedAt    time.Time `gorm:"column:created_at;index"`
}

func (UploadSession) TableName() string { return "upload_sessions" }

// Has 报告第 i 块是否已收到。
func (s *UploadSession) Has(i int) bool {
	return s.UploadedMask[i/8]&(1<<(uint(i)%8)) != 0
}

// Count 返回已收到的分块数。
func (s *UploadSession) Count() (n int) {
	for i := 0; i < s.TotalChunks; i++ {
		if s.Has(i) {
			n++
		}
	}
	return n
}

// StagingObject 是待清理对象的登记表：
//   - 上传已写入存储但尚未登记为 FileMeta 的对象（崩溃回收）；
//   - 引用计数归零后、物理对象尚未删除成功的 outbox。
//
// 契约没有 List，GC 只能靠这张表找到待删除对象，因此删除失败时不能先删登记行。
type StagingObject struct {
	StorageKey string    `gorm:"column:storage_key;primaryKey;size:1024"`
	CreatedAt  time.Time `gorm:"column:created_at;index"`
}

func (StagingObject) TableName() string { return "staging_objects" }

// FileListItem 是用户文件列表项（联查结果）。
type FileListItem struct {
	FileHash  string    `gorm:"column:file_hash" json:"file_hash"`
	FileName  string    `gorm:"column:file_name" json:"file_name"`
	FileSize  int64     `gorm:"column:file_size" json:"file_size"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
}

// PublicFileItem 是公开文件列表项：全部登记文件，不区分归属。
type PublicFileItem struct {
	FileHash   string    `gorm:"column:file_hash" json:"file_hash"`
	FileName   string    `gorm:"column:file_name" json:"file_name"`
	FileSize   int64     `gorm:"column:file_size" json:"file_size"`
	RefCount   int       `gorm:"column:ref_count" json:"ref_count"`
	UploaderID uint64    `gorm:"column:uploader_id" json:"uploader_id"`
	CreatedAt  time.Time `gorm:"column:created_at" json:"created_at"`
}
