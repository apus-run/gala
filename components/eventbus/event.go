package eventbus

import (
	"crypto/rand"
	"encoding/json"
	"maps"
	"sync/atomic"
	"time"
)

var fallbackIDCounter atomic.Uint64

type EventType string

// Event represents a generic event that can carry any type of data. Concurrent
// publishers isolate the Event and Metadata per handler; handlers must treat
// Data as immutable unless the payload provides its own synchronization.
type Event struct {
	// ID is a unique identifier for the event
	ID string `json:"id"`

	// Type identifies the kind of event (e.g., "email.received", "user.created")
	Type EventType `json:"type"`

	// Source identifies where the event originated from
	Source string `json:"source"`

	// Data contains the event payload (can be any type)
	Data any `json:"data"`

	// Metadata contains additional information about the event
	Metadata map[string]string `json:"metadata,omitempty"`

	// Timestamp when the event was created
	Timestamp time.Time `json:"timestamp"`

	// Priority of the event (higher = more important)
	Priority int `json:"priority"`
}

// NewEvent creates a new event with the given type and data
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

// WithSource sets the source of the event
func (e *Event) WithSource(source string) *Event {
	e.Source = source
	return e
}

// WithPriority sets the priority of the event
func (e *Event) WithPriority(priority int) *Event {
	e.Priority = priority
	return e
}

// WithMetadata adds metadata to the event
func (e *Event) WithMetadata(key, value string) *Event {
	if e.Metadata == nil {
		e.Metadata = make(map[string]string)
	}
	e.Metadata[key] = value
	return e
}

// GetData unmarshals the event data into the provided type
func (e *Event) GetData(v any) error {
	// If data is already the right type, try direct assignment
	if e.Data == nil {
		return nil
	}

	// Marshal and unmarshal through JSON for type conversion
	jsonData, err := json.Marshal(e.Data)
	if err != nil {
		return err
	}

	return json.Unmarshal(jsonData, v)
}

// Clone creates a handler-safe copy of the event. Metadata is cloned; Data is
// shared and must be treated as immutable by handlers.
func (e *Event) Clone() *Event {
	if e == nil {
		return nil
	}

	cloned := *e
	cloned.Metadata = maps.Clone(e.Metadata)
	return &cloned
}

// generateEventID generates a unique event ID
func generateEventID() string {
	return time.Now().Format("20060102150405.000000") + randomString(8)
}

// randomString generates a random string of the given length
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
