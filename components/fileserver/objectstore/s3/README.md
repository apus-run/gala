# s3

`s3.Store` 实现 `fileserver.Storage` 和 `objectstore.ObjectStorage`。大文件由 `Upload` 在内部走分片上传，不额外暴露分片 API。

凭据、Region、Endpoint 和 HTTP 超时由调用方构造的 AWS SDK Client 决定，不在 `Config` 里。PutObject、CreateMultipartUpload 和 CompleteMultipartUpload 强制单次尝试，防止成功后丢失回执被重试掩盖；其它操作沿用 Client 的重试配置。连接 MinIO 或其他路径风格的 S3 兼容服务时，在客户端上打开 `UsePathStyle`。连接池的 `MaxIdleConnsPerHost` 不要小于 `Config.Concurrency`。

请为 Bucket 配置 `AbortIncompleteMultipartUpload` 生命周期规则（建议 7 天）。它用来清理进程崩溃后留下的未完成分片。这个包不启动后台清理任务。

上传使用 SHA-256 附加校验和。分片 ETag 不是内容 MD5，不要用它核对对象内容。分片响应若回显 SHA-256，必须与本地计算一致，否则本次上传失败；没有回显时不视为失败。

`Read` 返回的流是顺序读取的 `io.ReadCloser`。S3 没有随机读能力：需要随机读时用 `Download` 加 `WithRange`，每次调用都是一次带 `If-Match` 的请求。

多段下载与范围读取依赖 ETag 做版本守卫：S3 兼容服务不返回 ETag 时，多段下载和 `Read` 的范围读取都返回 `ErrUnsupported`（整对象读取不受影响）。

`WithIfNotExists` 使用 `If-None-Match`。服务端不支持条件写时返回 501，映射为 `ErrUnsupported`——请求确定没有被处理，与“结果未知”的 `ErrIndeterminate` 不同。写请求没有收到响应或收到 5xx 时映射为 `ErrIndeterminate`，此时对象可能已经写入，不要自动重试。

缺少 `s3:ListBucket` 权限时，HeadObject 对不存在的 Key 返回 403 而不是 404，表现为 `ErrPermission` 而不是 `ErrNotFound`。

签名 URL 与临时凭据等价：不要写入日志、错误文本或缓存。浏览器或客户端直传完成后，用 `Stat` 核对对象大小。需要限制大小或类型时，签名时带上 `WithContentLength` 或 `WithContentType`。

## 发布

本模块对根模块 `components/fileserver` 使用本地 `replace`（新代码尚未打 tag）。`replace` 只在主模块里生效，下游使用者不会执行它——他们按 `require` 的版本去拉取，所以带着这行 `replace` 发布，使用者会拿到无法解析的依赖。

`scripts/release.sh <版本号>` 会一次性给仓库里所有模块建 tag 并推送，且**跳过已经存在的 tag**。因此分两步发布：

1. `./scripts/release.sh <版本号>` —— 先给 `components/fileserver` 和其它模块打上 tag（本模块这时也建了 tag，但它仍带着 `replace`，下一步会覆盖它）。
2. 把本模块 `require github.com/apus-run/gala/components/fileserver` 提到 `<版本号>`、删掉这行 `replace`，提交，再跑一次 `./scripts/release.sh <版本号>` —— 已存在的 tag 全部跳过，这次只创建本模块的 tag，且它指向的是去掉 `replace` 之后的提交。
3. 用临时模块 `go get github.com/apus-run/gala/components/fileserver/objectstore/s3@<版本号>` 验证可解析。
