package store_test

import (
	"context"
	"errors"
	"sync"
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

var (
	once sync.Once
	S    *datastore
)

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

func NewStore(db *gorm.DB) *datastore {
	once.Do(func() {
		S = &datastore{db}
	})
	return S
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

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
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
