# Gala 版本管理策略

## 概述

Gala 是一个多模块 Go 项目，包含 35+ 个独立模块。本文档说明了项目的版本管理策略。

## 模块架构

```
gala/
├── go.mod                    # 根模块
├── go.work                   # 开发工作空间 (本地使用，不发布)
├── components/               # 组件模块 (23个)
│   ├── authn/               # 认证组件
│   ├── authz/               # 授权组件
│   ├── backoff/             # 退避策略
│   ├── cache/               # 缓存组件
│   ├── conf/                # 配置管理
│   ├── db/                  # 数据库组件
│   ├── dlock/               # 分布式锁
│   ├── ginx/                # Gin Web 框架扩展
│   ├── grpcx/               # gRPC 扩展
│   ├── i18n/                # 国际化
│   ├── idgen/               # ID 生成器
│   ├── jsoncache/           # JSON 缓存
│   ├── log/                 # 日志组件
│   ├── mongo/               # MongoDB 组件
│   ├── mq/                  # 消息队列
│   ├── rdb/                 # 关系型数据库
│   ├── retry/               # 重试机制
│   ├── rmap/                # 映射工具
│   ├── tinycache/           # 小型缓存
│   └── ws/                  # WebSocket
├── options/                  # 选项模块 (1个)
├── pkg/                      # 通用包 (10个)
│   ├── ctxcache/            # 上下文缓存
│   ├── ctxkey/              # 上下文键
│   ├── errorsx/             # 错误扩展
│   ├── graceful/            # 平滑关闭
│   ├── id/                  # ID 工具
│   ├── jsonx/               # JSON 扩展
│   ├── jwtx/                # JWT 工具
│   ├── lang/                # 语言工具
│   ├── option/              # 选项模式
│   ├── rid/                 # 请求 ID
│   ├── signal/              # 信号处理
│   ├── taskgroup/           # 任务组
│   ├── tls/                 # TLS 配置
│   └── validator/           # 验证器
└── scripts/
    └── release.sh           # 自动化发布脚本
```

## 版本管理策略

### 统一版本号

所有模块使用**统一的版本号**，确保模块间的兼容性。

**版本格式**: `vMAJOR.MINOR.PATCH`

- **MAJOR**: 不兼容的 API 变更
- **MINOR**: 向后兼容的功能新增
- **PATCH**: 向后兼容的问题修正

### 标签命名规范

根模块和子模块使用不同的标签前缀：

```bash
# 根模块标签
v0.5.2

# 子模块标签
components/db/v0.5.2
components/cache/v0.5.2
pkg/errorsx/v0.5.2
```

### 当前版本状态

**最新版本**: v0.5.2

**模块版本列表**:
```
v0.5.2 (2025-12-13)
├── 根模块 (v0.5.2)
├── components/ (23个子模块)
├── options/ (v0.5.2)
└── pkg/ (10个子模块)
```

## 发布流程

### 自动发布 (推荐)

使用自动化脚本发布：

```bash
# 1. 确保代码已提交
git add .
git commit -m "feat: 添加新功能"

# 2. 运行发布脚本
./scripts/release.sh v0.6.0

# 3. 脚本会自动:
#    - 检测所有模块
#    - 为每个模块创建标签
#    - 推送标签到远程仓库
```

### 手动发布

如果需要手动发布：

```bash
# 1. 创建根模块标签
git tag -a v0.6.0 -m "Release v0.6.0"

# 2. 创建子模块标签
git tag -a components/db/v0.6.0 -m "Release components/db v0.6.0"
git tag -a components/cache/v0.6.0 -m "Release components/cache v0.6.0"
# ... 为所有模块创建标签

# 3. 推送所有标签
git push --tags
```

## 使用指南

### 依赖声明

#### Go 1.21+

```go
require (
    github.com/apus-run/gala/components/db v0.5.2
    github.com/apus-run/gala/pkg/errorsx v0.5.2
)
```

#### 或使用命令

```bash
# 添加特定模块
go get github.com/apus-run/gala/components/db@v0.5.2
go get github.com/apus-run/gala/pkg/errorsx@v0.5.2

# 更新到最新版本
go get github.com/apus-run/gala/components/db@latest
```

### 本地开发

#### 使用 go.work (推荐本地开发)

```bash
# 初始化工作空间 (首次)
go work init
go work use .
go work use ./components/db
go work use ./components/cache
# ... 添加其他需要的模块

# 同步依赖
go work sync
```

**注意**: `go.work` 只用于本地开发，不应提交到 Git。

#### 使用 replace 指令

在 `go.mod` 中添加：

```go
replace github.com/apus-run/gala/components/db => /path/to/gala/components/db
replace github.com/apus-run/gala/pkg/errorsx => /path/to/gala/pkg/errorsx
```

## 最佳实践

### 1. 版本同步

- 所有模块使用相同版本号
- 避免不同模块使用不同版本 (如 v0.5.1 和 v0.5.2)
- 确保模块间兼容性

### 2. 语义化版本

- **主版本 (MAJOR)**: 破坏性变更
- **次版本 (MINOR)**: 新功能 (向后兼容)
- **修订版本 (PATCH)**: Bug 修复

### 3. 发布前检查

```bash
# 检查测试
go test ./...

# 检查代码格式
go fmt ./...

# 检查依赖
go mod tidy
go mod verify

# 检查所有模块构建
for mod in $(find . -name "go.mod" -type f | sed 's|/go.mod||'); do
    (cd $mod && go build ./...)
done
```

### 4. 发布后验证

```bash
# 验证标签存在
git ls-remote --tags origin

# 验证模块可访问
go list -m github.com/apus-run/gala/components/db@v0.5.2
```

## 故障排除

### 问题 1: 伪版本 (v0.0.0-xxx)

**原因**: 未指定版本号或版本号不存在

**解决**:
```bash
go get github.com/apus-run/gala/components/db@v0.5.2
```

### 问题 2: 模块未找到

**原因**: 标签未推送到远程

**解决**:
```bash
git push --tags
```

### 问题 3: go.work 相关错误

**原因**: 发布了 go.work 文件

**解决**:
```bash
# 添加到 .gitignore
echo "go.work" >> .gitignore
echo "go.work.sum" >> .gitignore
```

## 常见问题

**Q: 多久发布一次版本？**

A: 建议根据功能或修复累积情况，通常每 2-4 周发布一次。

**Q: 如何处理破坏性变更？**

A: 升级主版本号 (MAJOR)，并确保所有模块同时升级。

**Q: 能否单独更新某个模块？**

A: 可以，但不推荐。统一版本确保兼容性。

**Q: 如何回滚版本？**

A: 使用 Git 回滚标签并重新推送：
```bash
git tag -d v0.5.2
git push origin :refs/tags/v0.5.2
```

## 参考资源

- [Go Modules 官方文档](https://golang.org/ref/mod)
- [语义化版本规范](https://semver.org/)
- [Go 工作空间文档](https://golang.org/ref/mod#go-work-files)

## 维护者

- 项目: https://github.com/apus-run/gala
- 文档更新: 2025-12-13
