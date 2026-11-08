# Gala Conf v2｜产品需求与技术规格

**评审稿 0.5 · 2026-11-08 · 目标环境 Go 1.27.2**

模块：`github.com/apus-run/gala/components/conf/v2`，包名 `conf`。本文是拟实现的契约，代码未实现，版本未发布。

## 1. 问题与承诺

部署同一服务时，文件提供基础值，环境变量覆盖少数差异。服务需要知道：启动时配置对不对；重载失败后用哪一份。

**承诺：按声明顺序合并来源。首次加载失败则报错；之后加载失败则保留上一份有效配置。** 业务拿到自己的结构体，不依赖文件名、字符串 Getter 或 Viper。

| 使用者 | 需要 | 成功标准 |
|---|---|---|
| 应用开发者 | 从文件构建配置并处理一次错误 | 只定义结构体、指定来源，无需 Watch 或后台服务 |
| 部署人员 | 用环境变量覆盖少数字段 | 后声明的来源优先；移除覆盖后恢复文件值 |
| 维护者 | 在线重载 | 一次读取不混合两个版本；失败指出阶段与字段，不泄露值 |

**为什么独立成 v2：** 旧 conf 的 `File(name)` 返回 Viper，多个文件是独立的命名配置，不是覆盖层。v2 只保留一种行为：完整重建，校验成功才替换。两份独立配置开两个实例。

## 2. 范围

**做：**

- 独立模块；文件、ENV 两个来源；JSON、YAML（含 `.yml`）。
- `Open` 一次加载；显式 `Reload`；强类型绑定；可选 `Validate`。
- Fake 来源测试；filehttp 迁移样例；依赖与发布验证。

**不做：** 修改旧模块；全局 Get/Set、默认路径、目录扫描；Parser 注册表；通用配置树；可插拔合并策略；磁盘回滚；fsnotify、远端 SDK、轮询、自动重试；TOML（见 §8）。

## 3. 接口

```go
// package conf；拟议契约，非现有代码。
type Source interface {
    Load(context.Context) (map[string]any, error)
}

func Open[T any](ctx context.Context, sources ...Source) (*Config[T], error)
func (c *Config[T]) Current() *T
func (c *Config[T]) Reload(ctx context.Context) error
```

```go
type AppConfig struct {
    Server struct {
        Port int `mapstructure:"port"`
    } `mapstructure:"server"`
}

func (c *AppConfig) Validate() error {
    if c.Server.Port < 1 || c.Server.Port > 65535 {
        return errors.New("server.port: must be in 1..65535")
    }
    return nil
}

cfg, err := conf.Open[AppConfig](ctx,
    file.New("config.yaml"),
    env.New(map[string]string{"SERVER_PORT": "server.port"}),
)
if err != nil { return err }
return serve(cfg.Current().Server.Port)
```

- `T` 只接受结构体值类型。参数无效（无来源、来源为 nil、T 不是结构体）时 `Open` 返回错误。
- `Open` 同步加载一次，不启动 goroutine。无需重载的程序也用它。
- `Validate` 可选，在新建的 `*T` 上调用。库不判断零值是否合法，必填、范围、跨字段约束由业务负责。
- 来源只返回 `map[string]any`。文件来源用 `pkg/encoding` 的 codec 解码，本身不实现格式。数字类型由 codec 决定（YAML 的 `1.10` 已是浮点），核心不还原原文。

## 4. 合并与绑定

| 规则 | 约定 |
|---|---|
| 来源顺序 | 后声明优先。文件路径必须显式给出；文件为空、根不是对象、扩展名无已注册 codec 时报错；不静默回退。扩展名小写，`yml` 视为 `yaml`。 |
| 合并 | map 递归合并；其他值整体替换，列表不追加。每批从空 map 重建，不以 Current 为底稿。 |
| 缺席 | 键不存在即缺席，下层值显现。ENV 未设置与设为空串都视为缺席，不覆盖文件值（与旧版一致，已实测）。显式 `null` 报错。 |
| ENV | 显式“变量名 → 叶子字段路径”，路径以点分段。重复路径、父子路径冲突报错。不按下划线猜字段。 |
| 绑定 | 字段名取 `mapstructure` 标签，无标签取 Go 字段名，精确匹配。未知键报错。 |
| 类型 | 见下表，其余组合报错 |

**类型转换表**（由 `DecodeHook` 实现）：

