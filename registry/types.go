// Package registry defines the service registration and discovery abstraction.
// It separates the server-side Registry from the client-side Discovery, so
// that each side only depends on what it actually uses.
//
// Requires Go 1.23+ (slices.Sorted, slices.Values).
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
const (
	MetaWeight       = "weight"
	MetaInitCapacity = "init_capacity"
	MetaMaxCapacity  = "max_capacity"
	MetaIncreaseStep = "increase_step"
	MetaGrowthRate   = "growth_rate"
)

// ServiceInstance describes a single service instance as seen by a registry.
type ServiceInstance struct {
	// ID uniquely identifies the instance. Some backends assign it; in that
	// case Register writes the assigned value back into the instance.
	ID string `json:"id"`
	// Name is the logical service name, e.g. "edu.course.student-api".
	Name string `json:"name"`
	// Version is the service version.
	Version string `json:"version"`
	// Metadata carries arbitrary key/value tags. See the Meta* constants for
	// the keys with agreed meaning.
	Metadata map[string]string `json:"metadata,omitempty"`
	// Endpoints lists reachable addresses with a scheme prefix and optional
	// query, e.g. "grpc://127.0.0.1:9000?isSecure=false".
	Endpoints []string `json:"endpoints"`
}

// String implements fmt.Stringer. It is safe to call on a nil receiver.
func (ins *ServiceInstance) String() string {
	if ins == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s-%s", ins.Name, ins.ID)
}

// Clone returns a deep copy of the instance. Consumers must Clone before
// modifying an instance received from a Discovery.
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
func (ins *ServiceInstance) Equal(o *ServiceInstance) bool {
	if ins == o { // same pointer, or both nil
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
func sameEndpoints(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	return slices.Equal(
		slices.Sorted(slices.Values(a)),
		slices.Sorted(slices.Values(b)),
	)
}

// EventType is the kind of change carried by an Event.
type EventType int

const (
	EventTypeUnknown EventType = iota
	// EventTypeAdd means the instance appeared or was updated. It is an
	// upsert: consumers replace any local entry that has the same ID.
	EventTypeAdd
	// EventTypeDelete means the instance is gone. Consumers match it by ID.
	// Backends that cannot recover the full instance send only its ID and Name.
	EventTypeDelete
)

func (t EventType) IsAdd() bool    { return t == EventTypeAdd }
func (t EventType) IsDelete() bool { return t == EventTypeDelete }

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

// Event describes a change of a service instance.
type Event struct {
	Type EventType
	// Instance is read-only: it may be shared between subscribers. Clone it
	// before modifying.
	Instance *ServiceInstance
}

// Registry is the server-side registration contract.
type Registry interface {
	// Register registers the instance and starts any keepalive needed. If the
	// backend assigns an ID, it is written back to instance.ID. A nil instance
	// returns an error.
	Register(ctx context.Context, instance *ServiceInstance) error
	// Deregister removes the instance and stops its keepalive.
	Deregister(ctx context.Context, instance *ServiceInstance) error
	// Close deregisters every instance still kept alive and releases
	// resources. It is idempotent.
	io.Closer
}

// Discovery is the client-side discovery contract.
type Discovery interface {
	// GetService returns the current instances of a service. The result is
	// read-only; Clone before modifying.
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
	Subscribe(ctx context.Context, serviceName string) (<-chan Event, error)

	// Close closes every live subscription (their channels are closed) and
	// releases resources. It is idempotent.
	io.Closer
}
