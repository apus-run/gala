# authz adapters

`redis` 是基于 casbin/redis-adapter v3.6.0 改造的独立模块，使用 Casbin v3 和 go-redis v9，包含 List 策略存储与失效通知 Watcher。详见 [Redis 接入说明](redis/README.md)。

`gorm` 是 `github.com/apus-run/gala/components/authz/adapter/gorm` 独立模块，`authz.NewAuthz` 统一依赖它。来源为上游 gorm-adapter v3.41.0，在该版本基础上修复整份保存的事务边界与空策略类型校验；Casbin 依赖对齐到 v3.10.0。保留许可证和上游版本记录，后续修复在此维护，并与上游对照验证。

`authz.NewRedis` 为兼容仍依赖 `adapter/redis`。各适配器不反向导入 authz；推荐应用显式创建 Adapter 后通过 `WithPolicyAdapter` 注入。
