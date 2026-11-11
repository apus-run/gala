# Gala Conf v2 规格与开发计划

**0.11.0 · 2026-11-11 · 上一发布候选 v2.0.0-rc.1**

模块路径为 `github.com/apus-run/gala/components/conf/v2`，下文路径均相对此模块。§1–§5 定义目标行为，§6 记录从当前 main 迁移时涉及的调用者和测试，§7 给出实施顺序。本次修订在 0.10.0 上增加显式热更新循环、投影卷恢复、文本绑定与安全诊断。§0、§6–§7 及 §9 保留历史迁移和验证记录。

## 0. 可复核的基线

- 基线的核心入口为 `Open[T](ctx, opts ...Option)`。来源通过 `WithSources` 注册；核心支持追加来源、合并与解析钩子、后台监视、轮询、订阅和关闭。可用 `git show e036ed1:components/conf/v2/conf.go` 查看该版本。
- `sources/file`、`sources/env` 和 `sources/document` 属于核心 Go 模块。`sources/etcd` 与 `sources/consul` 已各有一个 `go.mod`，通过 `replace .../conf/v2 => ../..` 与本地核心联调。两个适配器均已实现单键文档读取和监视。
- 基线上 `bind.go` 使用 mapstructure v2.2.1，对 uint、float 目标报错；`merge.go` 已有来源数据的深拷贝、环检测和深度限制。`internal/document` 已有文件和远端来源共用的根对象检查。
- Go 1.27.0、darwin/arm64 下，核心目录的 `GOWORK=off go test -race -count=1 ./...` 已通过。该命令止于嵌套模块目录。分别进入 etcd、Consul 模块后，两者均可在竞态检测构建下编译，基线均无测试文件。目标接口和 §8 的新增用例须在对应实现提交上验证。
- Apple M4、Go 1.27.0，§8 命令三次基准的中位数：BenchmarkCurrent 为 1.974 ns/op、0 B/op、0 allocs/op；BenchmarkReloadTwoSources 为 4069 ns/op、6488 B/op、76 allocs/op。

## 1. 模块职责与不变量

采用奥卡姆剃刀原则。在满足同一条使用需求和验收条件的实现中，优先选择公开接口更少、状态更少、依赖更少的方案。每条规则由掌握相应数据的模块实现；只有两个实际调用方需要同一规则时，才提取共享实现。

| 模块 | 拥有的状态与行为 | 调用关系 |
|---|---|---|
| `conf` | 有序来源列表、只读默认值、串行写入和已发布快照；完成合并、绑定、业务校验与发布 | 通过 `Source.Load` 读取，发布前调用可选的 `(*T).Validate`；依赖标准库 |
| `sources/file`、`sources/env`、`sources/flagset` | 把单次读取转换成一层配置。文件使用显式 decoder，环境和 flag 按点分路径构造对象 | 隐式满足 `conf.Source`；env 和 flagset 共用内部路径冲突检查 |
| `internal/document` | 空文档、解码失败与非对象根的判定；屏蔽 decoder 原文 | file、etcd、Consul 传入字节和解码函数；只依赖标准库 |
| `sources/etcd`、`sources/consul` | 各自的 Go 模块拥有 SDK 适配、键空间约定、订阅确认和断线恢复 | 客户端由应用创建和关闭；`Load` 提供数据，`Watch` 提供失效信号 |
| `sources/filewatch` | 独立模块借用 file 的文档读取实现，持有 OS 路径并监视其父目录 | `Load` 读取一层，`Watch` 发失效信号，生命周期由 ctx 管理 |
| 应用 | 持有 context，消费变更信号并调用 Reload；按 §5.2 重试失败的调用 | 手动调用、远端信号、文件事件或标准库定时器都走同一 Reload 入口 |

Config 原子发布同版本的强类型值与解析后的配置树。写入者保留解析前的树以便运行时编辑能重新求值跨字段引用。订阅注册与发布同步，通知经容量为 1 的通道交付；业务代码在调用方运行。

| ID | 可观察的不变量 |
|---|---|
| I1 | Open 首次加载失败返回 nil 和错误。失败的 Reload 丢弃自身候选，不发布、不修改已发布的快照。没有其他成功发布者时，调用前后 Current 指针相同；并发时 Current 可以指向另一调用成功发布的快照。 |
| I2 | 成功发布一次完整、只读的快照；读者可跨 Reload 持有旧指针。 |
| I3 | 每批从空对象和 Open 捕获的默认值重建。高优先级键消失后，低层值或默认值重新生效。 |
| I4 | 发布值与来源的 map、slice 独立。Open 复制可绑定的默认引用；忽略字段和私有状态及 Current 返回的值保持只读。 |
| I5 | Reload 串行。等待时取消的调用读取零个来源；发布前再次检查取消。 |
| I6 | 核心产生的诊断只展示固定原因和已知字段。动态键、配置值和控制字符不能通过核心生成的 Path 泄漏。StageValidate 只展示固定原因；业务错误仍可通过 errors.Is/As 检查。来源错误约定见 §3.4。 |
| I7 | Open 与 Config 无后台任务。显式 Run 的监视、定时器及转发 goroutine 绑定到传入的 ctx。 |
| I8 | 每层显式 null 在合并时失败；强类型目标含未知键或类型不符时绑定失败；无 schema 的 Tree 可保存任意字符串键。 |

## 2. 目标公共接口

以下为接口清单。来源包保留现有 `sources/` 前缀，新包 `sources/flagset` 使用同一约定。

