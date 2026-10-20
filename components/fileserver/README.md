# fileserver

对象存储的契约与实现：一个与后端无关的 Go 接口，加上本地文件系统（`filestore`）和 S3（`objectstore/s3`）两个驱动。

核心语义是同一套：**把一段字节以名字存放，再按名字取回；任何失败都必须让调用方能判断“能不能重试、要不要核对”。**

- 契约、选项、Key 规则、错误模型：见 [`doc.go`](doc.go) 的包文档。
- 本地实现：[filestore](filestore)，与根包位于同一个 Go 模块。
- 云端实现：[objectstore/s3](objectstore/s3)，独立 Go 模块；部署与服务验收见其 [部署指南](objectstore/s3/deploy/README.md) 和 [集成测试指南](objectstore/s3/integration/README.md)。

## 安装

当前两个模块的 `go.mod` 均要求 **Go 1.27 或更高版本**。根包及 `filestore` 不依赖第三方 SDK；只使用本地存储时无需安装 S3 模块。

以下命令适用于已发布且依赖版本可解析的版本：

```sh
go get github.com/apus-run/gala/components/fileserver
go get github.com/apus-run/gala/components/fileserver/objectstore/s3
```

当前 S3 模块仍以 `v0.0.0` 依赖本模块，并通过本地 `replace` 开发；依赖模块中的 `replace` 不会传递给下游。发布前在外部项目试用时，需要在调用方的 `go.mod` 中分别把这两个模块替换到本地检出目录。发布时应先发布 `fileserver`，再更新 S3 模块的 `require`、删除本地 `replace`，最后发布 S3 模块。不要假设重复执行发布脚本会覆盖已存在的 tag。

## 快速开始

下面是一个可运行的本地存储示例。`filestore.New` 要求根目录已存在，不会自动创建根目录；示例使用临时目录，退出时删除。

```go
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/filestore"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	root, err := os.MkdirTemp("", "fileserver-example-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)

	store, err := filestore.New(root)
	if err != nil {
		return err
	}
	defer store.Close()

	ctx := context.Background()
	const data = "hello"
	err = store.Upload(ctx, "docs/hello", strings.NewReader(data),
		fileserver.WithSize(int64(len(data))),
		fileserver.WithIfNotExists(),
	)
	if err != nil {
		return err
	}

	rc, err := store.Read(ctx, "docs/hello")
	if err != nil {
		return err
	}
	defer rc.Close()
	content, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	fmt.Println(string(content)) // hello
	return nil
}
```

S3 驱动接受 `*awss3.Client`（或实现了 `s3.API` 的窄接口）：

其中 `awss3` 是 `github.com/aws/aws-sdk-go-v2/service/s3` 的导入别名，`s3` 是本仓库驱动的导入名，`cfg` 为调用方准备的 `aws.Config`：

```go
client := awss3.NewFromConfig(cfg)
store, err := s3.New(client, s3.Config{
	Bucket:      "my-bucket",
	Prefix:      "tenant-a",
	Concurrency: 4,
	PartSize:    16 << 20,
	Presigner:   awss3.NewPresignClient(client), // 不需要签名时可省略
})
if err != nil {
	return err
}
```

凭据、Region、Endpoint 和 HTTP 超时由调用方构造的 Client 决定，不在 `Config` 里。提交操作（Put、创建和完成分片）强制单次尝试，保留结果未知的语义；分片与读取沿用 Client 的重试配置。

## 操作与选项

| 操作 | 结果与资源归属 |
| --- | --- |
| `Upload(ctx, key, r, opts...)` | 默认覆盖；成功时完整对象可见，不关闭输入 `r` |
| `Read(ctx, key, opts...)` | 返回 `io.ReadCloser`，调用方必须关闭；适合顺序读取 |
| `Download(ctx, key, w, opts...)` | 写入 `io.WriterAt`，不截断、不关闭 `w`；失败时可能已写入部分内容 |
| `Stat(ctx, key)` | 返回 `*ObjectInfo`；不存在时返回 `ErrNotFound` |
| `Remove(ctx, key)` | 删除对象；不存在时仍返回 `nil` |

`ObjectInfo` 提供 `FKey`、`FSize`、`FModTime`、`FContentType`、`FMetadata`；后端未提供的字段为零值。`FKey` 不含 S3 前缀。目录不是对象，本地 `Remove` 不会递归删除目录。

