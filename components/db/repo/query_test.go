package repo

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/apus-run/gala/components/db/page"
)

type UserModel struct {
	ID    uint   `gorm:"primaryKey"`
	Name  string `gorm:"uniqueIndex"`
	Email string `gorm:"uniqueIndex"`
}

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

func TestConnectDB(t *testing.T) {
	db := setupTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("Failed to get sql.DB from gorm.DB: %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}
}

// UserRepository defines user-specific repository methods
type UserRepository interface {
	Repository[UserModel]

	// User-specific methods
	FindByEmail(ctx context.Context, email string) (*UserModel, error)
}

// userRepository implements UserRepository
type userRepository struct {
	Repository[UserModel]
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) UserRepository {
	var baseRepo Repository[UserModel]
	baseRepo = NewGormRepository(db, UserModel{}, "users")
	return &userRepository{
		Repository: baseRepo,
		db:         db,
	}
}

func (r *userRepository) FindByEmail(ctx context.Context, email string) (*UserModel, error) {
	result, err := r.Where("email", email).First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

func TestRepoCRUD(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewUserRepository(db)

	ctx := context.Background()
	// Create
	user := &UserModel{Name: "John Doe", Email: "moocss@163.com"}
	err := userRepo.Create(ctx, user)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Read
	fetchedUser, err := userRepo.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("Failed to find user by ID: %v", err)
	}
	if fetchedUser.Name != "John Doe" {
		t.Fatalf("Expected name 'John Doe', got '%s'", fetchedUser.Name)
	}

	// Query
	result, err := userRepo.FindByEmail(ctx, "moocss@163.com")
	if err != nil {
		t.Fatalf("Failed to find user by email: %v", err)
	}
	if result.ID != user.ID {
		t.Fatalf("Expected user ID '%d', got '%d'", user.ID, result.ID)
	}
}

func TestGormPaginate(t *testing.T) {
	db := setupTestDB(t)

	for i := range 25 {
		user := &UserModel{
			Name:  fmt.Sprintf("user-%02d", i),
			Email: fmt.Sprintf("user-%02d@example.com", i),
		}
		if err := db.Create(user).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	userRepo := NewGormRepository(db, UserModel{}, "users")

	// 调用方事先设置的 Limit/Offset 不能影响总数与分页参数。
	meta, rows, err := userRepo.Limit(5).Offset(3).OrderBy("id", "ASC").Paginate(2, 10).Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if meta.Total != 25 || meta.TotalPages != 3 {
		t.Fatalf("meta = %+v, want total=25 totalPages=3", meta)
	}
	if meta.Page != 2 || meta.PerPage != 10 || !meta.HasNext || !meta.HasPrev {
		t.Fatalf("meta = %+v, want page=2 perPage=10 hasNext=true hasPrev=true", meta)
	}
	if len(rows) != 10 {
		t.Fatalf("len(rows) = %d, want 10", len(rows))
	}
	if rows[0].ID != 11 || rows[9].ID != 20 {
		t.Fatalf("rows span ids %d..%d, want 11..20", rows[0].ID, rows[9].ID)
	}

	// 末页不越界，也不误报下一页。
	meta, rows, err = userRepo.Limit(100).Paginate(3, 10).Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(rows) != 5 || meta.HasNext {
		t.Fatalf("len(rows) = %d, hasNext = %v, want 5 and false", len(rows), meta.HasNext)
	}
}

func TestGormPaginateEmpty(t *testing.T) {
	db := setupTestDB(t)
	userRepo := NewGormRepository(db, UserModel{}, "users")

	meta, rows, err := userRepo.Limit(100).Paginate(1, 0).Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rows == nil || len(rows) != 0 {
		t.Fatalf("rows = %#v, want an empty non-nil slice", rows)
	}
	if meta.Total != 0 || meta.TotalPages != 0 || meta.HasNext || meta.HasPrev {
		t.Fatalf("meta = %+v, want a zero-value meta", meta)
	}
	if meta.PerPage != page.DefaultSize {
		t.Fatalf("PerPage = %d, want %d", meta.PerPage, page.DefaultSize)
	}
}

func TestGormPaginateBeyondEnd(t *testing.T) {
	db := setupTestDB(t)
	if err := db.Create(&UserModel{Name: "only", Email: "only@example.com"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	userRepo := NewGormRepository(db, UserModel{}, "users")

	meta, rows, err := userRepo.Limit(100).Paginate(9, 10).Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if meta.Total != 1 || meta.Page != 9 || meta.TotalPages != 1 || meta.HasNext {
		t.Fatalf("meta = %+v, want total=1 page=9 totalPages=1 hasNext=false", meta)
	}
	if rows == nil || len(rows) != 0 {
		t.Fatalf("rows = %#v, want an empty non-nil slice", rows)
	}
}
