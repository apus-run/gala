package ecache

import (
	"crypto/rand"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/golang-lru/v2/simplelru"
	"github.com/stretchr/testify/assert"
)

func getRand(tb testing.TB) int64 {
	out, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		tb.Fatal(err)
	}
	return out.Int64()
}

func BenchmarkLRU_Rand_NoExpire(b *testing.B) {
	benchmarkRand(b)
}

func BenchmarkLRU_Rand_WithExpire(b *testing.B) {
	benchmarkRand(b, WithTTL(time.Millisecond*10))
}

func BenchmarkLRU_Freq_NoExpire(b *testing.B) {
	benchmarkFreq(b)
}

func BenchmarkLRU_Freq_WithExpire(b *testing.B) {
	benchmarkFreq(b, WithTTL(time.Millisecond*10))
}

// benchmarkRand adds and gets random keys, interleaved
func benchmarkRand(b *testing.B, opts ...Option) {
	l := New[int64, int64](append([]Option{WithLRU(), WithMaxKeys(8192)}, opts...)...)

	trace := make([]int64, b.N*2)
	for i := 0; i < b.N*2; i++ {
		trace[i] = getRand(b) % 32768
	}

	b.ResetTimer()

	var hit, miss int
	for i := 0; i < 2*b.N; i++ {
		if i%2 == 0 {
			l.Add(trace[i], trace[i])
		} else {
			if _, ok := l.Get(trace[i]); ok {
				hit++
			} else {
				miss++
			}
		}
	}
	b.Logf("hit: %d miss: %d ratio: %f", hit, miss, float64(hit)/float64(hit+miss))
}

// benchmarkFreq adds all keys first, then gets them, hot keys being half as frequent
func benchmarkFreq(b *testing.B, opts ...Option) {
	l := New[int64, int64](append([]Option{WithLRU(), WithMaxKeys(8192)}, opts...)...)

	trace := make([]int64, b.N*2)
	for i := 0; i < b.N*2; i++ {
		if i%2 == 0 {
			trace[i] = getRand(b) % 16384
		} else {
			trace[i] = getRand(b) % 32768
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		l.Add(trace[i], trace[i])
	}
	var hit, miss int
	for i := 0; i < b.N; i++ {
		if _, ok := l.Get(trace[i]); ok {
			hit++
		} else {
			miss++
		}
	}
	b.Logf("hit: %d miss: %d ratio: %f", hit, miss, float64(hit)/float64(hit+miss))
}

func TestSimpleLRUInterface(_ *testing.T) {
	var _ simplelru.LRUCache[int, int] = New[int, int]()
}

func TestCacheNoPurge(t *testing.T) {
	lc := New[string, string]()

	k, v, ok := lc.GetOldest()
	assert.Empty(t, k)
	assert.Empty(t, v)
	assert.False(t, ok)

	lc.Add("key1", "val1")
	assert.Equal(t, 1, lc.Len())
	assert.True(t, lc.Contains("key1"))
	assert.False(t, lc.Contains("key2"))

	v, ok = lc.Peek("key1")
	assert.Equal(t, "val1", v)
	assert.True(t, ok)

	k, v, ok = lc.GetOldest()
	assert.Equal(t, "key1", k)
	assert.Equal(t, "val1", v)
	assert.True(t, ok)

	lc.Add("key3", "val3")
	lc.Add("key4", "val4")
	lc.Peek("key3")
	k, v, ok = lc.GetOldest()
	assert.Equal(t, "key1", k)
	assert.Equal(t, "val1", v)
	assert.True(t, ok)

	lc.Add("key1", "val1")
	k, v, ok = lc.GetOldest()
	assert.Equal(t, "key3", k)
	assert.Equal(t, "val3", v)
	assert.True(t, ok)

	v, ok = lc.Peek("key2")
	assert.Empty(t, v)
	assert.False(t, ok)

	assert.Equal(t, []string{"key3", "key4", "key1"}, lc.Keys())
}

func TestCache_Values(t *testing.T) {
	lc := New[string, string](WithMaxKeys(3))

	lc.Add("key1", "val1")
	lc.Add("key2", "val2")
	lc.Add("key3", "val3")

	values := lc.Values()
	if !reflect.DeepEqual(values, []string{"val1", "val2", "val3"}) {
		t.Fatalf("values differs from expected")
	}

	assert.Equal(t, 0, lc.Resize(0))
	assert.Equal(t, 1, lc.Resize(2))
	assert.Equal(t, 0, lc.Resize(5))
	assert.Equal(t, 1, lc.Resize(1))
}

func TestCacheWithPurgeEnforcedBySize(t *testing.T) {
	lc := New[string, string](WithTTL(time.Hour), WithMaxKeys(10))

	for i := range 100 {
		lc.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("val%d", i), 0)
		v, ok := lc.Get(fmt.Sprintf("key%d", i))
		assert.Equal(t, fmt.Sprintf("val%d", i), v)
		assert.True(t, ok)
		assert.True(t, lc.Len() < 20)
	}

	assert.Equal(t, 10, lc.Len())
}

func TestCacheConcurrency(t *testing.T) {
	lc := New[string, string]()
	wg := sync.WaitGroup{}
	wg.Add(1000)
	for i := range 1000 {
		go func(i int) {
			lc.Set(fmt.Sprintf("key-%d", i/10), fmt.Sprintf("val-%d", i/10), 0)
			wg.Done()
		}(i)
	}
	wg.Wait()
	assert.Equal(t, 100, lc.Len())
}

