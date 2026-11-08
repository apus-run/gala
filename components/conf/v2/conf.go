// Package conf loads an ordered set of configuration sources into a typed
// struct, and reloads atomically: a batch is either fully published or
// discarded, and the previously published value is kept on failure.
//
// Package conf 按声明顺序把多个来源合并为强类型结构体，并原子重载：
// 一批要么整体发布，要么整体丢弃并保留上一份有效配置。
package conf

import (
	"context"
	"reflect"
	"slices"
	"sync/atomic"
)

// Source produces one layer of configuration as a nested map. Later sources
// in Open override earlier ones. Load must return data the caller may keep:
// the package never mutates returned maps.
//
// Source 产出一层嵌套 map 形式的配置。Open 中靠后的来源覆盖靠前的。
// Load 返回的数据须归调用方所有：本包不修改返回的 map。
type Source interface {
	Load(context.Context) (map[string]any, error)
}

// Config holds the currently published value of T. It is safe for concurrent
// use; Current never blocks on I/O.
//
// Config 持有 T 当前已发布的值。并发安全；Current 不做任何 I/O。
type Config[T any] struct {
	sources []Source
	cur     atomic.Pointer[T]
	// sem is a capacity-1 semaphore so that a waiting Reload can be
	// cancelled, which a sync.Mutex cannot do.
	// sem 是容量为 1 的信号量，使等待中的 Reload 可被取消（Mutex 做不到）。
	sem chan struct{}
}

// Open loads all sources once and returns a usable Config, or an error and a
// nil Config. It starts no goroutines.
//
// Open 同步加载一次所有来源。成功返回可用的 Config，失败返回 nil 与错误。
// 不启动任何 goroutine。
func Open[T any](ctx context.Context, sources ...Source) (*Config[T], error) {
	if err := checkType[T](); err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, newError(StageConfig, -1, "", "", nil)
	}
	for i, s := range sources {
		if isNilSource(s) {
			return nil, newError(StageConfig, i, "", "", nil)
		}
	}
	c := &Config[T]{sources: slices.Clone(sources), sem: make(chan struct{}, 1)}
	if err := c.Reload(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// Current returns the published value. The result is read-only: callers
// must not modify it or any map, slice or pointer reachable from it.
//
// Current 返回已发布的值。结果只读：调用方不得修改它或其可达的
// map、slice、指针。
func (c *Config[T]) Current() *T {
	return c.cur.Load()
}

// Reload rebuilds the value from all sources and publishes it only if every
// step succeeds. Concurrent calls queue; a call cancelled while waiting
// returns without reading any source.
//
// Reload 从所有来源重建值，且仅在每一步都成功时发布。并发调用排队；
// 等待中被取消的调用直接返回，不读取来源。
func (c *Config[T]) Reload(ctx context.Context) error {
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return newError(StageCanceled, -1, "", "", ctx.Err())
	}
	defer func() { <-c.sem }()

	if err := ctx.Err(); err != nil {
		return newError(StageCanceled, -1, "", "", err)
	}

	merged := map[string]any{}
	for i, s := range c.sources {
		if err := ctx.Err(); err != nil {
			return newError(StageCanceled, i, "", "", err)
		}
		layer, err := s.Load(ctx)
		if err != nil {
			return wrapSourceErr(i, err)
		}
		if err := mergeInto(merged, layer); err != nil {
			return newError(StageMerge, i, "", "", err)
		}
	}

	var next T
	if err := bind(merged, &next); err != nil {
		return err
	}
	if v, ok := any(&next).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			// Business validation text is exposed by design (spec §6): the
			// business owns its safety. See newValidateError.
			// 业务校验文本按规格暴露：安全性由业务保证。见 newValidateError。
			return newValidateError(err)
		}
	}
	if err := ctx.Err(); err != nil {
		return newError(StageCanceled, -1, "", "", err)
	}

	c.cur.Store(&next)
	return nil
}

// checkType rejects T that is not a struct value type.
// checkType 拒绝非结构体值类型的 T。
func checkType[T any]() error {
	t := reflect.TypeFor[T]()
	if t.Kind() != reflect.Struct {
		return newError(StageConfig, -1, "", "", nil)
	}
	return nil
}

func isNilSource(s Source) bool {
	if s == nil {
		return true
	}
	v := reflect.ValueOf(s)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}
