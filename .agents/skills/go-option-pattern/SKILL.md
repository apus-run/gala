---
name: go-option-pattern
description: >-
  为 Go 库设计、审查或迁移非泛型 Option 模式。用于用户要求统一 Option、将函数或泛型选项改为接口、私有化配置字段、整理 DefaultOptions/NewOptions/Apply/Validate，或重构其它库的配置 API。采用 Option 接口 + 导出 OptionFunc 适配器，保持 Apply 极简、默认值独立、最终统一校验；先审计兼容性，再修改代码和测试。
metadata:
  version: "1.0.0"
  language: "zh-CN"
---

# Go Option 模式：接口 + 函数适配器

将目标 Go 包的构造配置统一为以下约定。目标是实现一致、简单、可测试的配置 API，不是引入配置框架。本 Skill 表达已选定的项目设计，不宣称接口型 Option 在所有库中优于纯函数。

## 使用边界与优先级

- 先阅读目标仓库的 `AGENTS.md`、贡献规范、`go.mod`、相关实现及调用方。仓库约束和用户明确要求优先于示例。
- 默认只修改用户指定的包及其必要调用点，不重构无关模块，不升级 Go 版本或依赖，不移除其它用途的泛型。
- 先保证原库业务语义与已承诺的兼容性，再实现目标形态。遇到真实冲突，完成可安全部分并报告差异；不得宣称已完整迁移。
- 本模式用于构造阶段。不要把运行时热更新、已分配资源或解码 DTO 机械改成构造配置。

## 1. 必须遵守的目标契约

| 元素 | 目标职责 |
| --- | --- |
| `Option` | 接口，只声明私有 `apply(*Options)` |
| `OptionFunc` | 导出的函数适配器；`apply` 只调用函数 |
| `Options` | 配置类型；配置字段私有 |
| `WithXxx(...) Option` | 返回接口；普通选项使用 `OptionFunc` |
| `DefaultOptions() *Options` | 每次创建独立默认配置，是业务默认值唯一来源 |
| `NewOptions(opts ...Option) *Options` | 调用 `DefaultOptions()`，再复用 `Apply(opts...)` |
| `(*Options).Apply(opts ...Option)` | 按序原地应用；不返回值，不重置、不校验、不回滚 |
| `Validate() error` | 有真实校验规则时检查最终配置；不补默认值、不修改状态 |
| 组件构造函数 | 组装配置、完成校验后初始化资源；保留已有错误契约 |

`Validate` 是否导出、是否需要 Getter、是否公开配置快照，按真实调用需要决定。没有校验规则，不生成空的 `Validate`；不要为采用模式而扩张公开 API。

## 2. 最小标准骨架

示例字段与默认值仅用于说明。迁移时替换为原库字段、原有默认值及业务约束。

```go
package client

import "time"

type (
    OptionFunc func(*Options)

    Option interface {
        apply(*Options)
    }

    Options struct {
        timeout time.Duration
        retries int
    }
)

func (f OptionFunc) apply(o *Options) {
    f(o)
}

func DefaultOptions() *Options {
    return &Options{
        timeout: 5 * time.Second,
        retries: 3,
    }
}

func NewOptions(opts ...Option) *Options {
    o := DefaultOptions()
    o.Apply(opts...)
    return o
}

// Apply 按顺序应用选项，不重置默认值、不进行校验。
// 调用方应保证接收者和选项有效；不与同一配置的读写并发执行。
func (o *Options) Apply(opts ...Option) {
    for _, opt := range opts {
        opt.apply(o)
    }
}

func WithTimeout(d time.Duration) Option {
    return OptionFunc(func(o *Options) {
        o.timeout = d
    })
}

func WithRetries(n int) Option {
    return OptionFunc(func(o *Options) {
        o.retries = n
    })
}
```

在 `Apply` 与 `OptionFunc.apply` 中，不增加 nil 判断、静默跳过、显式 panic、recover、反射、锁或错误处理。无效 Option 不是业务配置错误；之后执行的 `Validate` 不能为选项调用兜底。不要承诺所有 nil 接收者调用都会 panic。

## 3. 修改前先建立现状清单

阅读 [迁移说明](references/migration.md)，确认：

- 所有 Option 类型、`WithXxx`、构造入口、应用循环、配置读取点及测试。
- 默认值来源；零值含义；选项顺序；重复配置是覆盖、追加、合并还是有其它约束。
- 是否存在错误型选项、即时解析、资源初始化、副作用、运行时修改。
- 外部字段访问、结构体字面量、序列化/反序列化、反射读取和生成代码。
- Go 最低版本、发布状态、兼容性约束，以及函数选项、切片展开、直接调用等使用方式。

