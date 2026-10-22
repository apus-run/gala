package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/apus-run/gala/components/db"
	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/model"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/repo"
	"github.com/apus-run/gala/components/fileserver/fileservertest"
)

type failingStore struct {
	*fileservertest.MemStore
	failRemove bool
}

func (s *failingStore) Remove(ctx context.Context, key string) error {
	if s.failRemove {
		return errors.New("injected storage removal failure")
	}
	return s.MemStore.Remove(ctx, key)
}

func newService(t *testing.T, storage fileserver.Storage) (*fileSvc, *chunkSvc, repo.Repository, db.Provider) {
	t.Helper()
	provider, err := db.NewDB(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := provider.DB(t.Context()).AutoMigrate(
		&model.FileMeta{}, &model.UserFile{}, &model.UploadSession{}, &model.StagingObject{},
	); err != nil {
		t.Fatal(err)
	}
	r := repo.New(provider)
	files := NewFileService(r, storage, 1<<20)
	return files, NewChunkService(files, r), r, provider
}

func TestRemoveQueuesFailedPhysicalDelete(t *testing.T) {
	store := &failingStore{MemStore: fileservertest.NewMemStore()}
	files, chunks, r, provider := newService(t, store)
	ctx := t.Context()
	res, err := files.Upload(ctx, 1, "", "a.txt", "text/plain", 3, strings.NewReader("abc"))
	if err != nil {
		t.Fatal(err)
	}
	store.failRemove = true
	if err := files.Remove(ctx, 1, res.FileHash); err != nil {
		t.Fatalf("Remove = %v", err)
	}
	if _, err := r.FileByHash(ctx, res.FileHash); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("FileByHash = %v, want ErrNotFound", err)
	}
	var outbox []model.StagingObject
	if err := provider.DB(ctx).Find(&outbox).Error; err != nil || len(outbox) != 1 {
		t.Fatalf("outbox = %+v, err=%v", outbox, err)
	}
	key := outbox[0].StorageKey
	if _, err := store.Stat(ctx, key); err != nil {
		t.Fatalf("object should still exist before GC: %v", err)
	}
	if err := provider.DB(ctx).Model(&model.StagingObject{}).Where("storage_key = ?", key).
		Update("created_at", time.Now().Add(-48*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	store.failRemove = false
	chunks.GC(ctx)
	if _, err := store.Stat(ctx, key); !errors.Is(err, fileserver.ErrNotFound) {
		t.Fatalf("object survived GC: %v", err)
	}
	var n int64
	if err := provider.DB(ctx).Model(&model.StagingObject{}).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("outbox after GC = %d, err=%v", n, err)
	}
}

func TestDedupCleanupFailureStaysTracked(t *testing.T) {
	store := &failingStore{MemStore: fileservertest.NewMemStore()}
	files, chunks, r, provider := newService(t, store)
	ctx := t.Context()
	if _, err := files.Upload(ctx, 1, "", "a.txt", "text/plain", 3, strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	store.failRemove = true
	res, err := files.Upload(ctx, 2, "", "b.txt", "text/plain", 3, strings.NewReader("abc"))
	if err != nil {
		t.Fatalf("dedup Upload = %v", err)
	}
	if !res.IsDedup {
		t.Fatal("second upload should be dedup")
	}
	if _, err := r.UserFile(ctx, 2, res.FileHash); err != nil {
		t.Fatalf("user2 association: %v", err)
	}
	var outbox []model.StagingObject
	if err := provider.DB(ctx).Find(&outbox).Error; err != nil || len(outbox) != 1 {
		t.Fatalf("outbox = %+v, err=%v", outbox, err)
	}
	key := outbox[0].StorageKey
	if err := provider.DB(ctx).Model(&model.StagingObject{}).Where("storage_key = ?", key).
		Update("created_at", time.Now().Add(-48*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	store.failRemove = false
	chunks.GC(ctx)
	if _, err := store.Stat(ctx, key); !errors.Is(err, fileserver.ErrNotFound) {
		t.Fatalf("staging object survived GC: %v", err)
	}
	if _, err := r.FileByHash(ctx, res.FileHash); err != nil {
		t.Fatalf("live object was damaged: %v", err)
	}
}