```go
// package conf
type Source interface {
    Load(context.Context) (map[string]any, error)
}
type SourceFunc func(context.Context) (map[string]any, error)
func (f SourceFunc) Load(ctx context.Context) (map[string]any, error)

func Optional(src Source) Source
type Watcher interface { Watch(context.Context) (<-chan struct{}, error) }
type RunOptions struct {
    Debounce, RetryInitial, RetryMax time.Duration
    OnError func(error)
}
func Run(ctx context.Context, cfg interface{ Reload(context.Context) error }, opts RunOptions, watchers ...Watcher) error

type Config[T any] struct { /* private */ }
func Open[T any](ctx context.Context, def T, sources ...Source) (*Config[T], error)
func (c *Config[T]) Current() *T
func (c *Config[T]) Reload(ctx context.Context) error

type Tree struct { /* private */ }
type Snapshot[T any] struct { Tree; Value *T; Revision uint64 }
type Change struct { Before, After Tree }
type Options struct {
    Merge func(dst, src map[string]any) error
    Resolve func(map[string]any) error
}
func OpenTree(ctx context.Context, sources ...Source) (*Config[Tree], error)
func OpenWith[T any](ctx context.Context, def T, options Options, sources ...Source) (*Config[T], error)
func (c *Config[T]) Snapshot() Snapshot[T]
func (c *Config[T]) Load(ctx context.Context, sources ...Source) error
func (c *Config[T]) Subscribe(path ...string) (<-chan Change, func())
func (c *Config[T]) SubscribeSnapshot(path ...string) (Snapshot[T], <-chan Change, func())
func (c *Config[T]) Set(ctx context.Context, value any, path ...string) error
func (c *Config[T]) Delete(ctx context.Context, path ...string) error
func (c *Config[T]) MergeAt(ctx context.Context, tree Tree, path ...string) error
func (t Tree) Lookup(path ...string) (any, bool)
func (t Tree) Exists(path ...string) bool
func (t Tree) Keys(path ...string) []string
func (t Tree) Cut(path ...string) (Tree, error)
func (t Tree) Decode(dst any, path ...string) error
func Get[T any](tree Tree, path ...string) (T, error)
func (t Tree) MarshalJSON() ([]byte, error)
func (t Tree) Marshal(encode func(any) ([]byte, error)) ([]byte, error)
func StrictMerge(dst, src map[string]any) error
func Expand(root map[string]any) error
var ErrNotFound error

type Stage string
const (
    StageConfig   Stage = "config"
    StageLoad     Stage = "load"
    StageMerge    Stage = "merge"
    StageResolve  Stage = "resolve"
    StageBind     Stage = "bind"
    StageValidate Stage = "validate"
    StageCanceled Stage = "canceled"
)
type Error struct {
    Stage  Stage
    Source int    // 从 0 计数，未知为 -1
    Path   string // §3.4 的静态路径，可为空
    Err    error
}
func (e *Error) Error() string
func (e *Error) Unwrap() error
```

```go
// package sources/file
func New(path string, dec func([]byte, any) error) *Source
func FS(fsys fs.FS, name string, dec func([]byte, any) error) *Source
func JSON(data []byte, v any) error
func (s *Source) WithMaxBytes(n int64) *Source
type SyntaxError struct { Line, Column int }
var ErrTooLarge error

// package sources/env
func New(bindings map[string]string) *Source
func NewWithLookup(bindings map[string]string, lookup func(string) (string, bool)) *Source
func Scan(environ func() []string, mapping func(name string) (path string, ok bool)) *Source

// package sources/flagset
func New(fs *flag.FlagSet) *Source
```

每个来源包导出自己的 `Source`，指针实现 `Load`。远端接口见 §5。核心 `Config` 由 Open 创建，禁止在首次使用后按值复制。零值 Current 返回 nil，零值 Reload 返回 StageConfig。传入的 context 遵循标准库非 nil 约定。

## 3. 核心事务

### 3.1 加载、取消与发布

1. Open 在读取任何来源前校验目标类型、标签和来源列表。结构体目标至少提供一个来源；OpenTree 可从空对象开始。含类型的 nil（包括 nil SourceFunc）也算无效。复制来源切片，复制 def 的可绑定集合、指针和导出字段，然后同步执行首次 Reload。
2. Reload 获取实例级信号量；等待可被 ctx 取消。获得信号量后重新检查 ctx。按声明顺序在每次 Load 前后检查 ctx。若 ctx 已结束，优先返回 StageCanceled；来源错误链若含取消或超时哨兵，也归为 StageCanceled。失败即停止，后续来源调用数为零。
3. 每个成功的 Load 立即将该层归一化、深拷贝并合并到本批私有对象。成功返回的 nil 根 map 代表空层。来源可以复用只读数据；在核心复制期间，来源或其他调用者并发写同一份 map/slice 会产生数据竞争，来源应返回独占副本或不可变数据。
4. 从私有默认值的浅拷贝开始绑定。缺席字段保留，显式零值覆盖；嵌套结构体值按字段覆盖。出现的 map、slice 和指针总是全新分配、从零值绑定后整体替换，禁止向默认引用写入。失败的部分绑定只影响本批候选。未被覆盖的默认引用可由多个快照只读共享。
5. 若 `*T` 实现 `Validate() error`，对候选调用一次；该方法须确定、只读、无副作用。若校验期间 ctx 结束，优先返回取消。Validate 触发 panic（含嵌入指针为空导致的方法调用失败）时返回 StageValidate，错误链只保留固定原因。通过校验并完成最后一次 ctx 检查后，仅在解析树变化时发布包含 `*T` 与只读树的同一快照，Revision 自 1 递增；有效的来源列表及解析前数据每次提交。取消与最终存储竞争时允许已成功的提交；返回取消错误的调用绝不能提交。Current 只做一次原子读取。

### 3.2 合并与目标类型

