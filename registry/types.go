package registry

import (
	"context"
	"fmt"
	"io"
	"sort"
)

type Listener func(event Event) error
type Registry interface {
	Register(ctx context.Context, ins *ServiceInstance) error
	Deregister(ctx context.Context, ins *ServiceInstance) error

	ListServices(ctx context.Context, name string) ([]ServiceInstance, error)
	Subscribe(listener Listener)

	io.Closer
}

type ServiceInstance struct {
	// ID is the unique instance ID as registered.
	ID string `json:"id"`
	// Name is the service name as registered.
	Name string `json:"name"`
	// Endpoints are endpoint addresses of the service instance.
	// schema:
	//   http://127.0.0.1:8000?isSecure=false
	//   grpc://127.0.0.1:9000?isSecure=false
	Endpoints []string `json:"endpoints"`
	// Version is the version of the compiled.
	Version string `json:"version"`
	// Metadata is the kv pair metadata associated with the service instance.
	Metadata map[string]string `json:"metadata"`

	InitCapacity int64
	MaxCapacity  int64
	IncreaseStep int64
	GrowthRate   float64
}

func (ins *ServiceInstance) String() string {
	return fmt.Sprintf("%s-%s", ins.Name, ins.ID)
}

// Equal returns whether i and o are equivalent.
func (ins *ServiceInstance) Equal(o interface{}) bool {
	if ins == nil && o == nil {
		return true
	}

	if ins == nil || o == nil {
		return false
	}

	t, ok := o.(*ServiceInstance)
	if !ok {
		return false
	}

	if len(ins.Endpoints) != len(t.Endpoints) {
		return false
	}

	sort.Strings(ins.Endpoints)
	sort.Strings(t.Endpoints)
	for j := 0; j < len(ins.Endpoints); j++ {
		if ins.Endpoints[j] != t.Endpoints[j] {
			return false
		}
	}

	if len(ins.Metadata) != len(t.Metadata) {
		return false
	}

	for k, v := range ins.Metadata {
		if v != t.Metadata[k] {
			return false
		}
	}

	return ins.ID == t.ID && ins.Name == t.Name && ins.Version == t.Version
}

type EventType int

const (
	EventTypeUnknown EventType = iota
	EventTypeAdd
	EventTypeDelete
)

func (e EventType) IsAdd() bool {
	return e == EventTypeAdd
}

func (e EventType) IsDelete() bool {
	return e == EventTypeDelete
}

type Event struct {
	Type     EventType
	Instance ServiceInstance
}
