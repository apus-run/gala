# filehttp：fileserver 生产示例（网盘后端）

一个可独立部署的文件托管服务，展示 gala 组件的生产组装方式：
**ginx**（HTTP 适配）+ **db**（持久化）+ **sqlite**（纯 Go 驱动）+ **errorsx**（错误模型）
+ **fileserver**（字节层，filestore 本地 / S3 云端可换）+ **templ/HTMX**（服务端渲染管理页）。

对照手写网盘（Gin+GORM+Redis+os 拼路径）的分层：`api → service → repo / Storage`，
但把"路径、临时文件、并发分块合并"这些容易写错的部分交给了组件。

```
api（ginx.W/B，errorsx→状态码）─→ service（业务：秒传/引用计数/断点续传）
                                     ├─→ repo（db.Provider + sqlite：元数据/会话/回收登记）
                                     └─→ fileserver.Storage（filestore ↔ objectstore/s3 可换）
```

## 核心设计

- **对象存储只当字节层**。内容寻址（SHA-256）、引用计数、用户关联、续传会话全在
  关系库——filestore 不持久化 ContentType/Metadata 也不影响功能，S3 随时可换。
- **秒传零拷贝**：上传写暂存对象的同时流式算哈希；内容已存在则删暂存、加引用，
  新内容则暂存 Key 直接转正为正式存储位置，没有第二次写入。
- **断点续传不依赖 Redis**：会话与分块位图在库里，分块是普通对象（幂等重传），
  合并是惰性串联的流式读，内存与分块数无关；合并同样走秒传登记。
- **崩溃残留靠登记表回收**（`staging_objects` / 过期会话），不需要存储端 List。
- **哈希即强 ETag**：内容寻址让 304/206/If-Range 在任何后端上都成立。
- **WithSize 在提交前拒绝不符长度**（声明 11MiB 实传 11,011,000 字节会被拒收），
  不会留下半个对象。

## 管理页面（templ + HTMX）

启动后访问 <http://localhost:8080/>：

