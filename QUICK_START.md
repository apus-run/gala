# 快速开始

## 发布新版本

```bash
./scripts/release.sh v0.6.2 --yes
```

## 在其他项目中使用

### 安装依赖

```bash
go get github.com/apus-run/gala/components/db@components/db/v0.6.2
go get github.com/apus-run/gala/pkg/errorsx@pkg/errorsx/v0.6.2
```

### go.mod

```go
require (
    github.com/apus-run/gala/components/db components/db/v0.6.2
    github.com/apus-run/gala/pkg/errorsx pkg/errorsx/v0.6.2
)
```

## 本地开发

### 使用 go.work（仅本地）

```bash
go work init
go work use ./components/db
go work use ./components/cache
go work sync
```

⚠️ go.work 不应提交到 Git

## 验证版本

```bash
# 查看标签
git ls-remote --tags origin

# 验证可访问
go list -m github.com/apus-run/gala/components/db@components/db/v0.6.2
```

## 核心要点

1. **明确指定版本** - 使用 `@v0.x.x`
2. **统一版本号** - 所有模块使用相同版本
3. **go.work 仅本地** - 本地开发使用，不提交

---

**完整文档**: [VERSIONING.md](VERSIONING.md)
