# 云主机 Docker 部署基线

这份单实例 Compose 配置已在本机 Docker 中执行 TLS、非 root 租户权限及销毁源卷后的恢复测试。它提供可移植的部署基线，尚未在你的云主机上验收，也不承诺单机高可用。

## 已实测的约束

- UID/GID `10001:10001`、只读根文件系统、去除 Linux capabilities、禁止提权。
- `/data` 为外部持久卷；TLS、CA 和 root 凭据从只读配置卷读取，凭据不作为容器环境变量值传递。
- Console 关闭，TLS 开启，健康检查验证 CA 与证书主机名。
- 默认限制 2 CPU、1536 MiB、256 PID，日志每文件 10 MiB、保留 3 个文件。
- 默认仅监听宿主机 loopback 的 9443 端口；应用通过实际网络拓扑连接。

这些资源值是本地验证使用的起点。应用、数据库、RustFS 在同一云主机时，需要分别限制资源，并在实际云盘上验证内存和 I/O 竞争；当前没有业务负载或延迟承诺。

## 启动前的输入

Compose 不创建或删除外部卷，需要预先提供：

| 变量 | 内容 |
|---|---|
| `RUSTFS_IMAGE` | 与云主机 CPU 架构匹配的不可变镜像引用，如 `仓库@sha256:摘要` |
| `RUSTFS_DATA_VOLUME` | 已挂载到预期持久存储的 Docker 数据卷名 |
| `RUSTFS_CONFIG_VOLUME` | 下述配置目录已准备好的 Docker 卷名 |
| `RUSTFS_TLS_SERVER_NAME` | 服务证书 SAN 包含的 DNS 名称 |
| `RUSTFS_CPU_LIMIT` / `RUSTFS_MEMORY_LIMIT` | 可选，默认 `2.0` / `1536m` |
| `RUSTFS_BIND_ADDRESS` / `RUSTFS_PORT` | 可选，默认 `127.0.0.1` / `9443` |

配置卷内布局：

```text
access.key              # root access key；UID 10001，0600
secret.key              # root secret key；UID 10001，0600
tls/rustfs_cert.pem     # 服务证书与必要的中间证书
tls/rustfs_key.pem      # 私钥；UID 10001，0600
tls/ca.pem              # 健康检查使用的信任 CA
```

数据卷目录须可由 UID 10001 写入。云盘需先在宿主机正确挂载，再让 Docker 卷映射到该存储；Docker 外部卷本身不保证云盘生命周期、整机故障后的重新挂载或异机备份。

配置完成后执行：

```sh
docker compose -f components/fileserver/objectstore/s3/deploy/compose.yaml config --quiet
docker compose -f components/fileserver/objectstore/s3/deploy/compose.yaml up -d
docker compose -f components/fileserver/objectstore/s3/deploy/compose.yaml ps
```

root 凭据用于初始化 IAM。应用使用单独的 IAM 用户/服务账号和业务前缀权限，权限测试已覆盖 Get/Put/Delete/AbortMultipartUpload。备份与恢复还需覆盖实际使用的 IAM、Bucket 策略、版本和生命周期配置；本轮恢复演练只核验了对象内容与内容类型/自定义元数据。

## 本地复验

从仓库根目录运行：

```sh
python3 components/fileserver/objectstore/s3/integration/acceptance_run.py
```

runner 使用本文件创建随机命名的隔离环境，生成短期测试证书和凭据，核对实际容器限制，执行 TLS/权限测试，再备份三个对象、删除测试源数据卷、恢复到空卷。它只删除本次生成的测试资源，不使用现有生产卷。镜像需已在本机；可先运行集成目录的 `run.py` 拉取固定版本。

原始证据与范围结论见 `audit/rustfs/acceptance/README.md`。本机通过不代表业务容量、云盘故障或云主机恢复指标已达标。
