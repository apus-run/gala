package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jinzhu/copier"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/apus-run/gala/components/db/where"

	genericStore "github.com/apus-run/gala/components/db/store"
)

// ---------------------------------data-------------------------------------------
// 确保 dataStore 实现了 DataStore 接口.
var _ DataStore = (*datastore)(nil)

// transactionKey 用于在 context.Context 中存储事务上下文的键.
type transactionKey struct{}

// DataStore 定义了 Store 层需要实现的方法.
type DataStore interface {
	genericStore.Provider

	User() UserStore
}

// store 实现了 Store 接口，提供了对 gorm.DB 的访问和事务处理功能.
// store 结构体包含了一个 *gorm.DB 实例，代表了数据库连接.
type datastore struct {
	db *gorm.DB

	// 可以根据需要添加其他数据库实例
	// fake *gorm.DB
}

// NewStore 为每个测试返回独立的 datastore。
//
// 之前这里用 sync.Once 复用了第一个测试的 :memory: 库，导致后续测试的
// db.Create 写进自己的库、而 store 读写第一个库，测试之间互相污染。
func NewStore(db *gorm.DB) *datastore {
	return &datastore{db: db}
}

// DB 根据传入的条件（wheres）对数据库实例进行筛选.
// 如果未传入任何条件，则返回上下文中的数据库实例（事务实例或核心数据库实例）.
func (store *datastore) DB(ctx context.Context, wheres ...where.Where) *gorm.DB {
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
func (store *datastore) TX(ctx context.Context, fn func(ctx context.Context) error) error {
	return store.db.WithContext(ctx).Transaction(
		func(tx *gorm.DB) error {
			ctx = context.WithValue(ctx, transactionKey{}, tx)
			return fn(ctx)
		},
	)
}

// User 返回一个实现了 UserStore 接口的实例.
func (store *datastore) User() UserStore {
	return newUserStore(store)
}

// ------------------------------------dao----------------------------------------
// 确保 userStore 实现了 UserStore 接口.
var _ UserStore = (*userStore)(nil)

type UserModel struct {
	ID   int64  `gorm:"primaryKey"`
	Name string `gorm:"size:255"`
}

type UserStore interface {
	Create(ctx context.Context, obj *UserModel) error
	Update(ctx context.Context, obj *UserModel) error
	Delete(ctx context.Context, opts *where.Options) error
	Get(ctx context.Context, opts *where.Options) (*UserModel, error)
	List(ctx context.Context, opts *where.Options) (int64, []*UserModel, error)
	Count(ctx context.Context, opts *where.Options) (count int64, err error)
	Pluck(ctx context.Context, column string, opts *where.Options) (rets []any, err error)

	UserExpansionStore
}

// UserExpansion 定义了用户操作的附加方法.
type UserExpansionStore interface {
	FindByUsername(ctx context.Context, opts *where.Options) (*UserModel, error)
}

type userStore struct {
	store *datastore
	*genericStore.Store[UserModel]
}

func newUserStore(store *datastore) *userStore {
	return &userStore{
		store: store,
		Store: genericStore.NewStore[UserModel](store),
	}
}

// FindByUsername implements [UserStore].
func (u *userStore) FindByUsername(ctx context.Context, opts *where.Options) (*UserModel, error) {
	var user UserModel
	err := u.store.DB(ctx, opts).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// ------------------------------------repo----------------------------------------
type UserRequest struct {
	ID   int64
	Name string
}

type UserResponse struct {
	ID   int64
	Name string
}

type LoginRequest struct {
	Name string
}

type LoginResponse struct {
	Token string
}

var _ UserRepository = (*userRepository)(nil)

type UserRepository interface {
	Create(ctx context.Context, rq *UserRequest) (*UserResponse, error)
	Get(ctx context.Context, rq *UserRequest) (*UserResponse, error)
	Login(ctx context.Context, rq *LoginRequest) (*LoginResponse, error)
}

type userRepository struct {
	store DataStore
}

func NewUserRepository(store DataStore) UserRepository {
	return &userRepository{
		store: store,
	}
}

// Create implements [UserRepository].
func (u *userRepository) Create(ctx context.Context, rq *UserRequest) (*UserResponse, error) {
	var userM UserModel
	_ = copier.Copy(&userM, rq)

	if err := u.store.User().Create(ctx, &userM); err != nil {
		return nil, err
	}

	return &UserResponse{ID: userM.ID, Name: userM.Name}, nil
}

// Get implements [UserRepository].
func (u *userRepository) Get(ctx context.Context, rq *UserRequest) (*UserResponse, error) {
	userM, err := u.store.User().Get(ctx, where.T(ctx))
	if err != nil {
		return nil, err
	}

	return &UserResponse{
		ID:   userM.ID,
		Name: userM.Name,
	}, nil
}

// Login implements [UserRepository].
func (u *userRepository) Login(ctx context.Context, rq *LoginRequest) (*LoginResponse, error) {
	// 获取登录用户的所有信息
	whr := where.F("name", rq.Name)
	_, err := u.store.User().Get(ctx, whr)
	if err != nil {
		return nil, errors.New("User Not Found")
	}

	// 密码加密

	// 生成 Token

	return &LoginResponse{
		Token: "ssss",
	}, nil
}

// ----------------------------------------------------------------------------

// setupTestDB 为每个测试打开一个独立的 SQLite 内存库。
//
// 注意两点：
//   - SQLite 的 ":memory:" 是「每个连接一个库」，连接池换连接就会看不到表，
//     所以用带名字的 shared cache DSN，把库限定在单个测试内；
//   - MaxOpenConns(1) 让同一个库只有一个连接，避免并发写时的 database is locked。
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("Failed to get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.AutoMigrate(&UserModel{}); err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}
	return db
}

func TestStore_Create(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewStore(db)
	store := genericStore.NewStore[UserModel](dataStore)

	ctx := context.Background()
	obj := &UserModel{Name: "Test Name"}

	err := store.Create(ctx, obj)
	assert.NoError(t, err)
	assert.NotZero(t, obj.ID)

	var result UserModel
	err = db.First(&result, obj.ID).Error
	assert.NoError(t, err)
	assert.Equal(t, obj.Name, result.Name)
}

func TestStore_Update(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewStore(db)
	store := genericStore.NewStore[UserModel](dataStore)

	ctx := context.Background()
	obj := &UserModel{Name: "Old Name"}
	db.Create(obj)

	obj.Name = "New Name"
	err := store.Update(ctx, obj)
	assert.NoError(t, err)

	var result UserModel
	err = db.First(&result, obj.ID).Error
	assert.NoError(t, err)
	assert.Equal(t, "New Name", result.Name)
}

func TestStore_Delete(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewStore(db)
	store := genericStore.NewStore[UserModel](dataStore)

	ctx := context.Background()
	obj := &UserModel{Name: "To Be Deleted"}
	db.Create(obj)

	err := store.Delete(ctx, where.F("id", obj.ID))
	assert.NoError(t, err)

	var count int64
	db.Model(&UserModel{}).Where("id = ?", obj.ID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestStore_Get(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewStore(db)
	store := genericStore.NewStore[UserModel](dataStore)

	ctx := context.Background()
	obj := &UserModel{Name: "To Be Retrieved"}
	db.Create(obj)

	result, err := store.Get(ctx, where.F("id", obj.ID))
	assert.NoError(t, err)
	assert.Equal(t, obj.Name, result.Name)
}

func TestStore_List(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewStore(db)
	store := genericStore.NewStore[UserModel](dataStore)

	ctx := context.Background()
	db.Create(&UserModel{Name: "Item 1"})
	db.Create(&UserModel{Name: "Item 2"})

	count, results, err := store.List(ctx, where.L(10))
	assert.NoError(t, err)
	assert.Equal(t, int64(2), count)
	assert.Len(t, results, 2)
}

func TestStore_Count(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewStore(db)
	store := genericStore.NewStore[UserModel](dataStore)

	ctx := context.Background()
	db.Create(&UserModel{Name: "Item 1"})
	db.Create(&UserModel{Name: "Item 2"})

	count, err := store.Count(ctx, where.L(10))
	assert.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestStore_Pluck(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewStore(db)
	store := genericStore.NewStore[UserModel](dataStore)

	ctx := context.Background()
	db.Create(&UserModel{Name: "Item 1"})
	db.Create(&UserModel{Name: "Item 2"})
	values, err := store.Pluck(ctx, "name", where.L(10))
	assert.NoError(t, err)
	assert.Len(t, values, 2)
	assert.Contains(t, values, "Item 1")
	assert.Contains(t, values, "Item 2")
}

// ----------------------------------------------------------------------------

// userIDKey is used to store user ID in context for tenant filtering
type userIDKey struct{}

func TestUserRepository(t *testing.T) {
	db := setupTestDB(t)
	dataStore := NewStore(db)
	userRepo := NewUserRepository(dataStore)

	// 这个 RegisterTenant 应该再 NewServer 开头就初始化好
	where.RegisterTenant("ID", func(ctx context.Context) string {
		if val := ctx.Value(userIDKey{}); val != nil {
			return val.(string)
		}
		return ""
	})

	ctx := context.Background()

	// Test Create
	createReq := &UserRequest{Name: "testuser"}
	userResp, err := userRepo.Create(ctx, createReq)
	assert.NoError(t, err)
	assert.Equal(t, "testuser", userResp.Name)

	// Test Get
	c := context.WithValue(ctx, userIDKey{}, "1")
	userResp, err = userRepo.Get(c, &UserRequest{Name: "testuser"})
	assert.NoError(t, err)
	assert.Equal(t, "testuser", userResp.Name)

	// Test Login
	loginReq := &LoginRequest{Name: "testuser"}
	loginResp, err := userRepo.Login(ctx, loginReq)
	assert.NoError(t, err)
	assert.Equal(t, "ssss", loginResp.Token)
}

func TestStore_ListDefaultOrder(t *testing.T) {
	db := setupTestDB(t)
	store := genericStore.NewStore[UserModel](NewStore(db))

	ctx := context.Background()
	for _, name := range []string{"first", "second", "third"} {
		assert.NoError(t, db.Create(&UserModel{Name: name}).Error)
	}

	count, rows, err := store.List(ctx, where.L(2))
	assert.NoError(t, err)
	assert.Equal(t, int64(3), count, "count must ignore the limit")
	assert.Len(t, rows, 2)
	assert.Equal(t, "third", rows[0].Name)
	assert.Equal(t, "second", rows[1].Name)
}

func TestStore_ListWithSort(t *testing.T) {
	db := setupTestDB(t)
	store := genericStore.NewStore[UserModel](NewStore(db))

	ctx := context.Background()
	for _, name := range []string{"b", "c", "a"} {
		assert.NoError(t, db.Create(&UserModel{Name: name}).Error)
	}

	columns, err := where.Sortable{"name": "name"}.Parse("name")
	assert.NoError(t, err)

	count, rows, err := store.List(ctx, where.P(1, 2).S(columns...))
	assert.NoError(t, err)
	assert.Equal(t, int64(3), count)
	assert.Len(t, rows, 2)
	assert.Equal(t, "a", rows[0].Name)
	assert.Equal(t, "b", rows[1].Name)
}

func TestStore_ListEmpty(t *testing.T) {
	db := setupTestDB(t)
	store := genericStore.NewStore[UserModel](NewStore(db))

	count, rows, err := store.List(context.Background(), where.L(10))
	assert.NoError(t, err)
	assert.Equal(t, int64(0), count)
	assert.NotNil(t, rows, "empty result must be an empty slice, not nil")
	assert.Empty(t, rows)
}

func TestStore_ListPageBeyondEnd(t *testing.T) {
	db := setupTestDB(t)
	store := genericStore.NewStore[UserModel](NewStore(db))

	ctx := context.Background()
	assert.NoError(t, db.Create(&UserModel{Name: "only"}).Error)

	count, rows, err := store.List(ctx, where.P(5, 10))
	assert.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.NotNil(t, rows, "empty page must be an empty slice, not nil")
	assert.Empty(t, rows)
}

func TestStore_ListNilOptions(t *testing.T) {
	db := setupTestDB(t)
	store := genericStore.NewStore[UserModel](NewStore(db))

	ctx := context.Background()
	assert.NoError(t, db.Create(&UserModel{Name: "first"}).Error)
	assert.NoError(t, db.Create(&UserModel{Name: "second"}).Error)

	count, rows, err := store.List(ctx, nil)
	assert.NoError(t, err)
	assert.Equal(t, int64(2), count)
	assert.Len(t, rows, 2)
	assert.Equal(t, "second", rows[0].Name, "default ordering is still id desc")
}

func TestStore_ListWithFilterAndSort(t *testing.T) {
	db := setupTestDB(t)
	store := genericStore.NewStore[UserModel](NewStore(db))

	ctx := context.Background()
	for _, name := range []string{"bob", "alice", "carol"} {
		assert.NoError(t, db.Create(&UserModel{Name: name}).Error)
	}

	columns, err := where.Sortable{"name": "name"}.Parse("name")
	assert.NoError(t, err)

	// 过滤、分页、排序走同一个 Options，计数与查询各应用一次。
	count, rows, err := store.List(ctx, where.F("name", "bob").P(1, 10).S(columns...))
	assert.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.Len(t, rows, 1)
	assert.Equal(t, "bob", rows[0].Name)
}
