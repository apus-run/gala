# Gala

GALA 一个轻量级的Golang应用搭建脚手架

## 文档

- [快速开始](QUICK_START.md) - 发布和使用指南
- [版本管理](VERSIONING.md) - 版本策略和标签格式
- [使用示例](USAGE_EXAMPLES.md) - 模块使用示例

## 版本管理

### 标签格式

**统一版本**: `v0.6.2`

所有模块共享同一个版本标签。

### 快速发布

```bash
./scripts/release.sh v0.6.3 --yes
```

### 使用依赖

```bash
# 明确指定版本（推荐）
go get github.com/apus-run/gala/components/db@v0.6.2
```

