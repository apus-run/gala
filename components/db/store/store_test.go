package store_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/apus-run/gala/components/db/where"

	ds "github.com/apus-run/gala/components/db/store"
)

// 确保 dataStore 实现了 DataStore 接口.
var _ Store = (*store)(nil)

var (
	once sync.Once
	S    *store
)

// transactionKey 用于在 context.Context 中存储事务上下文的键.
type transactionKey struct{}

// DataStore 定义了 Store 层需要实现的方法.
type Store interface {
	ds.Provider
}

// store 实现了 Store 接口，提供了对 gorm.DB 的访问和事务处理功能.
// store 结构体包含了一个 *gorm.DB 实例，代表了数据库连接.
type store struct {
	db *gorm.DB

	// 可以根据需要添加其他数据库实例
	// fake *gorm.DB
}

func NewStore(db *gorm.DB) *store {
	once.Do(func() {
		S = &store{db}
	})
	return S
}

// DB 根据传入的条件（wheres）对数据库实例进行筛选.
// 如果未传入任何条件，则返回上下文中的数据库实例（事务实例或核心数据库实例）.
func (store *store) DB(ctx context.Context, wheres ...where.Where) *gorm.DB {
	db := store.db
	// 从上下文中提取事务实例
	if tx, ok := ctx.Value(transactionKey{}).(*gorm.DB); ok {
		db = tx
	}

	// 遍历所有传入的条件并逐一叠加到数据库查询对象上
	for _, whr := range wheres {
		db = whr.Where(db)
	}
	return db
}

// TX 返回一个新的事务实例.
// nolint: fatcontext
func (store *store) TX(ctx context.Context, fn func(ctx context.Context) error) error {
	return store.db.WithContext(ctx).Transaction(
		func(tx *gorm.DB) error {
			ctx = context.WithValue(ctx, transactionKey{}, tx)
			return fn(ctx)
		},
	)
}

type TestModel struct {
	ID   uint   `gorm:"primaryKey"`
	Name string `gorm:"size:255"`
}

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	if err := db.AutoMigrate(&TestModel{}); err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}
	return db
}

func TestStore_Create(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewStore(db)
	store := ds.NewStore[TestModel](dataStore)

	ctx := context.Background()
	obj := &TestModel{Name: "Test Name"}

	err := store.Create(ctx, obj)
	assert.NoError(t, err)
	assert.NotZero(t, obj.ID)

	var result TestModel
	err = db.First(&result, obj.ID).Error
	assert.NoError(t, err)
	assert.Equal(t, obj.Name, result.Name)
}

func TestStore_Update(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewStore(db)
	store := ds.NewStore[TestModel](dataStore)

	ctx := context.Background()
	obj := &TestModel{Name: "Old Name"}
	db.Create(obj)

	obj.Name = "New Name"
	err := store.Update(ctx, obj)
	assert.NoError(t, err)

	var result TestModel
	err = db.First(&result, obj.ID).Error
	assert.NoError(t, err)
	assert.Equal(t, "New Name", result.Name)
}

func TestStore_Delete(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewStore(db)
	store := ds.NewStore[TestModel](dataStore)

	ctx := context.Background()
	obj := &TestModel{Name: "To Be Deleted"}
	db.Create(obj)

	err := store.Delete(ctx, where.F("id", obj.ID))
	assert.NoError(t, err)

	var count int64
	db.Model(&TestModel{}).Where("id = ?", obj.ID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestStore_Get(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewStore(db)
	store := ds.NewStore[TestModel](dataStore)

	ctx := context.Background()
	obj := &TestModel{Name: "To Be Retrieved"}
	db.Create(obj)

	result, err := store.Get(ctx, where.F("id", obj.ID))
	assert.NoError(t, err)
	assert.Equal(t, obj.Name, result.Name)
}

func TestStore_List(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewStore(db)
	store := ds.NewStore[TestModel](dataStore)

	ctx := context.Background()
	db.Create(&TestModel{Name: "Item 1"})
	db.Create(&TestModel{Name: "Item 2"})

	count, results, err := store.List(ctx, where.L(10))
	assert.NoError(t, err)
	assert.Equal(t, int64(2), count)
	assert.Len(t, results, 2)
}

func TestStore_Count(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewStore(db)
	store := ds.NewStore[TestModel](dataStore)

	ctx := context.Background()
	db.Create(&TestModel{Name: "Item 1"})
	db.Create(&TestModel{Name: "Item 2"})

	count, err := store.Count(ctx, where.L(10))
	assert.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestStore_Pluck(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewStore(db)
	store := ds.NewStore[TestModel](dataStore)

	ctx := context.Background()
	db.Create(&TestModel{Name: "Item 1"})
	db.Create(&TestModel{Name: "Item 2"})
	values, err := store.Pluck(ctx, "name", where.L(10))
	assert.NoError(t, err)
	assert.Len(t, values, 2)
	assert.Contains(t, values, "Item 1")
	assert.Contains(t, values, "Item 2")
}
