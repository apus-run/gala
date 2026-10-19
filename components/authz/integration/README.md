# Docker 集成验证

从仓库根目录执行：

```sh
python3 components/authz/integration/run.py
```

需要 Docker Compose、Python 3、Go 1.27+，以及本地镜像 `redis:7.4-alpine`、`mysql:8.4`、`postgres:16-alpine`。Compose 设置 `pull_policy: never`，不会自动拉取或升级镜像。需要指定 Go 时设置 `AUTHZ_GO_BINARY=/path/to/go`。

脚本创建随机项目名的独立容器，服务仅绑定 127.0.0.1 的随机端口；MySQL/PostgreSQL 使用本次创建的测试数据库。脚本等待健康检查，运行带 `integration` 标签的 Go race 测试，最后删除本次项目的容器、网络和匿名卷。已有容器不会被操作。测试密码仅供这个隔离环境使用。

Go 测试运行在主机，数据库和 Redis 运行在 Docker。Cluster 为同一容器内三个独立 Redis 主节点，覆盖全部 16384 个槽位；主机 Go 客户端通过 Dialer 转换 Docker 动态映射端口。没有副本，因此本测试不涵盖主从故障转移。

## 覆盖内容

测试源码：[docker_integration_test.go](../docker_integration_test.go)。普通 `go test` 不执行这些外部集成测试；启用标签后必须由脚本设置测试端点，不会静默跳过。

| 测试 | 验证内容 |
| --- | --- |
| TestDockerRedisSnapshotsAndWatchers | 单机与 Cluster 的并发完整快照读取、两实例撤权通知、关闭订阅后由周期重载恢复、注入客户端所有权 |
| TestDockerClusterPublishesInEveryMasterSlotRange | 在三个主节点的槽位范围分别执行 SavePolicy、LoadPolicy 和清空，验证暂存键与正式键同槽 |
| TestDockerRedisACLFailuresPreserveDeny | 分别禁用 RPUSH、PEXPIRE、PERSIST、RENAME、DEL，验证返回权限错误、正式 allow/deny 两条策略保留、重载仍拒绝 |
| TestDockerRedisWatcherReconnect | 用 CLIENT KILL 断开真实订阅连接，等待自动重连后验证撤权通知 |
| TestDockerGORMCommittedPolicyAndTimeout | MySQL/PostgreSQL 上验证构造不建表、显式迁移、清除调用方查询条件、直接角色查询、未提交变更不可见、回滚、提交后跨实例同步、表锁导致读取超时、保留旧策略及下一次加载恢复；真实 CHECK 约束失败、取消保存时原快照不变，后续保存成功 |
| TestDockerRedisReadFailureExpiresAndRecovers | 禁用 LRANGE 后加载失败，旧策略到期后 Enforce/角色查询拒绝；恢复读取权限并加载撤权策略后正常拒绝 |
| TestDockerRedisLiteralFilteringAndMalformedPolicy | 单机与 Cluster 上验证特殊字符字段的过滤读取、更新、撤权及无关策略保留，坏数据加载失败不更新新鲜度 |

ACL 错误在 Lua 内可能表现为 `ERR ACL failure in script`，直接命令可能返回 `NOPERM`；测试接受这两种真实服务器错误，随后验证存储状态与授权结果。

## 2026-10-19 结果

**PASS**。完整日志：[results-2026-10-19.txt](results-2026-10-19.txt)。

- Go 1.27.0，darwin/arm64；Docker Desktop 的服务容器为 linux/arm64。
- Redis 7.4.11，单机及三主节点 Cluster。
- MySQL 8.4.11。
- PostgreSQL 16.15。
- `go test -tags=integration -race -count=1 -timeout=180s -v -run '^TestDocker' .`：PASS，3.620s（Go 测试进程统计，含 race 收尾；不含容器启动/清理）。
- 测试项目 `gala-authz-2ef118f158` 已清理；原有 `ra-verify-pg` 保持运行。

本轮镜像摘要：

```text
redis@sha256:858f009f9709ce576febc734aa78b8f6d624b82571f9ddb6bda4377c833b3499
mysql@sha256:24989179f58856db763f418010c1169fecc944e3ec1ea7e921572d83eb070f70
postgres@sha256:cf78e76683b9ca8c5733cbbdce6c9262b45b6767934dd0a95e671f9a0fc20685
```

验证针对上述 authz 契约，不等于运行整个迁入 GORM 上游测试集。SQL Server、Redis 主从切换、长时间网络分区、大策略容量和性能压测仍未覆盖。
