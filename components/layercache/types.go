// Adapted from JetCache-Go; see NOTICE.md and LICENSE.jetcache-go.
package layercache

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Codec must be concurrent-safe. Marshal output must not be reused or mutated
// after return; Unmarshal must not modify its input. JSON is the default.
// Codec 必须并发安全。Marshal 的返回值不得复用或修改；Unmarshal 不得修改输入。
// 默认使用 JSON。
type Codec interface {
	// Marshal serializes v to bytes | 将 v 序列化为字节.
	Marshal(any) ([]byte, error)
	// Unmarshal parses data into v | 将 data 反序列化到 v.
	Unmarshal([]byte, any) error
}

// Entry is an immutable serialized snapshot. Local implementations must preserve
// its deadline even if Set is delayed. The coordinator owns the byte slice.
// Entry 是不可变的序列化快照。即使 Set 被延迟，本地实现也必须保留其截止时间。
// 字节切片归协调器所有。
type Entry struct {
	// Data holds the serialized value | 持有序列化后的值.
	Data []byte
	// ExpiresAt is the absolute expiry of the entry | 条目的绝对过期时间.
	ExpiresAt time.Time
}

// RemoteEntry is an alias of Entry for remote snapshots | RemoteEntry 是远端快照的 Entry 别名.
type RemoteEntry = Entry

// Local is synchronous and concurrent-safe; one instance belongs to one Cache.
// ecache.Cache[string, Entry] satisfies it directly. Eviction never deletes L2.
// Local 是同步且并发安全的；一个实例只属于一个 Cache。
// ecache.Cache[string, Entry] 可直接满足该接口。淘汰绝不清除 L2。
type Local interface {
	// Get returns the entry for key and whether it was present | 返回 key 对应的条目及是否存在.
	Get(string) (Entry, bool)
	// Set stores the entry with a TTL | 以给定 TTL 存储条目.
	Set(string, Entry, time.Duration)
	// Remove deletes the key and reports whether it existed | 删除 key 并报告其原是否存在.
	Remove(string) bool
}

// Remote reads atomic value/expiry snapshots. Set returns a conservative local
// deadline including backend precision and request latency. A zero deadline is
// invalid; an elapsed deadline means a successful write with no life left for L1.
// All operations must honor context. An error can leave a write outcome unknown.
// Remote 读取原子性的值/过期快照。Set 返回保守的本地截止时间，包含后端精度
// 与请求延迟。零值截止时间非法；已过期的截止时间表示写入成功但 L1 已无生命期。
// 所有操作必须遵守 context。出错时写入结果可能未知。
type Remote interface {
	// Get reads the remote snapshot for key | 读取 key 的远端快照.
	Get(context.Context, string) (RemoteEntry, error)
	// Set writes the value and returns the resulting local deadline | 写入值并返回最终的本地截止时间.
	Set(context.Context, string, []byte, time.Duration) (time.Time, error)
	// Delete removes the key from the backend | 从后端删除 key.
	Delete(context.Context, string) error
}

// Cache type labels | 缓存类型标识.
const (
	// TypeLocal marks a local-only cache | 表示仅本地的缓存.
	TypeLocal = "local"
	// TypeRemote marks a remote-only cache | 表示仅远端的缓存.
	TypeRemote = "remote"
	// TypeBoth marks a two-level cache | 表示两级缓存.
	TypeBoth = "both"
)

// SetMode selects the conditional write mode | SetMode 选择条件写入模式.
type SetMode uint8

const (
	// SetAlways writes unconditionally | 无条件写入.
	SetAlways SetMode = iota
	// SetIfAbsent writes only when absent | 仅在不存在时写入（NX）.
	SetIfAbsent
	// SetIfPresent writes only when present | 仅在存在时写入（XX）.
	SetIfPresent
)

// ConditionalRemote is the optional atomic NX/XX capability. A rejected write
// returns stored=false and must not alter the existing value or its TTL.
// ConditionalRemote 是可选的原子 NX/XX 能力。被拒绝的写入返回 stored=false，
// 且不得改动现有值或其 TTL。
type ConditionalRemote interface {
	// SetIf writes under the requested mode and reports whether it was stored | 按指定模式写入，并报告是否存储成功.
	SetIf(context.Context, string, []byte, time.Duration, SetMode) (expiresAt time.Time, stored bool, err error)
}

// LeaseRemote atomically acquires a missing lease or renews the same owner's
// lease. It must never replace another live owner's token. Owner IDs must be
// unique per Cache instance. This is refresh coordination, not database fencing.
// LeaseRemote 原子地获取缺失的租约，或为同一持有者续租。绝不能替换另一个
// 存活持有者的 token。Owner ID 必须在每个 Cache 实例内唯一。这是刷新协调，
// 不是数据库栅栏。
type LeaseRemote interface {
	// AcquireLease acquires a missing lease or renews the owner's own lease | 获取缺失的租约，或为 owner 续租.
	AcquireLease(context.Context, string, string, time.Duration) (expiresAt time.Time, acquired bool, err error)
}

// BatchRemote is optional. Returned maps contain only successful snapshots or
// writes; partial successes may accompany an error. Implementations may pipeline
// per-key commands; no cross-key transaction is implied.
// BatchRemote 可选。返回的映射只含成功的快照或写入；部分成功可伴随错误出现。
// 实现可按 key 管道化命令；不隐含跨 key 事务。
type BatchRemote interface {
	// MGet reads multiple remote snapshots | 批量读取远端快照.
	MGet(context.Context, []string) (map[string]RemoteEntry, error)
	// MSet writes multiple values and returns their deadlines | 批量写入值并返回各自的截止时间.
	MSet(context.Context, map[string][]byte, time.Duration) (map[string]time.Time, error)
}

