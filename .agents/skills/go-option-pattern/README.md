# go-option-pattern

一个用于其它 Go 库 Option 设计与迁移的可复用 Skill。采用已确认的非泛型 `Option` 接口 + 导出 `OptionFunc`，不再比较并反复切换基础模式。

## 使用

将完整的 `go-option-pattern/` 文件夹放入所用 Agent 支持的 Skill 目录，或让 Agent 直接读取本目录下的 `SKILL.md`。具体安装目录与启用方式由宿主决定；本包不假定某个产品的目录或命令。

保留文件夹结构，使主文件中的相对引用可以解析。只有 `SKILL.md` 也能表达核心规则；完整包额外提供迁移边界、测试矩阵和可运行样例。

建议任务指令：

```text
使用 go-option-pattern Skill，检查并重构当前仓库中的 <目标包路径>。
统一采用非泛型 Option 接口 + 导出 OptionFunc，私有配置字段，独立默认配置，
NewOptions 复用最简 Apply，最终统一校验。
先核对调用方和兼容性，保留既有默认值、零值、追加/合并及错误语义。
不要增加 Apply 的 nil 防御、泛型、反射、Builder 或无需求的抽象。
直接修改代码与测试，交付 diff 摘要、破坏性变更说明和真实验证结果。
未授权破坏公开 API 时，完成安全部分并报告冲突，不静默改变调用契约。
```

批量迁移时，先选择一个代表性包验证规则，再逐包执行；不要以全仓库替换代替逐包语义审查。

## 文件说明

| 文件 | 用途 |
| --- | --- |
| `SKILL.md` | Agent 入口；触发条件、固定设计、执行流程、限制和交付规范 |
| `references/migration.md` | 函数/泛型/接口选项、错误型选项、字段私有化、兼容性迁移 |
| `references/design-notes.md` | 所有权、接口 nil、比较、封装及参考来源 |
| `references/testing.md` | 面向目标库的测试矩阵和运行要求 |
| `references/evaluations.md` | 检查 Agent 是否错误套模板的任务场景 |
| `assets/example/` | 独立 Go 示例，含配置、构造和测试；不是新业务框架 |
| `scripts/verify-example.sh` | 只验证随包示例，不修改目标仓库 |
| `CHECKS.md` | 本次打包时实际执行的检查记录 |

## 验证随包示例

需要 Go 工具链和 POSIX shell。示例 `go.mod` 声明 Go 1.20，实际验证版本见 `CHECKS.md`；没有逐版本执行兼容性测试。示例只依赖标准库。

```sh
cd go-option-pattern
sh scripts/verify-example.sh
# 当前环境支持竞态检测时：
sh scripts/verify-example.sh --race
```

脚本检查 gofmt、运行单元测试和 go vet；`--race` 额外运行竞态检测。不下载 Go 工具链；宿主 Go 版本不足时会失败。只有示例通过，不代表某个尚未迁移的业务库已通过。

## 决策摘要

```go
type Option interface { apply(*Options) }
type OptionFunc func(*Options)
func (f OptionFunc) apply(o *Options) { f(o) }
```

`Apply` 只有循环。默认值只初始化一次。`Validate` 校验最终状态。选项类型统一，业务语义不强行统一。对可变数据明确所有权，但不机械深复制所有依赖。

## 格式

主文件采用 YAML frontmatter（`name`、`description`）加 Markdown；正文与按需参考文件分离。格式依据：[Agent Skills specification](https://agentskills.io/specification)。本包未在每一种 Agent 宿主中执行安装测试。
