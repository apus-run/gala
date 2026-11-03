// Package adapter is only the parent module of the Casbin policy adapters; it
// exports no API | 仅是 Casbin 策略适配器的父模块，不导出任何 API.
//
// # Notes | 说明
//
// The adapters themselves live in the gorm and redis submodules. Import
// gormadapter from github.com/apus-run/gala/components/authz/adapter/gorm, or
// redisadapter from github.com/apus-run/gala/components/authz/adapter/redis.
// 适配器实现在 gorm 与 redis 子模块中：GORM 存储请 import
// github.com/apus-run/gala/components/authz/adapter/gorm，Redis 存储请
// import github.com/apus-run/gala/components/authz/adapter/redis。
package adapter
