// Package atomic provides a generic value holder guarded by an RWMutex for
// safe concurrent reads and writes.
// 包 atomic 提供一个由 RWMutex 保护的泛型值容器，可安全并发读写。
package atomic

import (
	"sync"
)

// Value is an atomic value holder with an internal RWMutex to avoid
// concurrent writes.
// Value 是一个原子值容器，内部用 RWMutex 避免并发写冲突。
//
// Usage | 用法：
//
//	// Actually, we have a NewValue() constructor,
//	// but let's show how to use a Value without it.
//	val := &Value[int]{}
//	val.Set(1)
//	if val.Get() == 1 {
//		log.Println("value is 1")
//	}
type Value[T any] struct {
	value T
	lock  sync.RWMutex
}

// Get locks value mutex and returns current value.
// Get 加读锁并返回当前值。
func (a *Value[T]) Get() T {
	a.lock.RLock()
	defer a.lock.RUnlock()

	return a.value
}

// Set locks value mutex and sets current value.
// Set 加写锁并设置当前值。
func (a *Value[T]) Set(value T) {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.value = value
}

// Context locks value mutex and executes provided function with releasing
// lock in the end.
// Context 加写锁执行传入的函数，并在结束时释放锁；函数内可通过 set 更新当前值。
func (a *Value[T]) Context(c func(value T, set func(value T))) {
	a.lock.Lock()
	defer a.lock.Unlock()

	setter := func(value T) {
		a.value = value
	}

	c(a.value, setter)
}

// NewValue is a constructor of Value, which accepts default value.
// NewValue 是 Value 的构造函数，接受一个默认值。
func NewValue[T any](defaultval T) *Value[T] {
	return &Value[T]{
		value: defaultval,
	}
}
