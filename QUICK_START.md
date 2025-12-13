# Gala 快速开始指南

## 🚀 发布新版本

### 方法一：使用自动化脚本（推荐）

```bash
# 1. 确保代码已提交
git add .
git commit -m "feat: 添加新功能"

# 2. 运行发布脚本
./scripts/release.sh v0.6.0

# 3. 完成！脚本会自动创建统一标签并推送
```

### 方法二：手动发布

```bash
# 1. 创建统一版本标签
git tag -a v0.6.0 -m "Release v0.6.0"

# 2. 推送标签
git push --tags
```

## 📦 在其他项目中使用

### 添加依赖

```bash
# 添加特定组件
go get github.com/apus-run/gala/components/db@v0.6.0
go get github.com/apus-run/gala/components/cache@v0.6.0
go get github.com/apus-run/gala/components/log@v0.6.0

# 添加工具包
go get github.com/apus-run/gala/pkg/errorsx@v0.6.0
go get github.com/apus-run/gala/pkg/jwtx@v0.6.0
```

### 在 go.mod 中声明

```go
module myproject

go 1.25

require (
    github.com/apus-run/gala/components/db v0.6.0
    github.com/apus-run/gala/components/cache v0.6.0
    github.com/apus-run/gala/pkg/errorsx v0.6.0
)
```

## 🔧 本地开发

### 使用 go.work（仅本地）

```bash
# 1. 初始化工作空间
go work init

# 2. 添加需要的模块
go work use ./components/db
go work use ./components/cache
go work use ./pkg/errorsx

# 3. 同步依赖
go work sync

# 4. 现在可以直接使用本地代码
# 注意：go.work 不应提交到 Git
```

## ✅ 检查已发布版本

```bash
# 查看远程标签
git ls-remote --tags origin

# 验证特定版本可访问
go list -m github.com/apus-run/gala/components/db@v0.6.0
```

## 🆘 常见问题

### Q: 出现伪版本 v0.0.0-xxx？

**A**: 未指定版本号
```bash
# 正确做法：明确指定版本
go get github.com/apus-run/gala/components/db@v0.6.0
```

### Q: 模块未找到？

**A**: 标签未推送或版本不存在
```bash
# 检查标签
git ls-remote --tags origin

# 推送标签
git push --tags
```

### Q: go.work 相关错误？

**A**: `go.work` 只用于本地开发
```bash
# 添加到 .gitignore
echo "go.work" >> .gitignore
echo "go.work.sum" >> .gitignore
```

## 📚 完整文档

- [版本管理策略](VERSIONING.md) - 详细说明版本管理
- [使用示例](USAGE_EXAMPLES.md) - 各模块使用示例
- [自动化脚本](scripts/release.sh) - 查看发布脚本源码

## 🎯 核心原则

1. **统一版本号**: 所有 35 个模块使用相同版本
2. **明确指定版本**: 使用 `@v0.x.x` 而不是 `@latest`
3. **go.work 仅本地**: 本地开发使用，不提交到 Git
4. **自动化发布**: 使用脚本简化发布流程

---

**快速命令**:
```bash
# 发布
./scripts/release.sh v0.6.0

# 使用
go get github.com/apus-run/gala/components/db@v0.6.0
```
