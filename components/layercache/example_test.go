package layercache_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/apus-run/gala/components/layercache"
)

func Example() {
	c, err := layercache.New(layercache.WithNamespace("users:v2"))
	if err != nil {
		panic(err)
	}
	defer c.Close()
	users := layercache.NewTyped[string](c)
	for range 2 {
		value, err := users.GetOrLoad(context.Background(), "user:1", func(context.Context, string) (string, error) {
			fmt.Println("回源")
			return "alice", nil
		})
		fmt.Println(value, err)
	}
	// Output:
	// 回源
	// alice <nil>
	// alice <nil>
}
func ExampleTyped_GetOrLoad() {
	c, err := layercache.New(layercache.WithNamespace("users:v2"), layercache.WithNotFoundTTL(time.Second))
	if err != nil {
		panic(err)
	}
	defer c.Close()
	users := layercache.NewTyped[string](c)
	for range 2 {
		_, err := users.GetOrLoad(context.Background(), "404", func(context.Context, string) (string, error) {
			fmt.Println("回源")
			return "", layercache.ErrNotFound
		})
		fmt.Println(errors.Is(err, layercache.ErrNotFound))
	}
	// Output:
	// 回源
	// true
	// true
}
func ExampleT_MGet() {
	c, err := layercache.New(layercache.WithNamespace("users:v2"))
	if err != nil {
		panic(err)
	}
	defer c.Close()
	users := layercache.NewT[int, string](c)
	values, err := users.MGet(context.Background(), "user", []int{2, 1, 2}, func(_ context.Context, ids []int) (map[int]string, error) {
		fmt.Println(ids)
		return map[int]string{1: "alice", 2: "bob"}, nil
	})
	fmt.Println(values[1], values[2], err)
	// Output:
	// [1 2]
	// alice bob <nil>
}

// Applications can compose a reusable profile using only the public Option API.
func ExampleOptionFunc() {
	profile := layercache.OptionFunc(func(o *layercache.Options) {
		o.Apply(layercache.WithLocalCapacity(1000), layercache.WithLocalTTL(time.Minute))
	})
	c, err := layercache.New(layercache.WithNamespace("users"), profile)
	if err != nil {
		panic(err)
	}
	defer c.Close()
	fmt.Println(c.CacheType())
	// Output: local
}

// The byte core accepts operation options when an explicit output pointer is useful.
func ExampleCache_Once() {
	c, err := layercache.New(layercache.WithNamespace("users"))
	if err != nil {
		panic(err)
	}
	defer c.Close()
	var value string
	err = c.Once(context.Background(), "user:1",
		layercache.Value(&value),
		layercache.Do(func(context.Context) (any, error) { return "alice", nil }),
		layercache.TTL(time.Minute),
	)
	fmt.Println(value, err)
	// Output: alice <nil>
}

func ExampleTyped_Set() {
	c, err := layercache.New(layercache.WithNamespace("users"))
	if err != nil {
		panic(err)
	}
	defer c.Close()
	users := layercache.NewTyped[string](c)
	if err := users.Set(context.Background(), "user:1", "alice", layercache.TTL(time.Minute)); err != nil {
		panic(err)
	}
	value, err := users.Get(context.Background(), "user:1")
	fmt.Println(value, err)
	// Output: alice <nil>
}

// Operation profiles use the same interface/adapter pattern as construction profiles.
func ExampleItemOptionFunc() {
	insert := layercache.ItemOptionFunc(func(o *layercache.ItemOptions) {
		o.Apply(layercache.TTL(time.Minute), layercache.SetNX(true))
	})
	c, err := layercache.New(layercache.WithNamespace("users"))
	if err != nil {
		panic(err)
	}
	defer c.Close()
	users := layercache.NewTyped[string](c)
	if err := users.Set(context.Background(), "user:1", "alice", insert); err != nil {
		panic(err)
	}
	rejected := users.Set(context.Background(), "user:1", "bob", insert)
	value, err := users.Get(context.Background(), "user:1")
	if err != nil {
		panic(err)
	}
	fmt.Println(value, errors.Is(rejected, layercache.ErrNotStored))
	// Output: alice true
}
