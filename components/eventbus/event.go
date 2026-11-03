package eventbus

import (
	"crypto/rand"
	"encoding/json"
	"maps"
	"sync/atomic"
	"time"
)

var fallbackIDCounter atomic.Uint64

// EventType is the type marker of an event (e.g. "user.created") | EventType 是事件的类型标记（如 "user.created"）。
type EventType string

// Event represents a generic event that can carry any type of data. Concurrent
// publishers isolate the Event and Metadata per handler; handlers must treat
// Data as immutable unless the payload provides its own synchronization.
// Event 是可携带任意类型数据的通用事件。并发发布时会为每个 handler 隔离
// Event 与 Metadata；除非负载自带同步，handler 必须把 Data 当作不可变。
type Event struct {
	// ID is a unique identifier for the event | 事件的唯一标识。
	ID string `json:"id"`

	// Type identifies the kind of event (e.g., "email.received", "user.created") | 事件的种类标识（如 "email.received"、"user.created"）。
	Type EventType `json:"type"`

	// Source identifies where the event originated from | 事件来源的标识。
	Source string `json:"source"`

	// Data contains the event payload (can be any type) | 事件负载（可以是任意类型）。
	Data any `json:"data"`

	// Metadata contains additional information about the event | 事件的附加信息。
	Metadata map[string]string `json:"metadata,omitempty"`

	// Timestamp when the event was created | 事件的创建时间。
	Timestamp time.Time `json:"timestamp"`

	// Priority of the event (higher = more important) | 事件的优先级（越高越重要）。
	Priority int `json:"priority"`
}

// NewEvent creates a new event with the given type and data | NewEvent 用给定的类型与数据创建新事件。
func NewEvent(eventType EventType, data any) *Event {
	return &Event{
		ID:        generateEventID(),
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now(),
		Priority:  0,
		Metadata:  make(map[string]string),
	}
}

// WithSource sets the source of the event | WithSource 设置事件的来源。
func (e *Event) WithSource(source string) *Event {
	e.Source = source
	return e
}

// WithPriority sets the priority of the event | WithPriority 设置事件的优先级。
func (e *Event) WithPriority(priority int) *Event {
	e.Priority = priority
	return e
}

// WithMetadata adds metadata to the event | WithMetadata 向事件添加一条元数据。
func (e *Event) WithMetadata(key, value string) *Event {
	if e.Metadata == nil {
		e.Metadata = make(map[string]string)
	}
	e.Metadata[key] = value
	return e
}

// GetData unmarshals the event data into the provided type | GetData 将事件数据反序列化到提供的类型。
func (e *Event) GetData(v any) error {
	// nothing to decode when data is absent
	// 数据为空时无需解码
	if e.Data == nil {
		return nil
	}

	// marshal and unmarshal through JSON for type conversion
	// 通过 JSON 序列化/反序列化完成类型转换
	jsonData, err := json.Marshal(e.Data)
	if err != nil {
		return err
	}

	return json.Unmarshal(jsonData, v)
}

// Clone creates a handler-safe copy of the event. Metadata is cloned; Data is
// shared and must be treated as immutable by handlers.
// Clone 创建 handler 安全的事件副本。Metadata 会被克隆；Data 为共享，handler 必须将其当作不可变。
func (e *Event) Clone() *Event {
	if e == nil {
		return nil
	}

	cloned := *e
	cloned.Metadata = maps.Clone(e.Metadata)
	return &cloned
}

// generateEventID generates a unique event ID | generateEventID 生成唯一的事件 ID。
func generateEventID() string {
	return time.Now().Format("20060102150405.000000") + randomString(8)
}

// randomString generates a random string of the given length | randomString 生成给定长度的随机字符串。
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		sequence := fallbackIDCounter.Add(1)
		for i := len(b) - 1; i >= 0; i-- {
			b[i] = charset[sequence%uint64(len(charset))]
			sequence /= uint64(len(charset))
		}
		return string(b)
	}
	for i, value := range b {
		b[i] = charset[int(value)%len(charset)]
	}
	return string(b)
}