func TestCacheInvalidateAndEvict(t *testing.T) {
	var evicted int
	lc := New[string, string](WithLRU(), WithOnEvicted(func(_ string, _ string) { evicted++ }))

	lc.Set("key1", "val1", 0)
	lc.Set("key2", "val2", 0)
	lc.Set("key3", "val3", 0)

	val, ok := lc.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "val1", val)
	assert.Equal(t, 0, evicted)

	lc.Invalidate("key1")
	assert.Equal(t, 1, evicted)
	val, ok = lc.Get("key1")
	assert.Empty(t, val)
	assert.False(t, ok)

	val, ok = lc.Get("key2")
	assert.True(t, ok)
	assert.Equal(t, "val2", val)

	lc.InvalidateFn(func(key string) bool {
		return key == "key2"
	})
	assert.Equal(t, 2, evicted)
	_, ok = lc.Get("key2")
	assert.False(t, ok)
	assert.Equal(t, 1, lc.Len())

	assert.True(t, lc.Remove("key3"))
	assert.Equal(t, 3, evicted)
	val, ok = lc.Get("key3")
	assert.Empty(t, val)
	assert.False(t, ok)
	assert.False(t, lc.Remove("key3"))
	assert.Zero(t, lc.Len())
}

func TestCacheRemoveOldest(t *testing.T) {
	lc := New[string, string](WithLRU(), WithMaxKeys(2))

	lc.Set("key1", "val1", 0)
	assert.Equal(t, 1, lc.Len())

	v, ok := lc.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "val1", v)

	assert.Equal(t, []string{"key1"}, lc.Keys())
	assert.Equal(t, 1, lc.Len())

	lc.Set("key2", "val2", 0)
	assert.Equal(t, []string{"key1", "key2"}, lc.Keys())
	assert.Equal(t, 2, lc.Len())

	k, v, ok := lc.RemoveOldest()
	assert.Equal(t, "key1", k)
	assert.Equal(t, "val1", v)
	assert.True(t, ok)

	assert.Equal(t, []string{"key2"}, lc.Keys())
	assert.Equal(t, 1, lc.Len())

	k, v, ok = lc.RemoveOldest()
	assert.Equal(t, "key2", k)
	assert.Equal(t, "val2", v)
	assert.True(t, ok)

	k, v, ok = lc.RemoveOldest()
	assert.Empty(t, k)
	assert.Empty(t, v)
	assert.False(t, ok)

	assert.Empty(t, lc.Keys())

}

func TestCacheEvictionMode(t *testing.T) {
	// LRC, the default: oldest written entry goes first, a read doesn't save it
	// LRU: a read moves the entry to the front, so it survives the next eviction
	for _, tc := range []struct {
		name  string
		cache Cache[string, string]
		kept  bool
	}{
		{"lrc", New[string, string](WithMaxKeys(3)), false},
		{"lru", New[string, string](WithMaxKeys(3), WithLRU()), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.cache.Add("key1", "val1")
			tc.cache.Add("key2", "val2")
			tc.cache.Add("key3", "val3")
			tc.cache.Get("key1")
			tc.cache.Add("key4", "val4") // size exceeded, one entry evicted
			assert.Equal(t, tc.kept, tc.cache.Contains("key1"))
		})
	}
}

func TestCacheString(t *testing.T) {
	assert.Equal(t, "Size: 0, Stats: {Hits:0 Misses:0 Added:0 Evicted:0} (0.0%)", New[string, string]().String())
}

func ExampleCache() {
	// make cache with short TTL and 3 max keys
	cache := New[string, string](WithMaxKeys(3), WithTTL(time.Millisecond*100))

	// set value under key1.
	// with 0 ttl (last parameter) will use cache-wide setting instead (100ms).
	cache.Set("key1", "val1", 0)

	// get value under key1
	r, ok := cache.Get("key1")

	// check for OK value, because otherwise return would be nil and
	// type conversion will panic
	if ok {
		fmt.Printf("value before expiration is found: %v, value: %q\n", ok, r)
	}

	time.Sleep(time.Millisecond * 110)

	// get value under key1 after key expiration
	r, ok = cache.Get("key1")
	// don't convert to string as with ok == false value would be nil
	fmt.Printf("value after expiration is found: %v, value: %q\n", ok, r)

	// set value under key2, would evict old entry because it is already expired.
	// ttl (last parameter) overrides cache-wide ttl.
	cache.Set("key2", "val2", time.Minute*5)

	fmt.Printf("%+v\n", cache)
	// Output:
	// value before expiration is found: true, value: "val1"
	// value after expiration is found: false, value: "val1"
	// Size: 1, Stats: {Hits:1 Misses:1 Added:2 Evicted:1} (50.0%)
}

func TestCacheValuesConcurrency(_ *testing.T) {
	lc := New[string, string](WithTTL(time.Hour))
	wg := sync.WaitGroup{}
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := range 1000 {
			lc.Set(fmt.Sprintf("key-%d", i), fmt.Sprintf("val-%d", i), 0)
		}
	}()

	go func() {
		defer wg.Done()
		for i := range 1000 {
			lc.Remove(fmt.Sprintf("key-%d", i))
		}
	}()

	go func() {
		defer wg.Done()
		for range 1000 {
			lc.Values()
		}
	}()

	wg.Wait()
}

func TestCacheAddEvictsOnSize(t *testing.T) {
	lc := New[string, string](WithMaxKeys(2))

	assert.False(t, lc.Add("key1", "val1"), "nothing to evict yet")
	assert.False(t, lc.Add("key2", "val2"), "size not exceeded")
	assert.True(t, lc.Add("key3", "val3"), "size exceeded, oldest entry evicted")
	assert.False(t, lc.Add("key3", "val3-updated"), "existing key updated, nothing evicted")
	assert.Equal(t, 2, lc.Len())
}