- 后层覆盖前层，对象与对象递归合并，其他组合整体替换，列表保持输入顺序。每层先校验，因此后层覆盖同名键不能掩盖前层的 null。
- 接受字符串键 map（含命名字符串键），归一成 `map[string]any`；数组和 slice 归一成 `[]any`。保留字符串、布尔、整数、浮点及 `json.Number` 的标量类型。嵌套 nil、非字符串键、struct、指针、func、chan、complex、uintptr 和 unsafe.Pointer 报 StageMerge。
- 来源值按当前祖先链检查 map/slice 的环。共享但无环的子树按每次出现分别复制并计数。根深度为 0，进入对象成员或列表元素加 1；深度 128 可用，129 报 StageMerge。合并阶段仅报告来源序号和固定原因，Path 为空。
- 每次 Open 或 Reload 的合并阶段共用 **65,536 个展开节点**的额度。每个非 nil 层的根、每个对象成员值、每个列表元素各计一次；容器本身占一个节点，空容器也计数，map 键与 interface 解包不另计。nil 根表示空层，计零。即使被后层覆盖也计入本批，下次 Reload 重新计数。复制容器前先为其直接子节点预留额度，超限即在分配该容器前返回 StageMerge，原因固定为 `expanded node limit exceeded`。已预留的子节点进入遍历时只扣其后代额度，避免重复扣减。该上限约束合并阶段的展开量，来源读取和文档解码发生在计数前。
- 强类型配置的 T 为结构体。字段支持字符串、布尔、整数（不含 uintptr）、浮点、time.Duration、结构体、定长数组、slice、字符串键 map 和一层指针。数组长度须匹配；导出的嵌入字段采用字段名或标签作为嵌套键。支持 map[string]any，裸 interface 字段、非字符串键 map 和连续指针仍无绑定规则。实现 encoding.TextUnmarshaler 的具体类型可从字符串绑定，返回错误或 panic 时只报告固定绑定原因；无可绑定字段的普通结构体拒绝绑定。
- Open 的类型检查跟随可绑定字段、指针目标和容器元素。遇到当前祖先链中的类型，返回 StageConfig 和闭环字段的静态路径；检查通过的类型可在本次检查内复用。例：`Node.Next *Node`、`Node.Children []Node`、经 map 形成的相互递归均拒绝，两个兄弟字段复用同一结构体有效。忽略字段不参与遍历。此检查独立于来源**值**的深度与环检测。
- 未导出字段和 `conf:"-"` 忽略。无标签或空标签取 Go 字段名，`conf:"name"` 按标签精确匹配。标签含逗号、控制字符或同级有效名重复均报 StageConfig；类型检查报错时只使用安全的静态字段。每个非忽略的键都必须匹配，区分大小写。

### 3.3 标量转换

先识别 `json.Number` 和 `time.Duration`，再按标量 kind 转换。十进制整数字符串允许可选正负号和至少一位 ASCII 数字，十进制浮点文本允许小数点和十进制指数。均拒绝空白、下划线、进制前缀、NaN 和 Inf。`json.Number` 自定义来源也可构造，须先验证 JSON 数字语法。

| 目标 | 接受与结果 | 失败原因 |
|---|---|---|
| string | 仅字符串；json.Number 必须走数值分支 | `string required; quote string values` |
| int、uint 各位宽 | 整数值、十进制整数字符串、整数字面量 json.Number 按目标位宽直接解析。浮点来源需有限、无小数，绝对值严格小于其连续整数边界（float32 为 2²⁴，float64 为 2⁵³），并落在目标范围内 | 语法错误 `type mismatch`，非整数字面量或有小数 `not an exact integer`，超位宽、负数转 uint 或超出浮点安全边界 `value out of range` |
| float32、float64 | 有限数值、十进制浮点文本、json.Number。按目标位宽使用 IEEE 754 最近偶数舍入；文本直接按目标位宽解析。允许正常舍入（含下溢为零） | 非法语法或非有限输入 `type mismatch`，有限输入溢出为无穷 `value out of range` |
| bool | bool 或精确文本 `true`、`false` | `type mismatch` |
| time.Duration | 含单位、可由 time.ParseDuration 解析的字符串 | 裸数字（含文本 `0`）为 `duration needs an explicit unit`，其他非法输入为 `type mismatch` |

整数目标对 `json.Number("1.0")`、`json.Number("1e3")` 报 `not an exact integer`，以保持整数字面量的规则。非数值目标均拒绝 json.Number。结构体和 map 需要对象，slice 需要列表，顶层或嵌套形状不符报 `type mismatch`。

### 3.4 诊断与安全路径

Open、Reload 的失败均返回 `*conf.Error`。Error() 依序展示阶段、可用的来源序号、Path 和下表规定的显示原因；Unwrap() 返回 Err。

| Stage | Err 与可显示的信息 |
|---|---|
| config | 固定的参数、标签或类型错误；nil 来源可带序号，类型错误可带静态字段路径 |
| load | 来源返回的普通错误原样包装，errors.Is/As 可穿透；来源序号 |
| merge | 来源序号、固定原因（null、类型、环、深度、展开节点超限），Path 为空 |
| resolve | 可选解析器失败；保留错误链，来源序号为 -1 |
| bind | §3.3 的固定原因或 `unknown key`、静态字段路径 |
| validate | Error() 的原因固定为 `validation rejected configuration`；Err 保存 Validate 返回的原始错误，供 Unwrap 和 errors.Is/As 使用。Source 为 -1，Path 为空 |
| canceled | context.Canceled 或 context.DeadlineExceeded，可带当前来源序号 |

**Path 的构造。** 只追加已匹配的结构体字段名或标签。仅展示由字母、数字、下划线、连字符组成的非空段，用点连接；遇到不可展示段或进入 map/slice，停止追加，保留已确认的前缀。对未知键直接使用父路径。根下的未知键得到空 Path，`server.<未知键>` 得 `server`，`labels[<动态键>]` 得 `labels`。类型校验中无效标签可退回 Go 字段名。禁止从输入键、值或第三方错误文本重建路径。

**外部文本。** 核心不会调用业务校验错误的 Error() 来构造展示文本。StageLoad 会展示来源返回的错误，来源实现者对该文本负责。核心用来源序号定位失败，构造诊断时不调用来源的 String()。内置来源丢弃 OS、SDK、codec 的原始错误文本，保留固定原因和必要哨兵。文件来源可在自己的错误文本中附上可安全展示的 basename，含控制字符时省略；远端响应中的动态键不得进入错误文本。旧版隐藏来源错误链；这个可见性变化需要随 §6 的调用点迁移一起审查。

### 3.5 动态树、编辑与订阅

