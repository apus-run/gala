package plugin

import (
	"fmt"
	"reflect"
	"sync"
)

// Container lets plugins that never import each other share services by type.
// The zero value is usable and it is safe for concurrent use.
// Container 让互不 import 的插件按类型共享服务。零值可用，并发安全。
// 通常作为宿主的一个字段：
//
//	type App struct{ Services plugin.Container }
//
// Note: the key is the static type T. To let others fetch by interface,
// Provide must spell the interface out explicitly.
// 注意：键是 T 的静态类型。想让别人按接口取，Provide 时必须显式写出接口：
//
//	plugin.Provide[Tracer](&s.Services, myTracer) // 别人用 Inject[Tracer]
//	plugin.Provide(&s.Services, myTracer)         // 键是 *myTracer，Inject[Tracer] 取不到
type Container struct {
	mu sync.RWMutex
	m  map[reflect.Type]any
}

// Provide registers v under the key T. It returns an error when v is nil (in
// the interface sense; a typed nil slips through) or the type is already
// provided; a plugin can return it directly to abort the install flow.
// Provide 以 T 为键登记 v。v 为 nil（接口意义上的；typed nil 挡不住）或
// 同一类型已被登记时返回错误，插件可以直接 return 它，让安装流程中止。
func Provide[T any](c *Container, v T) error {
	key := reflect.TypeFor[T]()
	if any(v) == nil {
		return fmt.Errorf("plugin: provide nil %v", key)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, dup := c.m[key]; dup {
		return fmt.Errorf("plugin: %v already provided", key)
	}
	if c.m == nil {
		c.m = make(map[reflect.Type]any)
	}
	c.m[key] = v
	return nil
}

// Inject fetches the value registered under the key T | Inject 取出以 T 为键登记的值。
func Inject[T any](c *Container) (T, bool) {
	c.mu.RLock()
	v, ok := c.m[reflect.TypeFor[T]()]
	c.mu.RUnlock()
	// When unregistered, v is nil and t is the zero value
	// 未登记时 v 为 nil，t 是零值
	t, _ := v.(T)
	return t, ok
}