| 目标 | 接受 | 拒绝 |
|---|---|---|
| 字符串 | 字符串（保留原文） | JSON/YAML 的数字、布尔等标量，提示加引号 |
| 整数（int/int8…int64） | 十进制整数字符串（`"9000"`）；能精确表示的整数（`int`、`int64`）；无小数的浮点数且 `\|v\| < 2^53` | 非数字字符串；小数；超出目标位宽范围（如 int8 的 300）；浮点数 `\|v\| >= 2^53` |
| 布尔 | `true`、`false` 字符串或 bool | 其他字符串，如 `yes`、`1` |
| Duration | 带单位的字符串（`500ms`、`2s`） | 裸数字，包括 `500` 与 `"500"` |

- 整数规则的 2^53 边界针对 JSON 经 `any` 解码的情况：JSON v2 会把大整数解成 float64，`9007199254740993` 实测变为 `9007199254740992`，静默丢精度。YAML v3 保留为 `int`，不受此限。
- ENV 来源的值都是字符串，因此整数、布尔、Duration 必须接受字符串形式（`SERVER_PORT=9000`、`DEBUG=true`、`TIMEOUT=500ms`）。

绑定库固定为 `github.com/go-viper/mapstructure/v2` v2.2.1，并显式设置 `ErrorUnused: true` 与精确的 `MatchName`，不开启弱类型模式。默认配置已实测会：静默忽略未知键；把 `NAME` 匹配到 `name`；拒绝字符串 `"9000"` 绑定到 int（导致 ENV 无法使用）；接受 int8 的 `300` 并得到 44；接受 `8080.5` 并得到 8080；把 Duration 裸数字 `500` 当作 500ns。上表的每一行都由钩子显式判定，钩子必须按目标位宽检查范围，不能只在入口转换后交给默认逻辑。

## 5. 重载与并发

1. `Open` 只有在首次加载成功后才返回实例；失败返回 nil 与错误。
2. 同一实例的加载批次串行执行。用容量为 1 的 channel 作为信号量，等待时同时 `select` `ctx.Done()`。等待中被取消则直接返回，不读取来源。
3. 一个批次：逐一读取来源 → 合并 → 严格绑定到新 `T` → `Validate`。任一步失败则丢弃整批。发布前检查 ctx。
4. 成功后用 `atomic.Pointer[T]` 一次替换。`Current()` 不加锁、不访问来源。一个业务操作读取一次 `Current()`，并沿用得到的指针。
5. 发布的对象只读。库不修改它，也不深拷贝；调用方需要可写数据时自行复制。不防御恶意修改。
6. 全批发布保证的是单进程内的可见性，不是外部系统的事务快照。不会重建连接池、监听端口等资源。进程重启后必须重新加载成功。

## 6. 错误

单一错误类型，供调用方取阶段和来源：

```go
type Error struct {
    Stage  Stage  // read | decode | merge | bind | validate | canceled
    Source int    // 来源序号；无法确定胜出来源时为 -1，不猜
    Name   string // 安全名：文件为 basename，ENV 为变量名
    Path   string // 字段路径，可选
}
```

- `Error()` 只输出阶段、来源序号、安全名、字段路径和固定原因文案。不输出完整路径、配置值、ENV 内容或整份配置。
- codec 与 Source 的原始错误文本不进入 `Error()`。`Unwrap` 只暴露 `fs.ErrNotExist`、`context.Canceled`、`context.DeadlineExceeded`，其余原始错误丢弃。
- 业务 `Validate` 的错误文本由业务保证不含配置值；库把它作为 validate 阶段的原因附上，不做脱敏。
- 库不打印，不引入日志框架。

## 7. 验收

共同夹具：文件含 `server.port: 8080`，ENV `SERVER_PORT=9000`。以下为**待执行验收项**，不是已通过的报告。