- 服务端用 [templ](https://templ.guide/) 渲染完整页面与文件列表片段；
- [HTMX](https://htmx.org/) 负责上传、刷新、重命名、删除：请求只返回 `#file-list`
  片段并 `outerHTML` 替换，不整页刷新；
- 上传走同一个 `service.Upload`，因此秒传、引用计数、S3/filestore 切换与 JSON API
  完全一致；
- Demo 页面固定使用用户 `1`（`X-User-ID` 的浏览器等价物），生产应接
  `components/authn` 的 JWT/会话。

修改 `.templ` 后重新生成（生成的 `*_templ.go` 会随源码一起提交，Docker 构建不需要
再装 templ CLI）：

```sh
go generate ./internal/ui
# 或：
go run github.com/a-h/templ/cmd/templ@v0.3.1070 generate ./internal/ui/templates
```

页面路由：

| 方法 | 路径 | 行为 |
|---|---|---|
| GET | `/` | 完整管理页 |
| GET | `/ui/files` | 文件列表片段（HTMX 刷新） |
| POST | `/ui/files` | 上传，返回列表片段 |
| PUT/POST | `/ui/files/:hash` | 重命名，返回列表片段 |
| DELETE | `/ui/files/:hash` | 删除，返回列表片段 |

## 运行

```sh
cd components/fileserver/examples/filehttp
go run .                                        # config/config.yaml，./data 下建库与对象

# Docker（构建上下文是 components/fileserver；镜像内置 config/config.yaml）：
cd components/fileserver && docker build -f examples/filehttp/Dockerfile -t gala-filehttp .
docker run -p 8080:8080 -v filedata:/data gala-filehttp

# Docker + RustFS/MinIO（Bucket 需预先创建，或开发环境显式 S3_CREATE_BUCKET=true）：
docker run -p 8080:8080 -v filedata:/data \
  -e S3_BUCKET=filehttp \
  -e S3_ENDPOINT=http://rustfs:9000 \
  -e S3_CREATE_BUCKET=true \
  -e AWS_ACCESS_KEY_ID=... -e AWS_SECRET_ACCESS_KEY=... \
  gala-filehttp
```

## 接口

公开接口（哈希即能力凭证，无需登录）：下载、元信息、公开列表。
认证接口需要 `X-User-ID` 头（生产换 components/authn 的 JWT）。

| 方法与路径 | 鉴权 | 行为 |
|---|---|---|
| `POST /api/files` | ✓ | multipart `file`，流式上传，`is_dedup` 标记秒传 |
| `GET /api/files?page=&page_size=` | ✓ | 当前用户文件列表 |
| `GET /api/files/public` | — | 全部用户的公开文件列表（分页） |
| `GET /api/files/{hash}` · `HEAD` | — | 下载；Range（206）、ETag/If-None-Match（304）、If-Range |
| `GET /api/files/{hash}/meta` | — | 元信息 |
| `PUT /api/files/{hash}` | ✓ | 重命名（只改自己的显示名） |
| `DELETE /api/files/{hash}` | ✓ | 删除；引用计数归零才删物理对象 |
| `POST /api/uploads` | ✓ | 初始化分块上传 `{file_name, file_size, chunk_size?, content_type?}` |
| `POST /api/uploads/{id}/chunks/{index}` | ✓ | 上传单块，幂等 |
| `GET /api/uploads/{id}` | ✓ | 进度（含每块位图，供续传） |
| `POST /api/uploads/{id}/complete` | ✓ | 校验齐全后合并、登记（含秒传） |
| `DELETE /api/uploads/{id}` | ✓ | 取消并清理 |

```sh
curl -H 'X-User-ID: 1' -F "file=@photo.jpg" localhost:8080/api/files
curl -H 'Range: bytes=0-1023' localhost:8080/api/files/<hash> -o part.bin   # 匿名可下载
```

## 配置（components/conf）

配置由 [`components/conf`](../../../conf) 管理，默认读取 `config/config.yaml`；
`CONFIG_PATH` 可以指向另一个 YAML/JSON/TOML 文件或目录。环境变量覆盖文件值，
优先级为 **环境变量 > 配置文件**。

```yaml
server:
  listen_addr: ":8080"
  max_upload_bytes: 524288000
database:
  dsn: "./data/filehost.db"
storage:
  fs_root: "./data"
  s3_bucket: ""
  s3_region: "us-east-1"
  s3_endpoint: ""
  s3_prefix: ""
  s3_path_style: true
  s3_create_bucket: false   # 仅本地 RustFS/MinIO 开发用
```

| 配置键 | 兼容环境变量 | 默认 | 说明 |
|---|---|---|---|
| `server.listen_addr` | `LISTEN_ADDR` | `:8080` | 监听地址 |
| `server.max_upload_bytes` | `MAX_UPLOAD_BYTES` | `524288000` | 单文件上限（413） |
| `database.dsn` | `DB_DSN` | `./data/filehost.db` | sqlite 路径 |
| `storage.fs_root` | `FS_ROOT` | `./data` | 本地 filestore 根目录 |
| `storage.s3_bucket` | `S3_BUCKET` | 空 | 非空时字节层切到 S3 |
| `storage.s3_region` | `S3_REGION` | `us-east-1` | S3 Region |
| `storage.s3_endpoint` | `S3_ENDPOINT` | 空 | MinIO/RustFS Endpoint |
| `storage.s3_prefix` | `S3_PREFIX` | 空 | 对象 Key 前缀 |
| `storage.s3_path_style` | `S3_PATH_STYLE` | `true` | 路径风格；兼容 MinIO/RustFS |
| `storage.s3_create_bucket` | `S3_CREATE_BUCKET` | `false` | 开发环境自动建 Bucket；生产应预先创建 |

conf 还会自动把配置路径转成下划线形式的环境变量，例如
`STORAGE_S3_BUCKET`、`SERVER_LISTEN_ADDR`；旧变量名仍然兼容。
AWS 凭据继续使用 SDK 的 `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`。

Docker 镜像内置 `/etc/filehttp/config.yaml` 并设置 `CONFIG_PATH`；自定义配置可以：

```sh
docker run -p 8080:8080 -v filedata:/data \
  -v "$PWD/my-config.yaml:/etc/filehttp/config.yaml:ro" \
  gala-filehttp
```

S3 模式下 Bucket 必须已存在；启动时设置 `S3_CREATE_BUCKET=true` 只适合本地
RustFS/MinIO 开发环境，生产应由平台预先创建并收紧权限。

## 已验证（本地 + Docker + RustFS）

上传/秒传（跨用户、跨普通/分块路径）/下载逐字节一致/Range（前缀、后缀、尾部截断、
不可满足区间 416、HEAD 忽略 Range、多区间回退）/304/中文重命名/越权改名 404/
公开接口匿名可访问、认证接口匿名 401/引用计数精确到物理删除/分块 2/3 提前 complete 拒绝/
幂等重传/取消清理/容器重启数据持久/SIGTERM 优雅退出零 panic。

管理页（templ + HTMX）：GET `/` 完整渲染、上传（含秒传提示）、刷新、重命名、删除
都只替换 `#file-list` 片段；本地 filestore 与 Docker + RustFS 两条链路均实测。

RustFS 1.0.3 真实服务另已验证：Storage contract、multipart 边界、条件写竞争、
If-Match 版本守卫、校验和拒绝、Abort 清理、预签名 PUT/GET、并发负载、错误凭据，
以及“存储短暂不可用时删除进入 staging outbox，恢复后 GC 清理”的路径。

## 刻意不做的

客户端哈希预检秒传（免上传一次）、按用户配额、目录树、签名直传（见
[objectstore](../../objectstore)）——都是业务层扩展点，不影响本示例的组件组装方式。
