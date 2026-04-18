package plugin

import (
	"sort"
	"sync"
)

var (
	mu       sync.RWMutex
	registry = make(map[string]Plugin)
)

// Register adds a plugin under its Name(). Nil plugins are ignored; registering
// again with the same Name replaces the previous entry.
func Register(p Plugin) {
	if p == nil {
		return
	}
	mu.Lock()
	registry[p.Name()] = p
	mu.Unlock()
}

// All returns every registered plugin, sorted by Name.
func All() []Plugin {
	mu.RLock()
	defer mu.RUnlock()
	plugins := make([]Plugin, 0, len(registry))
	for _, p := range registry {
		plugins = append(plugins, p)
	}
	sort.Slice(plugins, func(i, j int) bool {
		return plugins[i].Name() < plugins[j].Name()
	})
	return plugins
}

// Get returns the plugin registered under name.
func Get(name string) (Plugin, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := registry[name]
	return p, ok
}
