// Package registry defines the service registration and discovery abstraction.
// It separates the server-side Registry from the client-side Discovery, so
// that each side only depends on what it actually uses.
// registry 定义服务注册与发现抽象：服务端的 Registry 与客户端的 Discovery 分开，
// 各自只依赖自己实际用到的那部分.
//
// Requires Go 1.23+ (slices.Sorted, slices.Values) | 需要 Go 1.23+.
package registry

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
)

// Well-known Metadata keys shared by backends and load balancers.
// Backends should not invent synonyms for these.
// 后端与负载均衡共用的知名 Metadata key。后端不应另造同义 key.
const (
	// MetaWeight is the load-balancing weight | 负载均衡权重.
	MetaWeight = "weight"
	// MetaInitCapacity is the initial connection pool capacity | 连接池初始容量.
	MetaInitCapacity = "init_capacity"
	// MetaMaxCapacity is the maximum connection pool capacity | 连接池最大容量.
	MetaMaxCapacity = "max_capacity"
	// MetaIncreaseStep is the pool growth step | 连接池扩容步长.
	MetaIncreaseStep = "increase_step"
	// MetaGrowthRate is the pool growth rate | 连接池增长速率.
	MetaGrowthRate = "growth_rate"
)

// ServiceInstance describes a single service instance as seen by a registry | ServiceInstance 描述注册中心眼中的单个服务实例.
type ServiceInstance struct {
	// ID uniquely identifies the instance. Some backends assign it; in that
	// case Register writes the assigned value back into the instance.
	// ID 唯一标识实例。部分后端会代为分配；此时 Register 把分配值写回 instance.ID.
	ID string `json:"id"`
	// Name is the logical service name, e.g. "edu.course.student-api".
	// Name 是逻辑服务名，如 "edu.course.student-api".
	Name string `json:"name"`
	// Version is the service version | Version 是服务版本.
	Version string `json:"version"`
	// Metadata carries arbitrary key/value tags. See the Meta* constants for
	// the keys with agreed meaning.
	// Metadata 携带任意键值标签；有约定含义的 key 见 Meta* 常量.
	Metadata map[string]string `json:"metadata,omitempty"`
	// Endpoints lists reachable addresses with a scheme prefix and optional
	// query, e.g. "grpc://127.0.0.1:9000?isSecure=false".
	// Endpoints 列出带 scheme 前缀与可选 query 的可达地址，
	// 如 "grpc://127.0.0.1:9000?isSecure=false".
	Endpoints []string `json:"endpoints"`
}

// String implements fmt.Stringer. It is safe to call on a nil receiver.
// String 实现 fmt.Stringer，对 nil 接收者调用是安全的.
func (ins *ServiceInstance) String() string {
	if ins == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s-%s", ins.Name, ins.ID)
}

// Clone returns a deep copy of the instance. Consumers must Clone before
// modifying an instance received from a Discovery.
// Clone 返回实例的深拷贝。从 Discovery 拿到的实例修改前必须先 Clone.
func (ins *ServiceInstance) Clone() *ServiceInstance {
	if ins == nil {
		return nil
	}
	c := *ins
	c.Endpoints = slices.Clone(ins.Endpoints)
	c.Metadata = maps.Clone(ins.Metadata)
	return &c
}

// Equal reports whether ins and o are equivalent. The order of Endpoints is
// ignored, and neither instance is modified. A nil Metadata equals an empty one.
// Equal 报告 ins 与 o 是否等价。Endpoints 顺序不影响结果，两个实例都不会被
// 修改；nil Metadata 等价于空 Metadata.
func (ins *ServiceInstance) Equal(o *ServiceInstance) bool {
	if ins == o { // same pointer, or both nil | 同一指针，或两者皆为 nil
		return true
	}
	if ins == nil || o == nil {
		return false
	}
	return ins.ID == o.ID &&
		ins.Name == o.Name &&
		ins.Version == o.Version &&
		maps.Equal(ins.Metadata, o.Metadata) &&
		sameEndpoints(ins.Endpoints, o.Endpoints)
}

// sameEndpoints reports whether a and b hold the same endpoints, ignoring
// order. It sorts copies, so the inputs are left untouched.
// sameEndpoints 报告 a 与 b 是否持有相同端点（忽略顺序）。排序在副本上进行，
// 输入不会被改动.
func sameEndpoints(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	return slices.Equal(
		slices.Sorted(slices.Values(a)),
		slices.Sorted(slices.Values(b)),
	)
}

// EventType is the kind of change carried by an Event | EventType 是 Event 携带的变更类型.
type EventType int