| ID | 操作 | 必须观察到 |
|---|---|---|
| C01 | 有 ENV 时文件改为 8081 并 Reload；移除 ENV 再 Reload | 第一次仍为 9000；第二次为 8081。优先级由声明顺序决定，与事件到达顺序无关 |
| C02 | 文件同时改合法字段和端口 `abc`；首次读取失败；已有实例时第二个来源读取失败 | 失败的批次不发布；旧指针内容不变；首次失败时无实例；修正后同一实例可成功 |
| C03 | 请求读取 `Current()`，另一协程反复 Reload 关联字段；旧指针持有者在替换后继续读取 | 每次读取的字段组合属于同一版本；旧指针内容保持不变；`-race` 通过 |
| C04 | 第一次 Reload 被可控 Fake 阻塞；第二个 Reload 在等待期间取消 | 来源读取不并发；被取消的调用不读取来源；旧批次不会晚于新批次发布 |
| C05 | 文件含层级对象、空列表、`0`、`false`；ENV 未设置与设为空串 | 文件中的合法零值保留；ENV 两种情况都不覆盖文件值 |
| C06 | 拼错 `server.prot`；`int8` 输入 300 与 `"300"`；`int` 输入 8080.5；整数输入 `"9000x"`；Duration 输入 `500` 与 `"500"`；布尔输入 `yes`；JSON 浮点数 2^53；嵌入含密钥的畸形输入 | 全部报错；错误文本不含测试密钥；不得静默得到 44、8080、500ns |
| C06a | ENV 字符串 `"9000"`、`"true"`、`"500ms"` 绑定到 int、bool、Duration；文件中的 `8080` 绑定到 int | 全部成功，值正确（ENV 能用是硬性要求） |
| C07 | 临时目录分别加载 `.json`、`.yaml`、`.yml`；缺 codec；缺文件；未知扩展名 | 三种格式得到相同配置；其余明确报错；同一文件单次 Load 只读一次 |
| C08 | filehttp：`S3_BUCKET` 与 `STORAGE_S3_BUCKET` 并存、各自单独存在；`server.mode` 删除前后 | 并存时取新名（已实测）；单独存在时各自生效；删除前报未知键 `server.mode`，删除后各键结果与旧版一致；错误不含 DSN |

**质量门槛：**

- 在 `components/conf/v2` 下执行 `GOWORK=off go test ./...`、`GOWORK=off go test -race ./...`、`GOWORK=off go vet ./...`。仓库 `go.work` 不含 v2，且不把它加入，以免改变其他模块的解析。
- v2 核心依赖不含旧 conf、Viper、fsnotify、远端 SDK。
- 独立消费项目用 `GOWORK=off` 验证导入与 codec 注册，不依赖仓库内 replace。
- 记录 `Current()` 的 allocs/op 与两来源 Reload 的耗时作为基线，不设目标值。

## 8. 迁移与发布

| 旧用法 | 新用法 |
|---|---|
| `Init` 读取 `CONFIG_PATH` 或扫描目录 | 应用 bootstrap 解析路径，显式传 `file.New` |
| `File(name)`、多个独立 Viper | 有覆盖关系的来源放进同一个 `Open`；无关系的各开一个实例 |
| `BindEnv`、`AutomaticEnv` | 只用显式映射。新旧名并存时用两个有序的 ENV 来源：旧名在前、新名在后，新名胜出（C08） |
| `Get`/`Set`/`UnmarshalKey` | 业务读取 `Current()` 中的结构体；测试用 Fake；不提供全局 Set |
| `Watch(fn)` | 应用在明确时机调用 `Reload` 并处理错误；监听需求明确后再加可取消的适配器 |

**filehttp 环境变量映射**（迁移样例须全部显式列出）：

| 配置键 | 旧 ENV | 新 ENV |
|---|---|---|
| server.listen_addr | LISTEN_ADDR | SERVER_LISTEN_ADDR |
| server.max_upload_bytes | MAX_UPLOAD_BYTES | SERVER_MAX_UPLOAD_BYTES |
| database.dsn | DB_DSN | DATABASE_DSN |
| storage.fs_root | FS_ROOT | STORAGE_FS_ROOT |
| storage.s3_bucket | S3_BUCKET | STORAGE_S3_BUCKET |
| storage.s3_region | S3_REGION | STORAGE_S3_REGION |
| storage.s3_endpoint | S3_ENDPOINT | STORAGE_S3_ENDPOINT |
| storage.s3_prefix | S3_PREFIX | STORAGE_S3_PREFIX |
| storage.s3_path_style | S3_PATH_STYLE | STORAGE_S3_PATH_STYLE |
| storage.s3_create_bucket | S3_CREATE_BUCKET | STORAGE_S3_CREATE_BUCKET |

`server.mode` 没有映射：代码不读取它（`gin.SetMode` 为硬编码），迁移前从 `config.yaml` 删除，否则严格绑定会报未知键。

**TOML：** 旧版支持但 `pkg/encoding` 中没有 TOML codec。若迁移需要 TOML，先在独立的 `pkg/encoding/toml` 补齐 codec 和对照测试，再扩大迁移范围。不能把旧版已支持的格式默认为 v2 已支持。

**依赖：** v2 的 `go.mod` 直接 require `github.com/apus-run/gala/pkg/encoding v0.17.0`。`pkg/encoding/yaml`、`json` 的 v0.17.0 声明的是 encoding v0.16.0，按 MVS，v2 的声明胜出，因此不重打 tag。

