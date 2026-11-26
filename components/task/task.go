// Package task defines the workload layer: shared contracts for scheduled and
// one-shot work. Long-running workloads use the [Locker] lease to coordinate
// across replicas; the concrete scheduler lives in components/task/cron and the one-shot
// runner in components/task/job | task 包定义工作负载层的共享契约：周期任务与一次性任务。
// 长驻工作负载通过 [Locker] 租约在副本间协调；具体调度器位于 components/task/cron，
// 一次性执行器位于 components/task/job.
//
// # Notes | 说明
//
// This package deliberately contains interfaces only and imports nothing beyond
// the standard library. [Locker] is structurally identical to
// components/dlock.Locker, so a dlock backend can be plugged in without the
// task module depending on it: adapters translate the backend's constructor
// into a LockerProvider. Business wiring (lock name, TTL, owner, payload
// semantics, retries) stays in the application, not in this layer.
// 本包只包含接口，除标准库外零导入。[Locker] 与 components/dlock.Locker 结构一致，
// 因此无需 task 模块依赖 dlock 即可接入 dlock 后端：适配器把后端的构造函数转换为
// LockerProvider。锁名、TTL、owner、payload 语义、重试策略等业务接线留在应用层，
// 不进入本层。
package task

import "context"

// Locker is the distributed lease contract shared by components/task/cron and components/task/job.
//
// Implementations should follow the same ownership rules as
// components/dlock: Unlock and Refresh only succeed while the caller still
// holds the lease, and Refresh must be called before the lease expires.
// Locker 是 components/task/cron 与 components/task/job 共用的分布式租约契约。
//
// 实现应遵循与 components/dlock 相同的所有权规则：只有仍持有租约时 Unlock 与
// Refresh 才成功，且必须在租约过期前调用 Refresh。
type Locker interface {
	// Lock attempts to acquire the lease | Lock 尝试获取租约。
	Lock(ctx context.Context) error

	// Unlock releases the lease | Unlock 释放租约。
	Unlock(ctx context.Context) error

	// Refresh extends the lease | Refresh 续期租约。
	Refresh(ctx context.Context) error
}