| 选项 | 适用操作 | 语义 |
| --- | --- | --- |
| `WithSize(n)` | Upload | 准确长度，`0 ≤ n < MaxInt64`；短输入或超长输入在提交前返回 `ErrSizeMismatch` |
| `WithIfNotExists()` | Upload | 只创建；已存在返回 `ErrConflict` |
| `WithContentType(s)` | Upload | 持久化内容类型；本地后端不支持非空值 |
| `WithMetadata(m)` | Upload | 持久化元数据，键统一小写；本地后端不支持非空元数据 |
| `WithRange(off, length)` | Read、Download | 从 `off` 开始，`length == -1` 表示到末尾，`0` 表示空范围 |
| `WithProgress(p)` | Upload、Download | 观察传输进度；Read 不支持此选项 |

不支持的语义选项返回 `ErrUnsupported`，不会静默忽略。空对象合法；未传 `WithSize` 表示长度未知，与 `WithSize(0)` 不同。

### 下载与范围读取

`Download` 使用**原对象偏移**：`WithRange(100, 20)` 把数据写到目标的 `[100, 120)`，不是从目标偏移 0 写入。若只想获得连续的范围内容，使用 `Read(ctx, key, fileserver.WithRange(100, 20))` 并读取返回流。

下载到新文件可使用 `os.Create` 得到的 `*os.File`。复用已有文件时应自行截断或调整长度，避免保留旧尾部。S3 并行下载可能并发调用 `WriteAt`，自定义目标需支持这种使用方式。

范围的 `off` 必须非负，`length` 必须不小于 `-1`；`off > size` 返回 `ErrInvalidArgument`，`off == size` 返回空内容，超出末尾的长度会被截短。

`Read` 的请求阶段尊重传入的 Context；流返回后绑定打开时的对象视图，由调用方 `Close` 结束流的生命周期，不应依赖原 Context 的取消来关闭流。

### 批量传输与进度

```go
results := fileserver.UploadAll(ctx, store, []fileserver.UploadTask{
    {
        Key: "docs/report",
        Open: func(context.Context) (io.ReadCloser, error) {
            return os.Open("report.txt")
        },
        Opts: []fileserver.UploadOpt{fileserver.WithIfNotExists()},
    },
}, fileserver.WithConcurrency(4))
for _, result := range results {
    if result.Err != nil {
        // 按 result.Index、result.Key 记录并处理每项错误。
        return result.Err
    }
}
```

批量函数返回与输入同序、同长的 `[]Result`。默认并发度为 4，`WithConcurrency` 允许 1–64；默认单项失败不影响其它项，`WithFailFast()` 会取消其余任务，但不会回滚已经成功的对象。函数返回前等待所有工作协程退出。

`UploadTask.Open` 在任务开始时才调用，返回的流由 `UploadAll` 关闭，关闭错误也会计入结果。`DownloadTask.W` 由调用方关闭；不同任务应使用独立目标，避免写入区域相互覆盖。

进度可通过 `fileserver.ProgressFunc(func(ctx context.Context, p fileserver.ProgressInfo) { ... })` 注入。`p.Total == -1` 表示总量未知，`0` 表示空文件。回调应快速返回；同一次调用串行通知，但多个任务共用观察者时需自行同步。**字节进度达到总量不代表提交成功，最终结果以方法返回值为准。**

## 契约要点

- **接口**：`Uploader`、`Downloader`、`Storage`（三者分开，调用方只依赖用到的能力）；`objectstore.ObjectStorage` 是可选的签名能力。
- **Key**：逻辑标识，不是物理路径。必须是非空合法 UTF-8，总长不超过 1024 字节，每段不超过 255 字节；以 `/` 分隔，不允许首尾 `/`、空段、`.` 或 `..` 段。跨平台安全由 `ValidateKey` 保证：不含 Windows 禁止的字符、段不以空格或 `.` 结尾、不使用 Windows 保留名。业务侧建议用 ASCII 小写生成 Key。
- **错误**：用 `errors.Is(err, ErrXxx)` 判断类别（`ErrInvalidKey`、`ErrInvalidArgument`、`ErrUnsupported`、`ErrNotFound`、`ErrConflict`、`ErrPermission`、`ErrSizeMismatch`、`ErrTooLarge`、`ErrIndeterminate`），用 `errors.As` 取 `*fileserver.Error` 里的 Op 与 Key。错误文本不含凭据、签名 URL 或本地绝对路径。
- **ErrIndeterminate** 表示“结果未知”，**不得自动重试**：先 `Stat`，结合业务标识、校验和或版本判断对象是否已经生效；大小相同不足以证明内容相同。本地发布后目录同步失败也可能返回此错误，此时对象可见但耐久性未确认。
- **范围**：`WithRange(off, length)`，语义与 HTTP Range 一致（超出末尾截到末尾，`off > size` 报 `ErrInvalidArgument`）。
- **进度**：`WithProgress` 注入观察者。同次调用的回调串行、单调不减，且不在 Store 的锁内执行——回调里可以安全调用同一个 Store 的其它方法。
- **批量**：`UploadAll` / `DownloadAll` 提供有界并发与逐项结果，默认一项失败不影响其它项。

