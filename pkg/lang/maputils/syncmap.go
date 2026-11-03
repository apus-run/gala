package maputils

import "sync"

// SyncMap is a generic wrapper of sync.Map.
// Note that K must be comparable, and be careful when using pointers as K:
// with a pointer key, whether two keys are equal depends only on their
// addresses, not the values the addresses point to. See the Load tests.
// Also note: a missing key and a key present with a zero value (e.g. nil) are
// two different things.
// SyncMap 是对 sync.Map 的一个泛型封装。
// 要注意，K 必须是 comparable 的，并且谨慎使用指针作为 K：
// 使用指针的情况下，两个 key 是否相等，仅仅取决于它们的地址，而不是地址指向的值，可参考 Load 测试。
// 注意，key 不存在和 key 存在但值恰好为零值（如 nil）是两码事。
type SyncMap[K comparable, V any] struct {
	sm sync.Map
}

// Load loads the value for a key | Load 加载键值对.
func (m *SyncMap[K, V]) Load(key K) (value V, ok bool) {
	var anyVal any
	anyVal, ok = m.sm.Load(key)
	if anyVal != nil {
		value = anyVal.(V)
	}
	return
}

// Store stores a key-value pair | Store 存储键值对.
func (m *SyncMap[K, V]) Store(key K, value V) {
	m.sm.Store(key, value)
}

// LoadOrStore loads or stores a key-value pair; loaded is true when the value
// was loaded, false when a store happened.
// LoadOrStore 加载或存储一个键值对；loaded 为 true 代表是加载的，false 代表执行了 store。
func (m *SyncMap[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	var anyVal any
	anyVal, loaded = m.sm.LoadOrStore(key, value)
	if anyVal != nil {
		actual = anyVal.(V)
	}
	return
}

// LoadOrStoreFunc is an optimization that avoids creating instances
// needlessly. It pays off when initialization is expensive; the cost is one
// extra Load call when the key is missing. It returns the error from fn.
// LoadOrStoreFunc 是一个优化，能避免无意义的创建实例：
// 如果初始化过程非常消耗资源，使用这个方法是有价值的，
// 代价是 key 不存在的时候会多一次 Load 调用；fn 返回 error 时它也会返回 error。
func (m *SyncMap[K, V]) LoadOrStoreFunc(key K, fn func() (V, error)) (actual V, loaded bool, err error) {
	val, ok := m.Load(key)
	if ok {
		return val, true, nil
	}
	val, err = fn()
	if err != nil {
		return
	}
	actual, loaded = m.LoadOrStore(key, val)
	return
}

// LoadAndDelete loads and deletes a key-value pair | LoadAndDelete 加载并删除一个键值对.
func (m *SyncMap[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	var anyVal any
	anyVal, loaded = m.sm.LoadAndDelete(key)
	if anyVal != nil {
		value = anyVal.(V)
	}
	return
}

// Delete deletes a key-value pair | Delete 删除键值对.
func (m *SyncMap[K, V]) Delete(key K) {
	m.sm.Delete(key)
}

// Range iterates the map; f must not be nil. f takes K and V as their
// concrete types, and returning false from f stops the iteration.
// Range 遍历 map，f 不能为 nil。传入 f 时 K 和 V 直接使用对应的具体类型，
// f 返回 false 则中断遍历。
func (m *SyncMap[K, V]) Range(f func(key K, value V) bool) {
	m.sm.Range(func(key, value any) bool {
		var (
			k K
			v V
		)
		if value != nil {
			v = value.(V)
		}
		if key != nil {
			k = key.(K)
		}
		return f(k, v)
	})
}
