// Package registry collects GORM models and migrates them in one shot |
// registry 收集 GORM 模型并一次性执行迁移.
//
// # Notes | 说明
//
// Register and Migrate may be called concurrently. Migrate runs
// gorm.AutoMigrate for every registered model in registration order and
// stops at the first failure. Both an instance API and package-level
// functions backed by a global registry are provided.
// Register 与 Migrate 可并发调用。Migrate 按注册顺序对每个模型执行
// gorm.AutoMigrate，遇到第一个失败即中止。同时提供实例 API 与基于全局注册表
// 的包级函数。
//
// # Example | 示例
//
//	registry.Register(&User{})
//	err := registry.Migrate(db)
package registry

import (
	"slices"
	"sync"

	"gorm.io/gorm"
)

var globalRegistry = NewRegistry()

// Registry manages model registration; Register and Migrate may be called
// concurrently.
// Registry 管理模型注册，Register 与 Migrate 可并发调用。
type Registry struct {
	mu     sync.Mutex
	models []any
}

// NewRegistry creates and returns a new Registry instance | NewRegistry 创建并返回一个新的 Registry 实例。
func NewRegistry() *Registry {
	return &Registry{
		models: make([]any, 0),
	}
}

// Register adds a new model to the Registry | Register 添加新的模型到 Registry。
func (r *Registry) Register(model any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models = append(r.models, model)
}

// Migrate runs migrations for all registered models | Migrate 执行所有注册模型的迁移。
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

// Register adds a model to the global registry | Register 把模型添加到全局注册表。
func Register(model any) {
	globalRegistry.Register(model)
}

// Migrate runs migrations for all models in the global registry | Migrate 对全局注册表中的所有模型执行迁移。
func Migrate(db *gorm.DB) error {
	return globalRegistry.Migrate(db)
}