## 后端：filestore（本地文件系统）

所有访问都经由构造时打开的 `os.Root`，Key 不会被拼成全局路径，符号链接与 `..` 不能越出根目录。Key 映射到 `data/` 之下，临时文件放在 `.fs-tmp/`，两个命名空间不相交。

- **原子发布**：普通对象先写入 `.fs-tmp` 的临时文件再发布；覆盖写用 `rename`，`WithIfNotExists` 用 `link`（目标已存在即 `ErrConflict`，不会退化成“先检查后改名”）。
- **耐久性**：`WithDurability` 选择 `SyncNone` / `SyncFile` / `SyncFileAndDir`（默认）。目录项的同步点是：新建目录时同步它的父目录，发布或删除文件名之后同步叶子目录。
  **进程内的测试无法验证 fsync**——page cache 不随进程消失。需要验收耐久性时，在目标文件系统上用块设备层工具单独验证。
- **吞吐**：默认耐久性下上传包含文件与目录同步，性能取决于文件系统和设备。应在目标环境用 benchmark 测量；选择 `SyncNone` 或 `SyncFile` 前需评估对应的掉电风险。
- **权限**：临时文件创建时为 `0600`，发布前按 `WithFileMode` 精确设置最终权限（默认 `0644`，不受 umask 影响）。目录权限在创建时受 umask 影响。
- **清理**：`CleanupTemp(ctx, olderThan)` 删除过期残留的临时文件（进程在发布前退出时留下的半成品）。库不启动后台任务，由调用方按自己的节奏调用（cron 或启动时），建议 `olderThan` 不小于 24 小时。
- **同一个 root 只应创建一个 `Store`**：发布与 Remove 的互斥只在同一个实例内成立。多个实例或多个进程共享同一 root 时，对象仍按 `rename`/`link` 原子发布，但 Remove 可能删掉另一个实例刚发布的版本。
- **平台**：Linux ext4/xfs 是主验收目标。Windows 没有目录同步，`SyncFileAndDir` 退化为 `SyncFile`。NFS、SMB、FUSE 不在承诺范围内。

本地构造选项均来自 `filestore` 包：

| 选项 | 默认值 | 约束或用途 |
| --- | --- | --- |
| `WithDurability` | `SyncFileAndDir` | 文件与目录同步策略 |
| `WithFileMode` | `0o644` | 仅权限位 |
| `WithDirMode` | `0o755` | 仅权限位，受 umask 影响 |
| `WithMaxObjectSize` | `0` | 非负字节数；0 不限，超限返回 `ErrTooLarge` |
| `WithBufferSize` | 256 KiB | 大于 0 且不超过 4 MiB |

`Close` 可重复调用；关闭后方法返回 `filestore.ErrClosed`。已经返回的 `Read` 流仍可读取，需单独关闭。`CleanupTemp` 的时间阈值必须为正，返回删除数量和汇总错误；阈值应覆盖最长上传时间，避免清理仍在使用的临时文件。

## 后端：objectstore/s3（云端）

| `s3.Config` 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `Bucket` | 无 | 必填，使用已有 Bucket |
| `Prefix` | 空 | 内部拼接前缀，去掉首尾 `/`；前缀与 Key 拼接后仍须不超过 1024 字节 |
| `PartSize` | 8 MiB | 允许 5 MiB–5 GiB |
| `Concurrency` | 1 | 单次传输内部并发度，允许 1–32 |
| `MaxObjectSize` | 0 | 非负字节数，0 不施加额外限制；驱动仍有对象与分片上限 |
| `Presigner` | nil | 未设置时签名方法返回 `ErrUnsupported` |

批量并发与单次传输并发会叠加，规划内存与连接池时需一起考虑。已知长度的大对象可能自动增大实际分片大小；估算缓冲时应使用实际分片大小。未知长度上传最多 10000 片，默认分片大小下约 78 GiB；当前驱动的对象上限为 5 TiB。