- Snapshot 一次读取两个视图。树保存来源合并、解析及临时编辑产生的显式键，零值和缺席可区分；类型化视图再应用结构体默认值。路径为字面字符串段序列，列表索引用规范十进制下标。Keys 返回指定对象的直接键并排序；Lookup、Decode 和自定义编码器取得独立数据。Tree、Snapshot、Change 作为独立日志参数时支持脱敏格式化；显式 Marshal 仍导出完整配置。
- Set/Delete/MergeAt 与 Reload 串行。Set 可创建中间对象，列表索引必须已存在；Delete 删除列表项会使后续下标前移。空路径表示整个对象。编辑无需 I/O，按解析前的对象重新求值、绑定、校验，成功后才发布。成功 Reload 或追加来源的 Load 清除编辑；失败保留编辑、旧指针与原来源列表。
- Subscribe 交付后续变化。需要初始状态时用 SubscribeSnapshot，在注册锁内同时捕获初始快照，交付事件严格晚于该快照。同类型 NaN 在标量、列表和对象中均视为相等；浮点正零和负零不同。每次成功发布比较选定路径在两棵解析后树中的存在性和值。出现、修改、消失均通知，被覆盖的来源变化不通知。缓冲区满时合并为最早未读 Before 与最新 After；幂等取消会关闭通道。
- 自定义 Merge 接收本批独占对象，Resolve 在全部来源合并后运行，两者返回的候选均经过深拷贝与资源限制检查。Source.Load、Merge、Resolve、Validate 禁止同步重入同一实例的写操作；需要后续写入时，由订阅消费者在发布后发起。严格合并按重叠键的具体类型拒绝冲突。文件数值配合 env/flagset 的字符串覆盖应使用默认合并。默认值为不启用解析、采用 §3.2 的递归覆盖。
- Expand 中的引用使用点分路径，`$${` 生成字面量 `${`，冒号后的默认值为字面字符串，只在引用缺席时采用。命名字符串同样支持直接与间接展开，json.Number 保留为数值。完整引用保持被引用值的标量类型，混合文本生成内建 string；引用容器、缺失且无默认值、非法表达式或循环均失败。引用深度按完整依赖链计算（含缓存后缀），最多 128，单次生成文本预算 16 MiB。

## 4. 本地来源

### 4.1 文件与共享文档解码

`file.New` 每次读取指定 OS 路径一次，`file.FS` 通过 `fs.ReadFile` 读取注入的文件系统。读取前后检查 ctx。不按扩展名选择 decoder；JSON 使用 `file.JSON`，YAML/TOML 由调用者传入具备相应签名的 Unmarshal。nil decoder、nil fs.FS 等无效适配器参数在 Load 返回固定错误。

文件与两种远端文档来源共用 `internal/document` 的同一次判定：

1. 纯空白或零字节返回 `empty document`。
2. 定义 `var root any`，调用一次 `dec(raw, &root)`。decoder 须支持 `*any`，并把字符串键对象解为 `map[string]any`。解码器错误统一为 `decode failed`，仅允许 SyntaxError 携带正数行号和字节列号。
3. 成功解码后，root 必须是非 nil 的 `map[string]any`。空对象有效，数组、标量、null 或其他根类型返回 `root is not an object`。嵌套 null 由核心合并阶段拒绝。
4. `file.JSON` 使用 `json.Decoder.UseNumber`，读到一个值后仅允许 JSON 空白。语法错误、截断及多余内容返回首个错误位置。文件不存在和权限错误分别保留 fs.ErrNotExist、fs.ErrPermission，其他 I/O 错误统一为 `read failed`。

WithMaxBytes 返回限长读取的副本，正数上限在解码前执行，零为无限制，负数为无效配置。可选层使用 conf.Optional，只有缺失返回空层，混合错误、权限错误和取消向上传递。嵌入默认配置时，把 `file.FS(embedFS, name, dec)` 放在最低优先级。

**本地文件监视。** 独立模块 `…/conf/v2/sources/filewatch` 提供 `New(path string, dec func([]byte, any) error) *Source`。该实例复用 file.Load 的行为，并提供 `Watch(ctx context.Context) (<-chan struct{}, error)`。构造时固定绝对路径，Watch 同步注册父目录，成功后发出首次失效信号。应用消费信号并全量 Reload，失败时按 §5.2 重试。

按实际文件系统识别目标目录项。大小写不敏感卷上的不同拼写可以指向同一文件；大小写敏感卷上的不同文件独立监视。目标目录项的修改、原子替换、删除和重建都可触发信号，缓冲为 1。同目录其他文件的变更忽略。监视后端报错或父目录被移除时，通知一次并在 ctx 有效期间重试注册，恢复后补发信号。初始目录不存在、无权限或参数无效时同步返回安全错误；ctx 取消会关闭通道并释放监听器。Kubernetes 投影路径的 ..data 替换通过目标文件身份识别，并每秒核对身份以处理合并丢失的目录事件。其余路径的观察范围是目标目录项，写入其他路径上的符号链接目标不在保证范围内。`fsnotify` 只进入 filewatch 模块的依赖图。

### 4.2 环境与命令行

- `env.New` 捕获变量名到点分叶子路径的映射，逐次调用 os.LookupEnv；NewWithLookup 可注入同签名函数，nil 表示 os.LookupEnv。未设置与空字符串均按缺席处理。值始终为字符串，可覆盖可由字符串绑定的叶子。
- `env.Scan` 要求显式提供 environ 和 mapping；每批重新枚举环境，在第一个等号处分割 NAME=value，调用 mapping(NAME) 选择路径。空值也参与映射冲突校验，校验通过后按缺席处理。前缀过滤、双下划线分层等命名约定由 mapping 实现。nil 回调在 Load 报错。
- env 和 flagset 通过一个私有路径构造函数共用规则：先按标识符排序，拒绝空段、空路径、重复叶子和父子冲突，再写入树。显式 env 映射在调用 lookup 前完成校验；Scan 先枚举再校验。错误可带安全变量名或 flag 名，不能带变量值。
- `flagset.New` 捕获已配置的 *flag.FlagSet；nil 或尚未 Parse 时 Load 报错。通过 Visit 仅读取显式设置的 flag，flag 名为点分路径，值为 Value.String()。显式空 flag 字符串覆盖字符串默认值。调用者在并发读取前完成 flag 修改。

## 5. 远端来源与重载

### 5.1 读取接口

保留现有独立模块路径 `…/conf/v2/sources/etcd` 与 `…/conf/v2/sources/consul`，依赖版本以各自的 go.mod 为准。客户端、凭据、连接的创建和关闭归应用。目标包各自提供以下入口：