**发布内容：** 模块必须包含 `examples/filehttp/testdata/`。Go 的模块打包（`golang.org/x/mod/zip`）只排除 vendor 目录与嵌套模块，不排除 `testdata`；迁移样例的测试依赖该目录，排除它会让已发布模块中的测试失败。

**发布顺序：** `pkg/encoding/v0.17.0`（已推送）→ `yaml/v0.17.0`、`json/v0.17.0`（已推送）→ v2 预发布标签，并通过 §7 → 安装验证 → `components/conf/v2.0.0`。稳定版标签写作 `components/conf/v2.0.0`，不是 `components/conf/v2/v2.0.0`。旧模块 `components/conf` 与旧导入路径保持不变。

## 9. 实施顺序

1. 按 §3 定公共签名，先写 C01–C04 的 Fake 测试。
2. 实现读取、合并、绑定、发布的最短路径，补齐 C02、C05、C06 的负例。
3. 接入文件与 ENV 来源，复用 codec；迁移 filehttp 样例。
4. 独立消费项目用 `GOWORK=off` 构建，跑 race 与错误脱敏检查。

## 10. 未决项与实现偏差

**已解决：**

- 新名为空串而旧名有值时，旧名生效。已用旧版 `components/conf` 实测（`S3_BUCKET=legacy`、`STORAGE_S3_BUCKET=""` → `legacy`），并由 `examples/filehttp/config` 的 `TestBucketEmptyNewNameFallsBackToLegacy` 固定。

**实现偏差（已在代码中落实，待评审确认是否写入规格）：**

- 阶段归类（已定，可由评审推翻）：阶段为 `read`、`decode`、`merge`、`bind`、`validate`、`canceled`、`config` 共七项。env 映射错误（重复路径、父子冲突、空段）归为 `config`：它在读取任何值之前就已错误，既非读取失败也非绑定失败。缺少 codec 归为 `decode`，因为它是解码所需组件缺失。由 `env/stage_test.go` 固定。
- 业务 `Validate` 错误：`Error()` 包含其文本、`Unwrap` 返回它，与 §6 一致。
- 吸收 koanf v2.4.1 env 包的 `Opt.EnvironFunc` 思想：新增 `env.NewWithLookup(bindings, lookup)`，lookup 签名同 `os.LookupEnv`，nil 回退进程环境；`New` 为其特例。测试可注入取值而不用 `t.Setenv`（后者禁止并行）。按 §8，Watch 适配器仍待需求明确后再加。

**仍未完成：**

- 消费者构建（§6 质量门槛）：需要推送预发布 tag（如 `components/conf/v2/v2.0.0-rc.1`）后，从代理以 `GOWORK=off` 解析验证。tag 对外可见，需要明确授权后才能推送。在此之前，消费者构建无法验证。
- 语法错误的 YAML 输入（C08 的畸形输入子项）只测了绑定错误，未测解析错误。

**迁移差异：**

- filehttp 旧加载器会把空的 `s3_region` 填为 `us-east-1`。v2 的 `Validate` 只能检查，不能改值，因此默认值改由配置文件提供（`config.yaml` 已有该值）。这是有意的行为差异，需要部署方知悉。
- JSON 大整数规则已实测（float64 解码）；但 codec 的实际行为仍须由 C06 在 v2 中验证，因为 `pkg/encoding/json` 调用的是 `encoding/json/v2` 的默认配置，实测使用的是相同的解码器。
- 新名为空串而旧名有值时的优先级尚未实测，需在 C08 补测后再固定。
- 验收项为 C01–C08 加 C06a，合并了重复场景；与旧讨论的编号不再对应。

## 参考

- 旧 conf 加载实现与公共类型：[conf.go](https://github.com/apus-run/gala/blob/78e6e264e30aff0df52caef0b413bb3ed1632c0c/components/conf/conf.go)、[types.go](https://github.com/apus-run/gala/blob/78e6e264e30aff0df52caef0b413bb3ed1632c0c/components/conf/types.go)
- 编码包与 filehttp 调用方：[pkg/encoding](https://github.com/apus-run/gala/tree/78e6e264e30aff0df52caef0b413bb3ed1632c0c/pkg/encoding)、[filehttp config.go](https://github.com/apus-run/gala/blob/78e6e264e30aff0df52caef0b413bb3ed1632c0c/components/fileserver/examples/filehttp/internal/config/config.go)
- 来源与格式分离的参考：[Koanf v2.4.1](https://github.com/knadh/koanf/tree/v2.4.1)
- 主版本子目录的标签规则：[Go Modules 参考](https://go.dev/ref/mod#vcs-version)
