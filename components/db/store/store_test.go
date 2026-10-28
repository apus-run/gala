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
	"gorm.io/gorm/clause"

	galadb "github.com/apus-run/gala/components/db"
	"github.com/apus-run/gala/components/db/page"
	"github.com/apus-run/gala/components/db/where"

	genericStore "github.com/apus-run/gala/components/db/store"
)

// ---------------------------------data-------------------------------------------
// 确保 dataStore 实现了 DataStore 接口.
var _ DataStore = (*datastore)(nil)

// DataStore 定义了 Store 层需要实现的方法.
type DataStore interface {
	galadb.Provider

	User() UserStore
}

// datastore 在 db.Provider 之上挂载各业务 store.
type datastore struct {
	galadb.Provider
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
	err := opts.Where(u.store.DB(ctx)).First(&user).Error
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
func setupTestDB(t *testing.T) (*gorm.DB, *datastore) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	provider, err := galadb.NewDB(sqlite.Open(dsn))
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	db, _ := galadb.Unwrap(provider)

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("Failed to get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = provider.Close() })

	if err := db.AutoMigrate(&UserModel{}); err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}
	return db, &datastore{Provider: provider}
}

func TestStore_Create(t *testing.T) {
	db, dataStore := setupTestDB(t)
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
	db, dataStore := setupTestDB(t)
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
	db, dataStore := setupTestDB(t)
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
	db, dataStore := setupTestDB(t)
	store := genericStore.NewStore[UserModel](dataStore)

	ctx := context.Background()
	obj := &UserModel{Name: "To Be Retrieved"}
	db.Create(obj)

	result, err := store.Get(ctx, where.F("id", obj.ID))
	assert.NoError(t, err)
	assert.Equal(t, obj.Name, result.Name)

	_, err = store.Get(ctx, where.F("id", obj.ID+1))
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestStore_List(t *testing.T) {
	db, dataStore := setupTestDB(t)
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
	db, dataStore := setupTestDB(t)
	store := genericStore.NewStore[UserModel](dataStore)

	ctx := context.Background()
	db.Create(&UserModel{Name: "Item 1"})
	db.Create(&UserModel{Name: "Item 2"})

	count, err := store.Count(ctx, where.L(10))
	assert.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestStore_Pluck(t *testing.T) {
	db, dataStore := setupTestDB(t)
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
	_, dataStore := setupTestDB(t)
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
	db, dataStore := setupTestDB(t)
	store := genericStore.NewStore[UserModel](dataStore)

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
	db, dataStore := setupTestDB(t)
	store := genericStore.NewStore[UserModel](dataStore)

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
	_, dataStore := setupTestDB(t)
	store := genericStore.NewStore[UserModel](dataStore)

	count, rows, err := store.List(context.Background(), where.L(10))
	assert.NoError(t, err)
	assert.Equal(t, int64(0), count)
	assert.NotNil(t, rows, "empty result must be an empty slice, not nil")
	assert.Empty(t, rows)
}

func TestStore_ListPageBeyondEnd(t *testing.T) {
	db, dataStore := setupTestDB(t)
	store := genericStore.NewStore[UserModel](dataStore)

	ctx := context.Background()
	assert.NoError(t, db.Create(&UserModel{Name: "only"}).Error)

	count, rows, err := store.List(ctx, where.P(5, 10))
	assert.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.NotNil(t, rows, "empty page must be an empty slice, not nil")
	assert.Empty(t, rows)
}

func TestStore_ListNilOptions(t *testing.T) {
	db, dataStore := setupTestDB(t)
	store := genericStore.NewStore[UserModel](dataStore)

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
	db, dataStore := setupTestDB(t)
	store := genericStore.NewStore[UserModel](dataStore)

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

// Store 在 db.Provider.TX 的 ctx 下自动参与事务。
func TestStore_InTransaction(t *testing.T) {
	db, dataStore := setupTestDB(t)
	store := genericStore.NewStore[UserModel](dataStore)
	ctx := context.Background()

	err := dataStore.TX(ctx, func(ctx context.Context) error {
		if err := store.Create(ctx, &UserModel{Name: "rolled back"}); err != nil {
			return err
		}
		return errors.New("abort")
	})
	assert.Error(t, err)

	var count int64
	db.Model(&UserModel{}).Count(&count)
	assert.Equal(t, int64(0), count)
}

// 表不存在时，每个方法都应把数据库错误原样返回，而不是吞掉。
func TestStore_PropagatesDBErrors(t *testing.T) {
	_, dataStore := setupTestDB(t)
	type Missing struct{ ID int64 }
	store := genericStore.NewStore[Missing](dataStore)
	ctx := context.Background()

	assert.Error(t, store.Create(ctx, &Missing{}))
	assert.Error(t, store.Update(ctx, &Missing{ID: 1}))
	assert.Error(t, store.Delete(ctx, where.F("id", 1)))

	_, err := store.Get(ctx, where.F("id", 1))
	assert.Error(t, err)
	assert.NotErrorIs(t, err, gorm.ErrRecordNotFound)

	_, _, err = store.List(ctx, nil)
	assert.Error(t, err)
	_, err = store.Count(ctx, nil)
	assert.Error(t, err)
	_, err = store.Pluck(ctx, "id", nil)
	assert.Error(t, err)
}

// 先 Count 为正、再取页失败时同样要返回错误：用不存在的排序列触发第二条查询失败。
func TestStore_ListPageQueryError(t *testing.T) {
	db, dataStore := setupTestDB(t)
	store := genericStore.NewStore[UserModel](dataStore)
	assert.NoError(t, db.Create(&UserModel{Name: "a"}).Error)

	_, _, err := store.List(context.Background(), where.NewWhere().S(clause.OrderByColumn{
		Column: clause.Column{Name: "no_such_column"},
	}))
	assert.Error(t, err)
}

// Delete 命中 0 行不是错误。
func TestStore_DeleteNoMatch(t *testing.T) {
	_, dataStore := setupTestDB(t)
	store := genericStore.NewStore[UserModel](dataStore)
	assert.NoError(t, store.Delete(context.Background(), where.F("id", 999)))
}

// 事务内 Count / Pluck / Update / Delete 也参与事务。
func TestStore_InTransactionAllOps(t *testing.T) {
	db, dataStore := setupTestDB(t)
	store := genericStore.NewStore[UserModel](dataStore)
	ctx := context.Background()
	seed := &UserModel{Name: "seed"}
	assert.NoError(t, db.Create(seed).Error)

	_ = dataStore.TX(ctx, func(ctx context.Context) error {
		seed.Name = "changed"
		assert.NoError(t, store.Update(ctx, seed))
		n, err := store.Count(ctx, where.F("name", "changed"))
		assert.NoError(t, err)
		assert.Equal(t, int64(1), n)
		names, err := store.Pluck(ctx, "name", nil)
		assert.NoError(t, err)
		assert.Equal(t, []any{"changed"}, names)
		assert.NoError(t, store.Delete(ctx, where.F("id", seed.ID)))
		return errors.New("rollback")
	})

	got, err := store.Get(ctx, where.F("id", seed.ID))
	assert.NoError(t, err)
	assert.Equal(t, "seed", got.Name)
}

// Count 忽略 opts 里的分页：返回全量匹配数，而不是 LIMIT/OFFSET 之后的行数。
func TestStore_CountIgnoresPaging(t *testing.T) {
	db, dataStore := setupTestDB(t)
	store := genericStore.NewStore[UserModel](dataStore)
	ctx := context.Background()
	for i := range 25 {
		assert.NoError(t, db.Create(&UserModel{Name: fmt.Sprintf("u%02d", i)}).Error)
	}

	for _, opts := range []*where.Options{where.P(2, 10), where.P(3, 10), where.P(9, 10), where.L(5), where.O(20)} {
		n, err := store.Count(ctx, opts)
		assert.NoError(t, err)
		assert.Equal(t, int64(25), n, "Count must return the total regardless of paging: %+v", opts)
	}
	n, err := store.Count(ctx, where.P(1, 10).F("name", "u03"))
	assert.NoError(t, err)
	assert.Equal(t, int64(1), n, "filters still apply, paging does not")
}

// 没指定条数时 List 只取 page.DefaultSize 条，count 仍是全量。
func TestStore_ListDefaultsToPageSize(t *testing.T) {
	db, dataStore := setupTestDB(t)
	store := genericStore.NewStore[UserModel](dataStore)
	ctx := context.Background()
	total := page.DefaultSize + 7
	for i := range total {
		assert.NoError(t, db.Create(&UserModel{Name: fmt.Sprintf("u%03d", i)}).Error)
	}

	for name, opts := range map[string]*where.Options{
		"nil opts":        nil,
		"no limit":        where.NewWhere(),
		"filter only":     where.F("id", 1).C(),
		"limit 0 -> none": where.L(0),
	} {
		count, rows, err := store.List(ctx, opts)
		assert.NoError(t, err, name)
		if name == "filter only" {
			assert.Len(t, rows, 1, name)
			continue
		}
		assert.Equal(t, int64(total), count, name)
		assert.Len(t, rows, page.DefaultSize, name)
	}

	// 显式指定条数时不被默认值覆盖。
	_, rows, err := store.List(ctx, where.L(total))
	assert.NoError(t, err)
	assert.Len(t, rows, total)
	_, rows, err = store.List(ctx, where.P(1, 3))
	assert.NoError(t, err)
	assert.Len(t, rows, 3)
}

// F 的参数不成对时，所有执行点都返回 ErrInvalidFilter，并且不碰数据。
func TestStore_InvalidFilterReturnsError(t *testing.T) {
	db, dataStore := setupTestDB(t)
	store := genericStore.NewStore[UserModel](dataStore)
	ctx := context.Background()
	assert.NoError(t, db.Create(&[]UserModel{{Name: "a"}, {Name: "b"}}).Error)

	bad := func() *where.Options { return where.F("name") }

	assert.NotPanics(t, func() { _ = bad() })
	_, err := store.Get(ctx, bad())
	assert.ErrorIs(t, err, where.ErrInvalidFilter)
	_, _, err = store.List(ctx, bad())
	assert.ErrorIs(t, err, where.ErrInvalidFilter)
	_, err = store.Count(ctx, bad())
	assert.ErrorIs(t, err, where.ErrInvalidFilter)
	_, err = store.Pluck(ctx, "name", bad())
	assert.ErrorIs(t, err, where.ErrInvalidFilter)
	assert.ErrorIs(t, store.Delete(ctx, bad()), where.ErrInvalidFilter)

	var n int64
	db.Model(&UserModel{}).Count(&n)
	assert.Equal(t, int64(2), n, "a rejected Delete must not delete anything")

	// 出错的 Options 不污染 store：下一次正常查询照常工作。
	got, err := store.Get(ctx, where.F("name", "a"))
	assert.NoError(t, err)
	assert.Equal(t, "a", got.Name)
}
