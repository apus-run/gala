// Package fileserver 定义对象存储的 Go 契约：上传、下载、读取、查询与删除。
//
// 本包只包含接口、类型与语义，不依赖任何具体后端或第三方 SDK。
// 本地实现见 filestore，云端实现见 objectstore/s3，测试套件与 Fake 见 fileservertest。
//
// # 接口
//
// 操作只有一套签名：Uploader、Downloader 与 Storage 分离，调用方只依赖自己用到的能力。
// ObjectStorage（见 objectstore）是可选的签名能力。批量传输是 UploadAll / DownloadAll，
// 不进入 Storage 接口。分片是 Upload 的内部实现细节，不是调用方需要知道的语义。
//
// # Key 规则
//
// Key 是根目录或 Bucket 内的逻辑标识，不是物理路径，也不是用户原始文件名。
// ValidateKey 在每个公共方法入口执行以下规则（违反返回 ErrInvalidKey）：
//
//   - 非空，合法 UTF-8；总长 ≤ 1024 字节，单段 ≤ 255 字节。
//   - 以 '/' 分隔，不以 '/' 开头或结尾，不含空段（不接受目录型 Key）。
//   - 段不得为 "." 或 ".."。
//   - 不含 NUL、ASCII 控制字符，以及 Windows 文件名不允许的字符：'\'、':'、'*'、'?'、'"'、'<'、'>'、'|'。
//   - 段不得以空格或 '.' 结尾；去掉扩展名后不得是 Windows 保留名（CON、NUL、COM1……）。
//
// 不做大小写折叠与 Unicode 归一化：A 与 a 在大小写不敏感的文件系统上会冲突，
// 需要跨平台安全时由业务使用 ASCII 小写的生成 Key。
//
// # 错误模型
//
// 调用方用 errors.Is(err, ErrXxx) 判断类别：ErrInvalidKey、ErrInvalidArgument、
// ErrUnsupported、ErrNotFound、ErrConflict、ErrPermission、ErrSizeMismatch、
// ErrTooLarge、ErrIndeterminate。*Error 只附带 Op 与 Key，
// 用 errors.As 获取。驱动保留原始原因，上下文取消或超时满足 errors.Is(err, ctx.Err())。
//
// ErrIndeterminate 只用于两种情况：云端请求已发出但结果未知（超时、断连后于
// 提交类请求）；本地对象已发布但目录同步失败（对象可见，耐久性未确认）。
// 返回它时不得自动重试，应先 Stat 并以业务可核验的信息（大小、校验和、版本）判断。
//
// 错误文本不得包含签名 URL、凭据或完整本地绝对路径。
//
// # 操作语义
//
//	| 操作      | 成功语义                                   | Key 不存在        | 覆盖与并发                                                                 | 资源归属                 |
//	|-----------|--------------------------------------------|-------------------|----------------------------------------------------------------------------|--------------------------|
//	| Upload    | 返回 nil 即对象完整可见（本地按耐久策略）  | —                 | 默认覆盖；WithIfNotExists 时已存在返回 ErrConflict；并发同 Key 后写者胜，读者看到完整旧版或完整新版 | 不关闭 r                 |
//	| Download  | 对象字节 i 写到 w 偏移 i 处（范围亦然）    | ErrNotFound       | 失败可能已写入部分内容；不截断、不关闭 w                                    | 不关闭 w                 |
//	| Read      | 返回流                                     | ErrNotFound       | 请求阶段尊重 ctx。流一旦返回，只在 Close 时取消，并绑定打开时的对象视图                                   | 调用方关闭               |
//	| Stat      | 返回元信息                                 | ErrNotFound       | —                                                                          | —                        |
//	| Remove    | 返回后对象不可见                           | 返回 nil（幂等）  | —                                                                          | —                        |
//
// 补充约定：
//
//   - 目录不是对象。本地上 Key 指向目录时，Stat、Read、Download 返回 ErrNotFound；
//     Remove 不删除目录，返回 nil。
//   - 范围：WithRange(off, length)。off < 0 或 length < -1 返回 ErrInvalidArgument；
//     off > size 返回 ErrInvalidArgument；off == size 得到空结果；length == -1 表示到末尾；
//     off+length 超出末尾时截到末尾（与 HTTP Range 语义一致）。
//   - 随机读使用 Download 加 WithRange。Read 返回的流只承诺顺序读取；本地实现的流另外实现
//     io.ReaderAt，这是实现的可选能力，不属于契约。云端没有随机读，每次范围读都是一次请求。
//   - 声明 WithSize(n) 时，短输入与超长输入都在提交前失败并返回 ErrSizeMismatch；
//     无法保证的后端对该选项返回 ErrUnsupported。未声明表示长度未知，空文件（0 字节）是合法输入。
//     n 必须小于 MaxInt64：声明长度会让驱动多读一个字节用于发现超长输入。
//   - 对不支持的语义选项，驱动在写入任何数据之前返回 ErrUnsupported，不静默忽略。
//   - 驱动只对可重放的缓冲内容做 SDK 级重试；一次性 io.Reader 的整对象上传失败后，
//     由调用方决定是否用新的 Reader 重来。库不提供通用重试装饰器。
//   - 提交前失败不得改变旧对象。
//
// # 选项
//
// 每个操作一个选项接口，方法私有，只能由本包构造：UploadOpt、DownloadOpt、ReadOpt。
// 被多个操作共享的选项（WithProgress、WithRange）由导出的小结构体同时实现多个接口。
// nil 选项视为无操作。后端通过 NewUploadOptions、NewDownloadOptions、NewReadOptions
// 解析并校验选项，获得只读访问器；元数据键统一小写。
//
// # 进度与并发
//
// 进度见 Progress：同次调用的回调串行、单调不减，且不在 Store 的锁内执行。Store 实现对不同
// Key 的并发调用是安全的。同一 Key 的并发写是后写者胜，不会出现混合内容。本地实现里，同一个
// Store 实例内 Upload 的发布与 Remove 互斥：Remove 不会删掉它尚未观察到的新版本。多个实例或
// 多个进程共享同一 root 时对象仍按 rename/link 原子发布，但这个互斥不再成立。
//
// # 依赖规则
//
// 根包不导入任何具体后端、SDK 或 objectstore。具体后端之间互不导入；
// 模块内的 internal 包只被本模块使用。
package fileserver
