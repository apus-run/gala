package registry

import (
	"slices"
	"sync"

	"gorm.io/gorm"
)

var globalRegistry = NewRegistry()

// 定义一个Registry类型用于管理模型注册，Register 与 Migrate 可并发调用。
type Registry struct {
	mu     sync.Mutex
	models []any
}

// NewRegistry 创建并返回一个新的Registry实例
func NewRegistry() *Registry {
	return &Registry{
		models: make([]any, 0),
	}
}

// Register 添加新的模型到Registry
func (r *Registry) Register(model any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models = append(r.models, model)
}

// Migrate 执行所有注册模型的迁移
func (r *Registry) Migrate(db *gorm.DB) error {
	r.mu.Lock()
	models := slices.Clone(r.models)
	r.mu.Unlock()

	for _, model := range models {
		if err := db.AutoMigrate(model); err != nil {
			return err
		}
	}
	return nil
}

func Register(model any) {
	globalRegistry.Register(model)
}

func Migrate(db *gorm.DB) error {
	return globalRegistry.Migrate(db)
}
