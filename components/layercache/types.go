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
type Codec interface {
	Marshal(any) ([]byte, error)
	Unmarshal([]byte, any) error
}

// Entry is an immutable serialized snapshot. Local implementations must preserve
// its deadline even if Set is delayed. The coordinator owns the byte slice.
type Entry struct {
	Data      []byte
	ExpiresAt time.Time
}
type RemoteEntry = Entry

// Local is synchronous and concurrent-safe; one instance belongs to one Cache.
// ecache.Cache[string, Entry] satisfies it directly. Eviction never deletes L2.
type Local interface {
	Get(string) (Entry, bool)
	Set(string, Entry, time.Duration)
	Remove(string) bool
}

// Remote reads atomic value/expiry snapshots. Set returns a conservative local
// deadline including backend precision and request latency. A zero deadline is
// invalid; an elapsed deadline means a successful write with no life left for L1.
// All operations must honor context. An error can leave a write outcome unknown.
type Remote interface {
	Get(context.Context, string) (RemoteEntry, error)
	Set(context.Context, string, []byte, time.Duration) (time.Time, error)
	Delete(context.Context, string) error
}

const (
	TypeLocal  = "local"
	TypeRemote = "remote"
	TypeBoth   = "both"
)

type SetMode uint8

const (
	SetAlways SetMode = iota
	SetIfAbsent
	SetIfPresent
)

// ConditionalRemote is the optional atomic NX/XX capability. A rejected write
// returns stored=false and must not alter the existing value or its TTL.
type ConditionalRemote interface {
	SetIf(context.Context, string, []byte, time.Duration, SetMode) (expiresAt time.Time, stored bool, err error)
}

// LeaseRemote atomically acquires a missing lease or renews the same owner's
// lease. It must never replace another live owner's token. Owner IDs must be
// unique per Cache instance. This is refresh coordination, not database fencing.
type LeaseRemote interface {
	AcquireLease(context.Context, string, string, time.Duration) (expiresAt time.Time, acquired bool, err error)
}

// BatchRemote is optional. Returned maps contain only successful snapshots or
// writes; partial successes may accompany an error. Implementations may pipeline
// per-key commands; no cross-key transaction is implied.
type BatchRemote interface {
	MGet(context.Context, []string) (map[string]RemoteEntry, error)
	MSet(context.Context, map[string][]byte, time.Duration) (map[string]time.Time, error)
}

type DoFunc func(context.Context) (any, error)
type Loader[V any] func(context.Context, string) (V, error)

// BatchLoader receives sorted, deduplicated, read-only keys. A nil error makes
// omitted keys authoritative absences; errors never authorize negative caching.
type BatchLoader[V any] func(context.Context, []string) (map[string]V, error)

type EventType uint8

const (
	EventSet EventType = iota + 1
	EventLoad
	EventRefresh
	EventBatch
	EventDelete
)

// Event is an advisory notification, not a distributed consistency protocol.
// Keys are unprefixed business keys; subscribers must check Namespace/SourceID.
type Event struct {
	Namespace string
	SourceID  string
	Type      EventType
	Keys      []string
}
type EventHandler func(context.Context, Event) error
type StatsHandler func(context.Context, Stats) error

// Stats counts backend probes and source executions, not unique HTTP requests.
// Snapshots are cumulative and each counter is atomic, not a transaction.
type Stats struct {
	LocalHits, LocalMisses, RemoteHits, RemoteMisses           uint64
	Loads, LoadErrors, Refreshes, RefreshErrors, DroppedEvents uint64
}

var (
	ErrMiss           = errors.New("layercache: cache miss")
	ErrNotFound       = errors.New("layercache: origin value not found")
	ErrCorrupt        = errors.New("layercache: corrupt or unsupported entry")
	ErrInvalidTTL     = errors.New("layercache: TTL must be finite and positive")
	ErrInvalidated    = errors.New("layercache: result invalidated")
	ErrOverloaded     = errors.New("layercache: shared task limit reached")
	ErrClosed         = errors.New("layercache: closed")
	ErrNotStored      = errors.New("layercache: conditional write rejected")
	ErrUnsupported    = errors.New("layercache: backend capability unsupported")
	ErrInvalidOptions = errors.New("layercache: invalid operation options")
)

// OpError describes a cache stage. Key includes the namespace when present.
type OpError struct {
	Op, Key string
	Err     error
}

func (e *OpError) Error() string { return fmt.Sprintf("layercache: %s %q: %v", e.Op, e.Key, e.Err) }
func (e *OpError) Unwrap() error { return e.Err }

type PanicError struct {
	Value any
	Stack []byte
}

func (e *PanicError) Error() string { return fmt.Sprintf("layercache: task panicked: %v", e.Value) }