// DoFunc is the origin-load callback | DoFunc 是回源加载回调.
type DoFunc func(context.Context) (any, error)

// Loader loads one value by key | Loader 按 key 加载单个值.
type Loader[V any] func(context.Context, string) (V, error)

// BatchLoader receives sorted, deduplicated, read-only keys. A nil error makes
// omitted keys authoritative absences; errors never authorize negative caching.
// BatchLoader 接收已排序、去重、只读的 key。错误为 nil 时，省略的 key 是权威的
// "不存在"；出错时绝不触发空值缓存。
type BatchLoader[V any] func(context.Context, []string) (map[string]V, error)

// EventType labels cache events | EventType 标识缓存事件类型.
type EventType uint8

const (
	// EventSet fires after an explicit Set | 显式 Set 后触发.
	EventSet EventType = iota + 1
	// EventLoad fires after an origin fill on the read path | 读路径回源后触发.
	EventLoad
	// EventRefresh fires after a background refresh | 后台刷新后触发.
	EventRefresh
	// EventBatch fires after a batch commit | 批量提交后触发.
	EventBatch
	// EventDelete fires after a remote delete | 远端删除后触发.
	EventDelete
)

// Event is an advisory notification, not a distributed consistency protocol.
// Keys are unprefixed business keys; subscribers must check Namespace/SourceID.
// Event 是通告性通知，不是分布式一致性协议。Keys 为不带前缀的业务 key；
// 订阅方必须自行检查 Namespace/SourceID。
type Event struct {
	// Namespace is the cache namespace | 缓存命名空间.
	Namespace string
	// SourceID identifies the emitting Cache instance | 标识发出事件的 Cache 实例.
	SourceID string
	// Type is the event type | 事件类型.
	Type EventType
	// Keys lists the affected business keys | 受影响的业务 key 列表.
	Keys []string
}

// EventHandler handles cache events | EventHandler 处理缓存事件.
type EventHandler func(context.Context, Event) error

// StatsHandler receives stats snapshots | StatsHandler 接收统计快照.
type StatsHandler func(context.Context, Stats) error

// Stats counts backend probes and source executions, not unique HTTP requests.
// Snapshots are cumulative and each counter is atomic, not a transaction.
// Stats 统计后端探测与回源执行次数，不是独立请求数。快照为累计值，
// 每个计数器原子独立，不构成事务。
type Stats struct {
	// LocalHits/LocalMisses/RemoteHits/RemoteMisses count L1/L2 probes | 统计 L1/L2 的命中与未命中次数.
	LocalHits, LocalMisses, RemoteHits, RemoteMisses uint64
	// Loads/LoadErrors/Refreshes/RefreshErrors/DroppedEvents count origin fills and dropped events | 统计回源次数/错误、后台刷新次数/错误及被丢弃的事件数.
	Loads, LoadErrors, Refreshes, RefreshErrors, DroppedEvents uint64
}

// Sentinel errors | 哨兵错误.
var (
	// ErrMiss reports a cache miss | 表示缓存未命中.
	ErrMiss = errors.New("layercache: cache miss")
	// ErrNotFound reports a value absent at the origin | 表示回源处不存在该值.
	ErrNotFound = errors.New("layercache: origin value not found")
	// ErrCorrupt reports a corrupt or unsupported entry | 表示条目损坏或不受支持.
	ErrCorrupt = errors.New("layercache: corrupt or unsupported entry")
	// ErrInvalidTTL reports a non-finite or non-positive TTL | 表示 TTL 非有限或非正数.
	ErrInvalidTTL = errors.New("layercache: TTL must be finite and positive")
	// ErrInvalidated reports an invalidated result | 表示结果已失效.
	ErrInvalidated = errors.New("layercache: result invalidated")
	// ErrOverloaded reports that the shared task limit was reached | 表示共享任务数已达上限.
	ErrOverloaded = errors.New("layercache: shared task limit reached")
	// ErrClosed reports that the cache is closed | 表示缓存已关闭.
	ErrClosed = errors.New("layercache: closed")
	// ErrNotStored reports a rejected conditional write | 表示条件写入被拒绝.
	ErrNotStored = errors.New("layercache: conditional write rejected")
	// ErrUnsupported reports an unsupported backend capability | 表示后端能力不受支持.
	ErrUnsupported = errors.New("layercache: backend capability unsupported")
	// ErrInvalidOptions reports invalid operation options | 表示操作选项非法.
	ErrInvalidOptions = errors.New("layercache: invalid operation options")
)

// OpError describes a cache stage. Key includes the namespace when present.
// OpError 描述缓存某阶段的错误。Key 存在时包含 namespace。
type OpError struct {
	// Op is the cache stage that failed | 失败的缓存阶段.
	Op string
	// Key is the affected key, including the namespace when present | 受影响的 key，存在时包含 namespace.
	Key string
	// Err is the underlying error | 底层错误.
	Err error
}

// Error implements the error interface | 实现 error 接口.
func (e *OpError) Error() string { return fmt.Sprintf("layercache: %s %q: %v", e.Op, e.Key, e.Err) }

// Unwrap returns the underlying error | 返回底层错误.
func (e *OpError) Unwrap() error { return e.Err }

// PanicError wraps a recovered task panic | PanicError 包装任务中恢复的 panic.
type PanicError struct {
	// Value is the recovered panic value | 恢复到的 panic 值.
	Value any
	// Stack is the captured stack trace | 捕获的调用栈.
	Stack []byte
}

// Error implements the error interface | 实现 error 接口.
func (e *PanicError) Error() string { return fmt.Sprintf("layercache: task panicked: %v", e.Value) }