不要只根据类型声明或单个文件进行全库字符串替换。跨多个独立 Go 模块时，分别盘点和验证；根目录的一次测试不代表所有模块已覆盖。

## 4. 按最小差异执行迁移

1. 先补能锁定原有语义的测试，再修改 Option 类型及函数适配器。
2. `WithXxx` 统一返回 `Option`；闭包包装为 `OptionFunc`。具名结构体仅在确有数据或行为需求时使用。
3. 收拢业务默认值至 `DefaultOptions`，保证新实例独立。可保留命名常量，但不创建共享可变默认对象。
4. `NewOptions` 只组合默认值和 `Apply`；目标应用循环只保留 `opt.apply(o)`。
5. 以包边界为准私有化配置字段，并更新必要调用点；不能靠 Getter 自动恢复写入或解码能力。
6. 在组件构造边界校验最终状态，然后初始化资源；按真实所有权处理配置快照。
7. 更新示例、测试、迁移文档和兼容性说明；运行检查并检查 diff。

不要把原本的追加型选项改为覆盖，也不要在应用选项后用 `if field == 0` 覆盖用户明确传入的合法零值。

## 5. 校验与错误契约

`WithXxx` 以组装配置为主，`Apply` 只应用，最终校验只检查最终状态。普通校验不需要新建 `OptionErr` 或 `ApplyErr`。

已有错误型选项必须先分类：纯字段校验、跨字段校验、输入解析、I/O 或资源操作。将其改成无错误接口可能改变失败时机、错误身份和“首错停止”语义。不得丢弃原错误、改成 panic、返回假成功或用隐藏错误字段凑接口。

保留既有 `errors.Is/As` 行为与构造函数签名。若原构造函数不能返回错误，不能悄悄为其新增返回值；必须按获准的 API 变更策略处理。无法保持契约时，记录阻碍，不强行套用。

## 6. 可变数据与生命周期

阅读 [边界说明](references/design-notes.md)，分别检查：

- 默认实例之间是否共享本应独立的 map、slice 或嵌套可变数据。
- `WithXxx` 是否按契约捕获输入快照；同一个选项重复应用是否令多个配置共享可变容器。
- Getter 或配置快照是否把内部可变数据暴露给调用方。
- 组件是否持有仍可被调用方修改的构造对象；导出适配器也可能让调用方保留目标指针。

只复制配置拥有的数据。日志器、连接池、回调等依赖按约定共享，不用反射实现通用深复制，也不复制包含锁的运行中对象。仅有值字段时不引入多余复制层。

`Options` 用于构造，不默认并发安全。接口、私有字段或复制外层结构体均不自动解决竞态。不要为此在 `Apply` 中加锁；已有热更新机制单独保留。

## 7. 不允许增加的抽象

不新增泛型 Option、全局 option 工具包、通用 Builder、`...any` 适配、反射配置、优先级排序、Option 注册中心、元数据协议或通用回滚。

不要求每个 Option 都实现 `String`，不为简单字段强制创建结构体。不比较 Option 的身份，不把 Option 作为去重 map 的 key；验证应用结果。

不要声称私有 `apply` 使接口完全封闭，也不要声称接口必然更快、更慢、可比较或零分配。

## 8. 验收与交付

按 [测试清单](references/testing.md) 增补行为测试。可编译参考在 [assets/example/options.go](assets/example/options.go)，构造快照在 [assets/example/client.go](assets/example/client.go)。参考示例不应整包复制进目标库。

在目标仓库允许的环境内执行：

```sh
# 对实际修改的 Go 文件执行 gofmt。
go test ./...
go vet ./...
# 环境支持时再运行；不支持或失败要单独说明。
go test -race -count=1 ./...
```

沿用原仓库已有 CI、build tags、多模块策略与兼容性检查。不要为了消除既有失败而修改无关代码；区分变更引入的问题与基线问题。

本 Skill 自带示例可独立验证：

```sh
sh scripts/verify-example.sh --race
```

最终交付说明实际修改、保留的业务语义、API 破坏点、逐项检查结果及未覆盖部分。没有执行命令不能写“测试通过”；没有访问外部消费者不能写“全部向后兼容”。不得伪造性能结论。

使用 [评估场景](references/evaluations.md) 检查执行结果是否遵守本 Skill。完整包的文件说明见 [README.md](README.md)。
