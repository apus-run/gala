# Gala 版本管理

## 概述

Gala 是多模块仓库，包含 35+ 个独立 Go 模块。

## 版本策略

### 标签格式

```
统一标签:  v0.6.2
```

**所有模块共享同一个版本标签**:
- `v0.6.2` - 根模块（整个仓库）
- `components/db/v0.6.2` - 数据库组件
- `pkg/errorsx/v0.6.2` - 错误处理工具

**说明**: 子模块使用统一版本号，通过模块路径区分

### 版本号规则

- **MAJOR**: 破坏性变更
- **MINOR**: 新功能（向后兼容）
- **PATCH**: Bug 修复

## 发布流程

### 自动发布

```bash
./scripts/release.sh v0.6.2 --yes
```

### 手动发布

```bash
# 1. 根模块标签
git tag -a v0.6.2 -m "Release v0.6.2"

# 2. 子模块标签（脚本自动创建）
./scripts/release.sh v0.6.2 --yes
```

## 使用方法

### 添加依赖

```bash
# 明确指定版本（推荐）
go get github.com/apus-run/gala/components/db@v0.6.2
go get github.com/apus-run/gala/pkg/errorsx@v0.6.2
```

### go.mod 声明

```go
require (
    github.com/apus-run/gala/components/db v0.6.2
    github.com/apus-run/gala/pkg/errorsx v0.6.2
)
```

## 常见问题

### Q: 出现伪版本 v0.0.0-xxx？

**A**: 未指定版本号
```bash
# 解决：明确指定版本
go get github.com/apus-run/gala/components/db@v0.6.2
```

### Q: go.work 冲突？

**A**: go.work 只用于本地开发
```bash
# 添加到 .gitignore
echo "go.work" >> .gitignore
```

## 最佳实践

1. **明确指定版本**: 使用 `@v0.x.x`
2. **统一版本**: 所有模块使用相同版本号
3. **使用脚本**: 自动化发布避免错误

---

**快速命令**:
```bash
# 发布
./scripts/release.sh v0.6.2

# 使用
go get github.com/apus-run/gala/components/db@components/db/v0.6.2
```
