# Gala

GALA 一个轻量级的Golang应用搭建脚手架

## 文档

- [快速开始](QUICK_START.md) - 发布和使用指南
- [版本管理](VERSIONING.md) - 版本策略和标签格式
- [使用示例](USAGE_EXAMPLES.md) - 模块使用示例

## 版本管理

### 标签格式

- **根模块**: `v0.6.2`
- **子模块**: `{dir_prefix}/v0.6.2`

### 快速发布

```bash
./scripts/release.sh v0.6.3 --yes
```

### 使用依赖

```bash
go get github.com/apus-run/gala/components/db@components/db/v0.6.2
```

