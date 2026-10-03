# 设计边界与参考

以下是执行时需要记住的边界，不是要求为每个边界增加防御分支。

## 1. 接口与适配器

`Option` 保留不同实现形式，`OptionFunc` 使普通字段配置仍能用闭包编写，`WithXxx` 返回接口以隐藏实现。仅声明函数类型不会自动实现接口，需要对应的方法。标准库 `http.HandlerFunc` 也使用函数类型加方法的适配思路。[1][5]

具体类型的可读表示或额外行为确实有用途时，可以这样实现：

```go
type timeoutOption struct {
    value time.Duration
}

func (t timeoutOption) apply(o *Options) {
    o.timeout = t.value
}

func (t timeoutOption) String() string {
    return "WithTimeout(" + t.value.String() + ")"
}
```

不要立即把所有闭包改成结构体。没有 `String()` 的闭包适配器不因装入接口就变得更好调试。性能、内联、分配情况需要对真实调用路径测量，不以接口层数断言优劣。

Zap 源码展示了 `Option` 接口加私有 `optionFunc` 的组合；本 Skill 选择导出 `OptionFunc` 是自己的 API 决策，不照搬其作用于 Logger 的生命周期行为。[4]

## 2. 私有方法不是完全封闭

其它包自己声明的同名私有方法与此接口的方法并不相同；但导出的 `OptionFunc` 已实现接口，外部可以使用它，也可以通过嵌入已有实现获得方法。不要声称选项只能在包内构造。[1][2]

外部组合现有配置能力：

```go
func WithFastFail() client.Option {
    return client.OptionFunc(func(o *client.Options) {
        o.Apply(
            client.WithTimeout(time.Second),
            client.WithRetries(0),
        )
    })
}
```

外部仍不能直接访问私有字段。组合能力不等于允许随意改写内部表示；也不等于这一能力是接口版独有。

## 3. nil 与比较

```go
var f OptionFunc
var opt Option = f
// f == nil 是 true；opt == nil 是 false。
```

接口有动态类型时，不因里面保存 nil 函数就等于 nil。调用适配器最终会调用 nil 函数。接收者为 nil 但没有选项时，简单循环不会执行。以上语言行为不应变成额外防御代码。[1][3]

两个接口值的相等比较，在它们保存相同的不可比较动态类型时会 panic。函数、map、slice 不是可比较类型，因此不能把任意 Option 当作可比较标识，或作为 Option 去重 map 的 key。具名选项结构体是否可比较取决于它的字段。[1]

测试选项应用后的结果，不测试任意 Option 的 `==`。不要通过反射给选项创造身份系统。

## 4. 默认值与输入归属

外层结构体复制不等于引用数据复制；map/slice 等需要按所有权决定复制范围。[1]

以下示例明确选择“调用 WithLabels 时获取快照；每次应用独立替换全部标签”，不是所有库必须采用的 map 语义：

```go
func WithLabels(labels map[string]string) Option {
    snapshot := cloneLabels(labels)
    return OptionFunc(func(o *Options) {
        o.labels = cloneLabels(snapshot)
    })
}

func cloneLabels(src map[string]string) map[string]string {
    if src == nil {
        return nil
    }
    dst := make(map[string]string, len(src))
    for key, value := range src {
        dst[key] = value
    }
    return dst
}
```

这里 helper 中的 nil 判断保留输入的 nil 语义，不是 Apply 的无意义防御。不要把“Apply 极简”误读为全库禁止条件判断。

第一次复制隔离调用方输入，第二次复制隔离同一个 Option 的多个接收者。仅复制外层 map 不解决嵌套可变值；对 map[string]string 已足够。复制期间调用方不得并发写输入。

对于 slice，需要隔离可能被改写的底层数组；追加型选项也需检查是否复用了调用方数组及容量。不要让某次扩容的偶然行为成为所有权保证。

## 5. 构造对象与运行时对象

导出 OptionFunc 后，调用方能够在闭包中保存收到的 `*Options`。要承诺组件构造后配置稳定，应把必要字段复制或提取到组件私有状态，而不是原样共享该指针。返回配置时也要避免通过 Getter 泄露本应私有的可变容器。

仅值字段的配置按值保存即可；包含自有容器时进行相应复制。共享服务依赖不需要通用深复制。不要复制含锁或原子状态的运行中对象来冒充配置快照。

示例中的 `Client.Options()` 用于展示快照边界，不代表每个库都应新增该 API。

私有字段不等于不可变或并发安全。`Apply` 仅在配置组装期间使用，构造后的状态按组件原有并发模型处理。竞态检测只覆盖实际执行到的路径，不能证明任意自定义选项都安全。[6]

## 6. Validate 的准确定位

检查真实业务规则：字段取值、必需字段、跨字段关系。它不负责为 nil Option 调用兜底，不补默认值，不规范化修改配置，也不创建外部资源。

例如有效零值与默认值必须区分。若业务约定 `0` 表示不重试，就不能在应用选项后把 `0` 改回默认次数；而超时 `0` 是否非法取决于具体库，不能照搬示例。

一个选项暂时设置非法状态，后续选项覆盖后合法，是最终校验模式允许的组装过程。已有“首错停止”契约不自动等价于这种模式；迁移必须明确这一变化。

## 7. 参考来源

语言事实与格式说明按以下一手文档核对。规则性建议来自本项目已确认的设计，不作为所有 Go 项目的强制规范。

1. [Go language specification](https://go.dev/ref/spec)：方法集、导出规则、赋值、接口及比较。
2. [Go: Keeping Your Modules Compatible](https://go.dev/blog/module-compatibility)：接口扩展及公开 API 兼容性边界。
3. [Go FAQ: nil interface](https://go.dev/doc/faq#nil_error)：接口动态类型与 nil 的区别。
4. [Zap options.go](https://github.com/uber-go/zap/blob/master/options.go)：接口与函数适配器的实际代码示例。
5. [net/http HandlerFunc](https://pkg.go.dev/net/http#HandlerFunc)：函数适配接口的标准库例子。
6. [Go data race detector](https://go.dev/doc/articles/race_detector)：竞态检测及其执行覆盖限制。
7. [Agent Skills specification](https://agentskills.io/specification)：SKILL.md 元数据、目录与按需参考文件。

本包是基于上述语言机制编写的原创示例，不复制第三方库的具体实现或业务策略。