const (
	// EventTypeUnknown is the zero value, not a real change | 零值，不代表真实变更.
	EventTypeUnknown EventType = iota
	// EventTypeAdd means the instance appeared or was updated. It is an
	// upsert: consumers replace any local entry that has the same ID.
	// EventTypeAdd 表示实例新增或更新，语义为 upsert：消费方替换本地同 ID 条目.
	EventTypeAdd
	// EventTypeDelete means the instance is gone. Consumers match it by ID.
	// Backends that cannot recover the full instance send only its ID and Name.
	// EventTypeDelete 表示实例已消失，消费方按 ID 匹配。无法恢复完整实例的后端
	// 只发送 ID 与 Name.
	EventTypeDelete
)

// IsAdd reports whether the event is an add/update | IsAdd 报告事件是否为新增/更新.
func (t EventType) IsAdd() bool { return t == EventTypeAdd }

// IsDelete reports whether the event is a delete | IsDelete 报告事件是否为删除.
func (t EventType) IsDelete() bool { return t == EventTypeDelete }

// String implements fmt.Stringer | 实现 fmt.Stringer.
func (t EventType) String() string {
	switch t {
	case EventTypeAdd:
		return "add"
	case EventTypeDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// Event describes a change of a service instance | Event 描述一次服务实例变更.
type Event struct {
	// Type is the kind of change | Type 是变更类型.
	Type EventType
	// Instance is read-only: it may be shared between subscribers. Clone it
	// before modifying.
	// Instance 只读：可能在多个订阅者间共享，修改前先 Clone.
	Instance *ServiceInstance
}

// Registry is the server-side registration contract | Registry 是服务端的注册契约.
type Registry interface {
	// Register registers the instance and starts any keepalive needed. If the
	// backend assigns an ID, it is written back to instance.ID. A nil instance
	// returns an error.
	// Register 注册实例并按需启动保活。后端代为分配 ID 时写回 instance.ID；
	// 实例为 nil 时返回错误.
	Register(ctx context.Context, instance *ServiceInstance) error
	// Deregister removes the instance and stops its keepalive.
	// Deregister 移除实例并停止其保活.
	Deregister(ctx context.Context, instance *ServiceInstance) error
	// Close deregisters every instance still kept alive and releases
	// resources. It is idempotent.
	// Close 注销所有仍在保活的实例并释放资源，幂等.
	io.Closer
}

// Discovery is the client-side discovery contract | Discovery 是客户端的发现契约.
type Discovery interface {
	// GetService returns the current instances of a service. The result is
	// read-only; Clone before modifying.
	// GetService 返回服务当前的实例列表。结果只读，修改前先 Clone.
	GetService(ctx context.Context, serviceName string) ([]*ServiceInstance, error)

	// Subscribe watches instance changes of the named service.
	//
	//  1. After a successful subscription, an Add event is first sent for
	//     every current instance (full replay), followed by incremental
	//     events. Consumers need not call GetService first.
	//  2. The channel is closed by the implementation only; consumers must
	//     never close it.
	//  3. A closed channel ends the subscription. Causes: ctx cancelled,
	//     Close called, unrecoverable backend error, or a consumer too slow
	//     for the buffer. Unless ctx was cancelled or Close was called, the
	//     consumer should drop its local state and subscribe again.
	//  4. A slow consumer never blocks other subscribers and events are never
	//     dropped silently: when the buffer is full, that subscription's
	//     channel is closed instead.
	//  5. Failure to establish the subscription (auth, network, ...) is
	//     reported through the returned error.
	// Subscribe 订阅指定服务的实例变更。
	//
	//  1. 订阅成功后，先对每个现存实例发一条 Add 事件（全量回放），再发增量事件，
	//     消费方无需先调 GetService.
	//  2. 频道只由实现方关闭；消费方绝不能关闭它.
	//  3. 频道关闭即订阅结束。原因：ctx 取消、调用 Close、后端不可恢复错误、或
	//     消费速度慢于缓冲。除非是 ctx 取消或 Close，消费方应丢弃本地状态并重新订阅.
	//  4. 慢消费方不会阻塞其他订阅者，事件也不会被静默丢弃：缓冲满时关闭的是该
	//     订阅的频道.
	//  5. 建立订阅失败（鉴权、网络等）通过返回的 error 上报.
	Subscribe(ctx context.Context, serviceName string) (<-chan Event, error)

	// Close closes every live subscription (their channels are closed) and
	// releases resources. It is idempotent.
	// Close 关闭所有活跃订阅（其频道被关闭）并释放资源，幂等.
	io.Closer
}
