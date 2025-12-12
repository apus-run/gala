package repo

import (
	"context"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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
