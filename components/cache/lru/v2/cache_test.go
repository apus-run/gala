package lru_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/apus-run/gala/components/cache/lru/v2"
)

func TestCacheNew(t *testing.T) {
	cache := lru.New[string, string]()
	assert.NotNil(t, cache)
}

func TestCache_SetAndGet(t *testing.T) {
	cache := lru.New[string, string]()
	ctx := context.Background()

	// Test setting and getting a value
	err := cache.Set(ctx, "key1", "value1", time.Minute)
	assert.NoError(t, err)

	val, err := cache.Get(ctx, "key1")
	assert.NoError(t, err)
	assert.Equal(t, "value1", val)

	// Test getting a non-existent key
	_, err = cache.Get(ctx, "key2")
	assert.Error(t, err)
}

func TestCache_Expiration(t *testing.T) {
	cache := lru.New[string, string]()
	ctx := context.Background()

	// Set a value with a short expiration
	err := cache.Set(ctx, "key1", "value1", time.Second)
	assert.NoError(t, err)

	time.Sleep(2 * time.Second)

	// Ensure the value has expired
	_, err = cache.Get(ctx, "key1")
	assert.Error(t, err)
}

func TestCache_Delete(t *testing.T) {
	cache := lru.New[string, string]()
	ctx := context.Background()

	// Set and delete a value
	err := cache.Set(ctx, "key1", "value1", time.Minute)
	assert.NoError(t, err)

	err = cache.Delete(ctx, "key1")
	assert.NoError(t, err)

	// Ensure the value is deleted
	_, err = cache.Get(ctx, "key1")
	assert.Error(t, err)
}

func TestCache_Deletes(t *testing.T) {
	cache := lru.New[string, string]()
	ctx := context.Background()

	// Set multiple values
	err := cache.Set(ctx, "key1", "value1", time.Minute)
	assert.NoError(t, err)
	err = cache.Set(ctx, "key2", "value2", time.Minute)
	assert.NoError(t, err)

	// Delete multiple values
	count, err := cache.Deletes(ctx, "key1", "key2")
	assert.NoError(t, err)
	assert.Equal(t, int64(2), count)

	// Ensure the values are deleted
	_, err = cache.Get(ctx, "key1")
	assert.Error(t, err)
	_, err = cache.Get(ctx, "key2")
	assert.Error(t, err)
}

func TestCache_Flush(t *testing.T) {
	cache := lru.New[string, string]()
	ctx := context.Background()

	// Set multiple values
	err := cache.Set(ctx, "key1", "value1", time.Minute)
	assert.NoError(t, err)
	err = cache.Set(ctx, "key2", "value2", time.Minute)
	assert.NoError(t, err)

	// Flush the cache
	err = cache.Flush(ctx)
	assert.NoError(t, err)

	// Ensure all values are deleted
	_, err = cache.Get(ctx, "key1")
	assert.Error(t, err)
	_, err = cache.Get(ctx, "key2")
	assert.Error(t, err)
}

func TestCache_Keys(t *testing.T) {
	cache := lru.New[string, string]()
	ctx := context.Background()

	// Set multiple values
	err := cache.Set(ctx, "key1", "value1", time.Minute)
	assert.NoError(t, err)
	err = cache.Set(ctx, "key2", "value2", time.Minute)
	assert.NoError(t, err)

	// Get all keys
	keys := cache.Keys(ctx)
	assert.ElementsMatch(t, []string{"key1", "key2"}, keys)
}

func TestCache_Contains(t *testing.T) {
	cache := lru.New[string, string]()
	ctx := context.Background()

	// Set a value
	err := cache.Set(ctx, "key1", "value1", time.Minute)
	assert.NoError(t, err)

	// Check if the key exists
	assert.True(t, cache.Contains(ctx, "key1"))

	// Check a non-existent key
	assert.False(t, cache.Contains(ctx, "key2"))
}

func TestCache_Close(t *testing.T) {
	cache := lru.New[string, string]()
	ctx := context.Background()

	// Set a value
	err := cache.Set(ctx, "key1", "value1", time.Minute)
	assert.NoError(t, err)

	// Close the cache
	cache.Close()

	// Ensure the cache is cleared
	_, err = cache.Get(ctx, "key1")
	assert.Error(t, err)
}