```go
// package etcd
type Client interface {
    Get(context.Context, string, ...clientv3.OpOption) (*clientv3.GetResponse, error)
}
func New(client Client, key string, dec func([]byte, any) error) *Source
func NewPrefix(client Client, prefix string) *Source
func (s *Source) Watch(ctx context.Context) (<-chan struct{}, error)

// package consul
type Client interface {
    Get(string, *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error)
}
type Lister interface {
    List(string, *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error)
}
func New(client Client, key string, dec func([]byte, any) error) *Source
func NewPrefix(client Lister, prefix string) *Source
func (s *Source) Watch(ctx context.Context) (<-chan struct{}, error)
```

两个适配器在构造时绑定客户端、目标和读取模式。etcd 的两种 Load 都使用 Get；开始 Watch 时，检查同一客户端是否实现 SDK 签名的 `Watch(context.Context, string, ...clientv3.OpOption) clientv3.WatchChan`。Consul 的 New 绑定 Get，NewPrefix 绑定 List；Load 和 Watch 均使用构造时绑定的查询操作。核心的 Source 接口仍只有 Load。

- 单键一次 Get，使用 §4.1 解码。树模式每次 Load 一次前缀范围读（etcd）或 List（Consul），直接使用同一响应中的键和值。每次请求数在 SDK 方法调用处统计。跨来源的读取时刻可以不同。
- Consul 的 Load 在单键 Get 和前缀 List 上均显式设置 `QueryOptions.RequireConsistent=true`，使用调用方 ctx，关闭陈旧读和缓存。这样 Watch 在索引 N 发出的信号触发 Reload 时，选举中的旧 leader 无法用较早状态完成一次成功发布。无法完成一致读时返回读取错误，本次调用丢弃候选，由 §5.2 的应用循环重试。[Consul 读取一致性模式](https://developer.hashicorp.com/consul/api-docs/features/consistency)
- 树模式使用同一个规范前缀执行读取、Watch 和响应键裁剪。`app` 与 `app/` 均查 `app/`，因此不会读入 `apple/`。Consul 允许一个可选前导斜杠，`/app` 同样规范为 `app/`；空串、单独的 `/` 或含空段的路径均拒绝，避免查询整个 KV 空间。etcd 保留前导斜杠的字面含义，`/app` 查 `/app/`，单独的 `/` 只匹配以斜杠开头的键；同样拒绝空串与多余空段。适配器在 NewPrefix 的 Load 或 Watch 启动时完成校验，失败时 SDK 调用数为零。[Consul SDK 的请求路径处理](https://github.com/hashicorp/consul/blob/api/v1.32.1/api/kv.go#L133-L135)
- **Consul 前缀完整性的部署条件。** ACL 关闭时没有权限过滤。启用 ACL 时，部署方须确认整个请求路径支持 Consul 1.11 起提供的 KV 列表过滤标志，代理完整透传 KV 响应元数据（包括过滤头、有效的 X-Consul-Index 和 X-Consul-LastContact），并为请求配置有效的非匿名 token。匿名请求或旧服务端可能静默省略标志；SDK 将缺头和未过滤都映射为 `ResultsFilteredByACLs=false`。List 返回的 QueryMeta 无法区分这两种情况。部署方无法确认这些条件时，应停用前缀来源；此时可使用具备明确读权限的单键文档。构造器和 Load 不额外探测集群版本或凭据。[Consul ACL 过滤响应头](https://developer.hashicorp.com/consul/api-docs/api-structure#results-filtered-by-acls)
- Consul 的每次前缀 List（含阻塞查询）在请求成功后先检查 QueryMeta。元数据缺失视为读取失败；部署条件满足时才可按未过滤响应处理 false。若 `ResultsFilteredByACLs` 为真，即使返回零条，也返回固定权限错误，可由 `errors.Is(err, fs.ErrPermission)` 判断。Load 经核心包装为 StageLoad，本次调用不发布；可选层仅捕获 fs.ErrNotExist。Watch 将该响应按 §5.2 查询失败处理，权限恢复后重新取基线并发信号。[Consul KV 递归读取的权限过滤标志](https://developer.hashicorp.com/consul/api-docs/kv#read-key)
- 完整响应中的键须位于规范前缀下。跳过 Consul 以斜杠结尾且值为空的目录占位键，再裁剪前缀并按斜杠分段。前缀外的键、空段及叶子/父节点冲突报固定错误；其余值即使为空也作为字符串传递。
- 单键缺失或完整的前缀响应中无有效键时，返回可由 errors.Is 判定的 fs.ErrNotExist；其他 SDK 错误保留固定原因，取消保留 context 哨兵。失败调用遵守 I1。来源不保留 SDK 响应缓冲区。

### 5.2 监视与恢复协议

Watch 校验来源已有的客户端和目标。etcd 客户端只有 Get 时，Load 可正常使用；Watch 同步返回 nil 通道和固定错误 `watch unsupported`，不启动 goroutine 或 SDK 请求。无效参数也同步返回 nil 通道和固定错误；有效参数返回容量为 1 的信号通道和 nil 错误，首次建立和后续重建都遵循下表。生产者非阻塞投递，未消费的信号可合并。通道只传达“需要全量读取”。调用者取消 ctx 后，生产者停止请求、退出退避并关闭通道。Watch 不占用 Config 的重载信号量。

| 时刻 | etcd | Consul |
|---|---|---|
| 首次建立 | 每次创建订阅（含重建）都向 SDK 传入 `clientv3.WithRequireLeader(ctx)`，并请求创建通知（WithCreatedNotify）；收到服务端 Created 确认后立即发一个信号。数据事件同样触发信号 | 基线与后续阻塞查询均使用带 ctx 的 RequireConsistent=true；保留各阶段所需的 WaitIndex。以 WaitIndex=0 查询基线。首次成功就发信号，即使键尚不存在；保存响应的索引 |
| 正常运行 | 使用单键或前缀监视。创建确认之后的变更进入同一条订阅 | 正常等待使用至少 1 的索引。若零基线后首次得到正索引，也发一次信号（即使索引恰为 1）；其后按上次成功响应的实际索引比较，相同则继续等待，前进则发信号 |
| 中断与恢复 | 收到 ErrNoLeader、压缩错误或订阅取消，或 SDK 通道关闭时，结束本次订阅；若调用方 ctx 仍有效，可取消地退避再建。每次收到 Created 都发信号 | 请求失败后可取消地退避，下一次以 0 重新取基线；索引回退也触发一次重新取基线。恢复首次成功后补发信号 |
| 异常零索引 | — | 优先处理返回的 0：首次或重置查询后置等待索引为 1；正常等待中遇 0，补发重同步信号，可取消地退避，再以 1 等待。只有正索引回退才发起一次新基线请求 |
| 退出 | ctx 结束后停止重建，不关闭借入客户端 | 阻塞查询与退避都绑定 ctx |

**启动时序。** Open 读到 A 后发生唯一一次写入 B，随后远端静默。若写入在订阅就绪前，首次确认信号触发的 Reload 会读到 B；若写入在就绪后，监视事件会触发 Reload。重连采用同一规则。成立条件是应用消费信号并重试失败的 Reload。分区节点失去 leader 时的订阅行为见 [etcd v3.6.4 Watcher](https://pkg.go.dev/go.etcd.io/etcd/client/v3@v3.6.4#Watcher)。起始 revision 和索引规则见 [etcd Watch API](https://etcd.io/docs/v3.6/learning/api/#watch-streams) 和 [Consul blocking queries](https://developer.hashicorp.com/consul/api-docs/features/blocking)。

**信号消费。** conf.Run 注册一个或多个 Watcher。注册失败会取消已启动的监听。默认在信号静默 75ms 后全量 Reload；失败且设置 OnError 时先报告错误，随后按指数退避重试，即使没有新信号。初始等待上限 1 秒，最大 30 秒，每次取当前上限的一半到上限的随机值。未设置回调时返回首次失败。所有通道关闭后最多再处理一次待重载状态；ctx 取消可中断去抖和退避。手动 Reload 仍可独立调用。

## 6. 从 e036ed1 迁移

v2 的目标接口允许发布前调整。实施时按以下表处理现有调用点、README、示例与测试；仓库外使用者需要同步迁移。旧 `components/conf` 模块不在此次迁移范围内。

| 基线入口或行为 | 目标与迁移动作 | 对应验证 |
|---|---|---|
| Open + Option、WithSources、SourceFunc | 改用 Open(ctx, def, sources...)；保留 SourceFunc | 改写现有示例及调用点，保留空来源、类型化 nil、来源顺序与切片捕获测试 |
| Current、Reload、Validate | 保留全批发布，引入预填默认值 | 保留无其他写入者时的旧指针断言；并发断言按发布者区分，保留取消与快照归属测试 |
| Config.Load 追加来源与层缓存 | Load 追加来源时全量重读，成功后同时提交来源列表与快照 | 新来源失败不会注册，也不会清除本版编辑 |
| WithMerge、WithResolver、StageResolve | 用 OpenWith 的 Options 显式传入 Merge、Resolve；解析失败归 StageResolve | 覆盖冲突、循环引用、错误回滚和回调数据归属 |
| WithWatch、WithPollInterval、Start、Close、Watchable、Watcher、ErrWatchUnsupported、StageWatch | 应用管理监视 context。文件监视调用者用 filewatch 来源订阅文件事件，订阅建立后先触发一次全量重载；轮询使用 time.NewTicker。两者共用 §5.2 的消费循环。中断在途手动调用时取消其 ctx | 在远端适配器及消费示例验证首次补读、重试、退出与重开；保留手动 Reload 取消测试 |
| Subscribe、Observe、Event、Change | 按路径订阅发布后树值，业务自行消费 Change.Before/After | 覆盖缺席与零值、慢消费者、取消和并发发布 |
| StageRead/Decode、SourceError、DecodeError、MappingError、Error.Name 及私有 cause | 删除 Error.Name，调用者按 Error.Source 定位来源。普通来源错误经 StageLoad 包装；StageValidate 沿用基线的固定显示文案与业务错误链。来源错误链与显示文本的变化见 §3.4 | 更新断言，保留伪造 stage 不得改变外层阶段、取消优先、内置来源不泄漏三项 |
| mapstructure 标签和解码、bind_diagnostic 的二次结构遍历 | 全部目标字段改 conf 标签；类型检查与绑定共用字段名规则；移除 mapstructure。增加 uint/float，Open 预检不支持字段；未知键仅报告安全前缀 | 更新 bind、diagnostic、regression 的行为断言，保留旧新差分语料中仍有效的输入 |
| sources/file.New(path)、NewFS、ReadBytes；sources/document.Reader、ReaderFunc、New | 保留 file 包路径；改为显式解码函数和 FS。删除公共文档包装，远端与文件用 internal/document；有自定义读取器的应用用 SourceFunc 组合 | 将根类型矩阵归入 internal/document，保留文件读一次、解码失败传播和取消测试；文件事件与目录重建测试归 filewatch 模块 |
| sources/env；filehttp 的 YAML 示例与旧变量回退 | 保留 env 包路径和既有构造函数，新增 Scan。将同模块 filehttp 示例及 testdata 改为 file.JSON，保留显式环境回退；YAML 兼容性由测试注入 yaml.Unmarshal | 检查示例生产构建依赖与 §8 白名单；保留旧环境变量优先级测试 |
| sources/etcd、sources/consul 的 Client、New(client,key,format)、NewReader、ReadBytes 和 Source.Watch | 保留模块路径；保留两包已有的 Get-only Client，改为显式 decoder。Consul 新增的 NewPrefix 只接收 Lister。etcd 从同一客户端取得监视能力；两包的 src.Watch(ctx) 均返回信号通道及启动错误 | 两个独立模块与核心同步编译；用假 SDK 和真实服务分别验证协议及集成 |

## 7. 实施顺序

每个实施提交都应能在核心和受影响的嵌套模块中独立构建、测试。涉及签名、标签和跨模块接口的切换须与全部本仓调用点在同一提交迁移。

1. **基线。** 以 §0 的提交、测试和基准为参照，按 §6 迁移现有用例。
2. **准备内部实现。** 在不切换公共调用前实现私有严格绑定器、类型预检和输入安全路径；复用 merge 的环和深度检查，在同一复制遍历中加入跨来源共享的节点额度。以现有绑定器和固定语料对照适用的输入，把预填默认值、数值、标签和路径的有意差异列为表驱动测试。把 internal/document 改为普通解码函数，迁移文件和两个远端调用者。过渡包装仅保留到下一步切换。
3. **切换事务和来源。** 一次迁移 Open、标签、源错误、示例和 §6 所列旧契约。切换独立模块中的旧 SourceError 和 Watcher 引用；相应可用的触发实现及其安全性测试一起迁移。删除旧绑定器、诊断二次遍历、注册表、核心后台及 fsnotify 依赖。迁移后 tidy，并分别检查普通构建依赖和核心模块图（§8）。新增 env.Scan、flagset 可在独立提交交付。
4. **远端适配与调度。** 按 §5 统一各适配器的前缀处理，将 Consul 前缀的 ACL 部署条件写入来源包文档，验证 Get/List 的一致读参数、过滤与缺头行为，以及 etcd 的 leader 要求，完成订阅状态机和 §8 验收。两套 SDK 无相互依赖，可分别交付。用 §5.2 的消费示例验证只有一次写入时，首次建立和断线恢复都能发布新值。
5. **文档与发布。** README、包注释及可运行的 ExampleOpen、ExampleConfig_Reload、ExampleOptional 更新至 §2；远端消费示例覆盖 §5.2 的失败重试。发布前，在临时模块代理或本地带 tag 的仓库做消费者构建；核心预发布 tag 如 `components/conf/v2.0.0-rc.1`。独立模块引用可获取的核心版本，发布清单移除本地 replace，消费者在 GOWORK=off 且无 replace 时构建。远端模块 tag 使用其实际目录前缀，例如 `components/conf/v2/sources/etcd/v0.1.0`。实际推送 tag 为单独发布操作。

## 8. 验收

| 范围 | 必须覆盖的输入与断言 |
|---|---|
| Open、并发与事务（I1–I5、I7） | 零值 Config、类型化 nil、首读失败、来源顺序；多层覆盖与键消失；排队取消零读取、读取中取消、校验期间取消；并发持有旧指针；显式 0/空串/false、空列表/对象；更新预填 map/slice/指针后校验失败，旧快照和 def 内容均不变 |
| 并发失败的归属（I1、I5） | 用通道阻塞已持有信号量的调用 A；让 B 在等待信号量时取消。B 返回 StageCanceled，其来源调用数为零。放行 A 成功发布后，两者结束时 Current 为 A 的值，之前持有的快照内容保持原样。只有排除其他成功发布者的失败用例才断言 Current 与调用前指针相同。 |
| 类型、合并与绑定（I4、I8） | 指针/切片/映射递归类型、可复用的兄弟结构体；标签重复及选项、精确匹配；来源 map/slice 自环、共享无环子树、128/129 深度、嵌套 nil、命名 map 和数组归一；高层覆盖前发现低层 null |
| 展开节点上限 | 单层根和叶子恰好 65,536 个时成功，多一个失败；两个单独合法的来源累计超限时错误指向第二层。用 30 层共享二叉子图验证有界失败，后续来源零调用、旧指针不变；随后换成小输入可成功重载。覆盖单个超宽 slice 分配前的拒绝。 |
| 诊断（I6） | 根未知键 Path 为空，嵌套未知键仅带父路径，动态 map 键仅带静态字段前缀。把带换行 canary 分别放入键、值、集合元素、解码器和 SDK 错误，检查核心或内置来源的 Error() 及 Path；用实现 String() 的来源返回普通错误，断言诊断保留来源序号、String() 调用数为零，Is/As 仍能穿透。保留 TestSourceAndValidationTrustBoundary 的业务分支：Validate 返回带换行 canary 的包装错误，Error() 使用固定文案且不含 canary，errors.Is 和 errors.As 均可取得业务原因，旧快照不变 |
| 动态配置 | 查询与解码不别名；并发读到同版本的树与结构体；临时编辑跨失败重载保留、成功重载清除；成功追加来源、失败追加回滚；逐键订阅的新增/修改/删除/取消/合并；解析顺序、循环与大小限制；严格合并和子树导出 |
| 数值 | `json.Number("9007199254740993")` → int64/uint64 精确，→ float64 为 9007199254740992；`16777217` → float32 为 16777216。覆盖 MaxUint64、溢出一位、负转 uint、两种浮点来源转整数的边界、十进制舍入、指数与目标溢出。大整数用 file.JSON 走完整链路，且在 string/bool/Duration 目标上失败 |
| 共享文档解码 | internal/document 独占根类型与错误矩阵。零字节、纯空白 → empty document，decoder 调用数为零；其余输入调用一次。合法数组、null、标量 → root is not an object，decoder 失败 → decode failed，空对象 → 成功。检查错误不含 decoder 原文。 |
| JSON 与文档适配器 | file.JSON 验证数字保留为 json.Number、拒绝第二个值和尾随垃圾。文件及两种远端的文档 Load 各验证一次成功解码、一次解码失败传播及每次读取次数；分别覆盖文件权限、缺失文件或远端键、取消与可选层行为。§8 数值用例保留大整数端到端绑定。 |
| 远端客户端能力 | etcd 用只实现 Get 的客户端验证两种 Load，并断言 Watch 同步返回 nil 通道与 watch unsupported；用同一实现 Get/Watch 的客户端验证监视。Consul 分别用仅有 Get、仅有 List 的客户端验证对应构造函数、Load 和阻塞监视，都只调用绑定的操作。 |
| env、flagset | 映射表拷贝、每批重新取值、空 env 缺席、空 flag 可覆盖、显式前缀过滤；同路径/父子冲突的顺序稳定、非法映射零 lookup；未 Parse 失败，默认 flag 不覆盖，显式 bool/int/Duration 能绑定 |
| 本地文件监视 | 真实临时目录验证首次补读、原地写入、原子替换、删除后保持旧快照、重新创建后恢复、父目录替换后补读、无关文件过滤和取消退出。高优先级来源继续覆盖文件值。 |
| 树模式 | 目录前缀隔离、空段/叶子冲突、占位键、空值、无有效键、取消，多个叶子仍只一次 SDK 调用。对 app、app/、/app、/ 逐一断言两种 SDK 收到的读取与监视前缀、裁剪后的字段或零调用错误。Consul List 分别返回部分键、零键并置 ResultsFilteredByACLs；普通和可选来源均返回 StageLoad，可匹配 ErrPermission、不可匹配 ErrNotExist，旧指针不变。在 ACL 部署条件满足时验证完整空响应可由可选层跳过 |
| Consul SDK 与发布 | 用 httptest 和锁定版本的真实 SDK 对照：部分键响应带过滤头时 SDK 将标志置真，适配器的 Load 返回权限错误；相同响应省略头时 SDK 得到 false，与完整响应不可区分。另测仅移除 X-Consul-LastContact：即使带有过滤头，v1.32.1 也会提前结束元数据解析，标志仍为 false。这两种情况都验证部署限制。对 Load 的 Get/List 及 Watch 基线、阻塞请求检查 HTTP 查询含 consistent、无 stale/cached，阻塞索引仍正确。另用可控 KV 让一次写入触发信号、首次一致读失败；在无其他发布者时旧指针保持，随后无新事件、无周期触发也重试成功并发布新值。 |
| Watch 与调度 | 用可控假 watcher 在 Open 后、首次确认前只写一次，此后静默、无周期触发，最终 Current 为新值；断线期间同样只写一次，重连后读到新值；每次 etcd 订阅都携带 leader 要求，ErrNoLeader 后退避重建。Consul 的过滤响应不能作为成功基线，恢复完整响应后补发信号。覆盖 compaction 重建、同索引超时不触发、索引回退后一次基线读取、连续零索引退避且下一等待索引 ≥1。覆盖信号合并、Reload 首次失败后经过带抖动的非零间隔且无新事件仍重试、取消能中断等待和阻塞请求。使用 testing/synctest 或可控通道 |
| 集成 | integration 标签下分别启动真实 etcd 和 Consul，对单键及前缀执行写入、读取、监视、更新、删除。删除必需文档或最后一个有效叶子后 Reload 失败且旧指针保持；删除部分叶子可成功发布其余配置。对三节点 etcd 隔离当前连接节点与多数派，向多数派仅写入一次；恢复可达性后，无周期触发也能读到该值 |

**真实集群作业。** 从核心模块目录运行 `testdata/ci/integration.sh`。脚本启动临时三节点 etcd 和启用 ACL 的 Consul，在两个独立模块分别运行带 integration 标签的竞态测试。它检查 go test 的 JSON 事件，确认 Document、Prefix、LeaderRecovery（etcd）及 ACLFilteredPrefix（Consul）对应的六个用例全部执行，缺失或跳过均失败。退出后清理临时容器、凭据和网络。[集群验收脚本](../testdata/ci/integration.sh)

**构建与依赖。** 最低工具链为 Go 1.26.11。在工作树中运行 `testdata/ci/check.sh`，脚本在适配器的临时副本中引用本地核心。发布候选在独立模块解析环境中对核心、sources/etcd、sources/consul 和 sources/filewatch 四个目录分别执行以下命令。后续新增独立 Go 模块时同样逐个执行。

```sh
export GOWORK=off
go build ./...
go vet ./...
go test -race -count=1 ./...
go mod tidy -diff
```

核心模块单独运行以下两项检查。第一项覆盖 filehttp 及内部包的普通构建。第二项检查完整模块列表，覆盖测试或间接依赖引入的 etcd、Consul、gRPC 和 protobuf。YAML 仍可作为测试依赖。CI 同时要求 gofmt 无差异。

```sh
export GOWORK=off
deps=$(go list -deps -f '{{if not .Standard}}{{if .Module}}{{if not .Module.Main}}{{.ImportPath}}{{end}}{{else}}{{.ImportPath}}{{end}}{{end}}' ./...) || exit 1
test -z "$deps" || exit 1

modules=$(go list -m -f '{{.Path}}' all) || exit 1
printf '%s\n' "$modules" | awk '
  /^(go\.etcd\.io\/etcd|github\.com\/hashicorp\/consul|google\.golang\.org\/(grpc|protobuf)|github\.com\/(golang|gogo)\/protobuf)(\/|$)/ {
    print "核心模块引入了远端依赖", $0
    forbidden = 1
  }
  END { exit forbidden }
'
```

门槛自测使用临时模块：普通源码只导入标准库，单个 `_test.go` 导入 Consul API。执行 tidy 后，普通构建检查应通过，模块列表检查应打印 `github.com/hashicorp/consul/api` 并以非零状态退出。移除测试导入并 tidy 后两项均通过。

两个 Fuzz 入口分别在 CI 运行 30s，合并前各 60s。FuzzBind 验证有效输入产生的候选和失败时的安全路径；FuzzMerge 验证有序逐层覆盖、输入独立性和错误回滚。基准在同一机器用 `go test -run '^$' -bench '^(BenchmarkCurrent|BenchmarkReloadTwoSources)$' -benchmem -count=3 .` 对照，ns/op 或 allocs/op 劣化超过 30% 时在 PR 解释。逐包对照 go doc、§2 和 §5 的导出面，运行带 Output 断言的 Example；在实际发布的模块包内确认 filehttp 测试数据可读。


## 9. 早期版本实施验证记录

在 Go 1.27.0、darwin/arm64、Apple M4 上完成以下验收。实现和真实集群用例随工作树交付。

| 项目 | 实测结果 |
|---|---|
| 核心、etcd、Consul 三个模块 | 各自 GOWORK=off 构建、vet、竞态测试与 tidy -diff 通过；核心普通构建只依赖标准库与模块自身 |
| 共享语义 | 与 e036ed1 的 18 项输入比较接受/拒绝及成功绑定的值，全部一致。数值扩展、标签、安全路径和默认值由新契约用例覆盖 |
| 模糊测试 | FuzzBind 与 FuzzMerge 各运行 60 秒通过 |
| 集成 | 三节点 etcd 的单键、前缀及分区恢复通过；启用 ACL 的 Consul 的单键、前缀及受限 token 部分响应拒绝通过 |
| 消费者 | 用临时 Go 模块代理构建三个预发布模块包，独立消费者在禁用工作区、无 replace 的条件下运行成功，并核对 filehttp 的 JSON 测试数据随包交付 |
| 基准（三次中位数） | Current 为 1.846 ns/op、0 B/op、0 allocs/op；双来源 Reload 为 1303 ns/op、1992 B/op、21 allocs/op。基线见 §0 |