- **分片上传在 `Upload` 内部完成**，不额外暴露分片 API：输入在第一片内结束就用单次 `PutObject`，否则创建分片上传，所有片成功后才 `CompleteMultipartUpload`。缓冲按实际使用的并发槽位分配，分片缓冲上界是 `实际分片大小 × Concurrency`；单对象不会预分配其它槽位。**长度已知时用 `WithSize` 声明**，否则仍按整片分配首个缓冲。
- **校验和**：请求带 SHA-256 附加校验和。分片 ETag 在多片上传和 SSE-KMS 下不是内容 MD5，不要用它核对对象内容。分片响应若回显 SHA-256，必须与本地计算一致，否则本次上传失败；没有回显时不视为失败。
- **版本守卫**：范围读取与多段下载依赖 ETag（`If-Match`）确认 HEAD 与 GET 看到的是同一个版本。**不返回 ETag 的 S3 兼容服务无法做这个守卫**：多段下载与范围读取返回 `ErrUnsupported`，整对象读取不受影响。
- **条件写**：`WithIfNotExists` 使用 `If-None-Match`。服务端不支持该功能时返回 501，映射为 `ErrUnsupported`（请求确定没有被处理），不是 `ErrIndeterminate`。
- **结果未知**：写请求没有收到响应，或收到 5xx 时，映射为 `ErrIndeterminate`——服务端可能已经写入了。不得自动重试。
- **未完成分片**：请为 Bucket 配置 `AbortIncompleteMultipartUpload` 生命周期规则（建议 7 天），用于清理进程崩溃后留下的未完成分片。这个包不启动后台清理任务。
- **HeadObject 的 403**：缺少 `s3:ListBucket` 权限时，HeadObject 对不存在的 Key 返回 403 而不是 404。这会表现为 `ErrPermission` 而不是 `ErrNotFound`。
- **预签名**：`SignUploadReq` / `SignDownloadReq` 在本地计算签名，不访问存储服务。**URL 等同于临时凭据**：不要写入日志、错误文本或缓存。直传完成后用 `Stat` 核对对象大小；需要限制大小或类型时，签名时带上 `WithContentLength` 或 `WithContentType`。

签名选项来自 `objectstore` 包，与上传选项分开：`WithExpiry` 默认为 15 分钟、允许 1 秒至 7 天；`WithContentLength` 指定确切长度；`WithContentType` 指定类型。返回的 `SignedRequest` 包含 `Method`、`URL`、`Header` 和 `ExpiresAt`，客户端必须使用指定方法并原样携带签名头。签名上传不会经过 `Store.Upload` 的输入长度校验，完成后仍应核验对象。

## 运维清单

1. 选定耐久性策略，并在目标文件系统上单独验收掉电行为。
2. 每个 root 只创建一个 `Store`；进程退出前 `Close()`，它会等待进行中的调用退出。
3. 为 Bucket 配置未完成分片的生命周期规则。
4. 长度已知时传 `WithSize`：既避免多余的分配，也让超长输入在提交前失败。
5. `ErrIndeterminate` 一律先 `Stat` 核对，不要直接重试；把这类错误单独计数告警。
6. 预签名 URL 不落日志、不进缓存。
7. 定期调用 `CleanupTemp` 回收残留的临时文件。
8. 指标按 `errors.Is` 分类：区分 `ErrNotFound`（正常）、`ErrConflict`（并发条件）、`ErrIndeterminate`（需要人工核对）、`ErrPermission`/`ErrTooLarge`（配置或容量问题）。

## 已知限制

- 库不提供通用重试装饰器：可重放的 UploadPart 可以沿用 SDK 重试；提交和创建 multipart 不自动重试，一次性 `io.Reader` 的整对象上传失败后由调用方先核对结果。
- 库不内置指标与追踪，需要时由调用方装饰 `Uploader`/`Downloader` 接口。
- 随机读：`Download` 加 `WithRange` 是各后端一致的方式；本地 `Read` 返回的流另外实现了 `io.ReaderAt`，这是实现的可选能力，不属于契约。云端每次范围读都是一次请求。

## 测试

在仓库中分别运行两个模块的测试；根模块的 `./...` 不会进入嵌套的 S3 模块：

```sh
(cd components/fileserver && go test ./...)
(cd components/fileserver/objectstore/s3 && go test ./...)
```

本地吞吐测试：

```sh
(cd components/fileserver && go test ./filestore -run '^$' -bench . -benchmem)
```

真实 S3 兼容服务的准备与验收步骤见 [集成测试指南](objectstore/s3/integration/README.md)。业务单元测试可使用 `fileservertest.NewMemStore()`，无需启动外部服务。

第三方后端实现 `fileserver.Storage` 后，用共享契约套件自测：

```go
fileservertest.RunStorageSuite(t, func(t *testing.T) fileserver.Storage {
	return newBackend(t)
}, fileservertest.Capabilities{IfNotExists: true})
```

套件覆盖 `doc.go` 的操作语义表、选项拒绝规则、范围规则与 Key 规则。
