# ecache

进程内过期缓存：LRC / LRU 淘汰 + 每条目 TTL + 容量上限。读写由内部互斥锁保护，不启动任何 goroutine，实现只依赖标准库。接口兼容 `hashicorp/golang-lru/v2/simplelru` 的 `LRUCache`，可从 `simplelru`、`expirable` 迁移过来，只需改构造那一行。

## 安装

要求 **Go 1.25 或更高版本**。

```sh
go get github.com/apus-run/gala/components/ecache
```

以下接口与示例对应当前仓库源码。使用尚未发布的变更时，在应用的 `go.mod` 中通过 `replace` 指向本地 `components/ecache` 模块。

## 接口签名

```go
func New[K comparable, V any](opts ...Option) Cache[K, V]

func DefaultOptions() *Options
func Apply(opts ...Option) *Options

type Cache[K comparable, V any] interface {
    fmt.Stringer
    Add(key K, value V) bool
    Set(key K, value V, ttl time.Duration)
    Get(key K) (V, bool)
    GetExpiration(key K) (time.Time, bool)
    GetOldest() (K, V, bool)
    Contains(key K) bool
    Peek(key K) (V, bool)
    Values() []V
    Keys() []K
    Len() int
    Remove(key K) bool
    Invalidate(key K)
    InvalidateFn(fn func(key K) bool)
    RemoveOldest() (K, V, bool)
    DeleteExpired()
    Purge()
    Resize(size int) int
    Stat() Stats
}
```

`Set` 的时间参数传 0 表示使用缓存级 TTL，传具体值则覆盖它。`Add` 使用缓存级 TTL，返回本次是否发生淘汰。

## Option

| 选项 | 说明 | 默认值 |
| --- | --- | --- |
| `WithTTL(time.Duration)` | 所有条目的默认 TTL，非正数视为不过期 | 10 年（等于不过期） |
| `WithMaxKeys(int)` | 最多保留的条目数，0 表示不限 | 0 |
| `WithLRU()` | 淘汰模式改为 LRU | LRC |
| `WithOnEvicted(func(K, V))` | 自动淘汰和手动删除时的回调 | 无 |

`Option` 不是泛型，`K`/`V` 只出现在 `WithOnEvicted`，由传入的函数推断；回调的键值类型必须与缓存一致，否则在首次淘汰时 panic。选项按传入顺序应用，后者覆盖前者。`DefaultOptions` 返回独立默认值，`Apply` 按顺序原地应用。

`WithTTL(0)` 表示不过期，与仓库里其它 cache 组件（`cache/lru/v2`、`cache/memory/v2`）的语义一致。

## 快速开始

```go
package main

import (
    "fmt"
    "time"

    "github.com/apus-run/gala/components/ecache"
)

func main() {
    c := ecache.New[string, string](
        ecache.WithTTL(100*time.Millisecond),
        ecache.WithMaxKeys(3),
        ecache.WithOnEvicted(func(key, value string) {
            fmt.Printf("evicted: %s=%s\n", key, value)
        }),
    )

    c.Set("user:1", "alice", 0) // 0 表示用缓存级 TTL
    v, ok := c.Get("user:1")
    fmt.Println("hit:", ok, v)

    time.Sleep(120 * time.Millisecond)
    v, ok = c.Get("user:1") // 已过期：值仍在，ok=false
    fmt.Println("expired:", ok, v)

    c.Set("user:2", "bob", time.Minute) // 顺带清掉最旧的那条（已过期的 user:1）
    fmt.Println(c)
}
```

输出：

```text
hit: true alice
expired: false alice
evicted: user:1=alice
Size: 1, Stats: {Hits:1 Misses:1 Added:2 Evicted:1} (50.0%)
```

## 过期与淘汰

- 过期不等于删除。组件没有后台任务，过期条目在有人碰到之前一直留在内存里：每次 `Set`/`Add` 会顺带检查并删除**最旧的一条**（仅当它已过期），其余靠 `DeleteExpired` 清理。建议用 `Ticker` 按 TTL 的一半定期调用：

  ```go
  go func() {
      ticker := time.NewTicker(ttl / 2)
      defer ticker.Stop()
      for range ticker.C {
          c.DeleteExpired()
      }
  }()
  ```

- 过期条目的可见性：`Get`/`Peek` 返回 `(原值, false)`——值仍是写入时的值，调用方必须检查 ok；`Contains`、`Values` 视为不存在；`Len`、`Keys`、`GetOldest` 仍然计入它们。
- 容量淘汰：条目数超过 `WithMaxKeys` 时淘汰队尾，不管它是否过期。`Resize(size)` 改变上限并返回被淘汰的条数，传 0 恢复不限。
- 淘汰顺序：默认 LRC（按最后写入时间），`Set` 已存在的 key 会把它移到队首，读操作不影响顺序；`WithLRU()` 改成 LRU，读也移到队首。
- `WithOnEvicted` 在容量淘汰、过期清理、`Remove`、`Invalidate`、`InvalidateFn`、`Purge` 时都会回调。

## 统计与并发

`Stat()` 返回 `Stats{Hits, Misses, Added, Evicted}`；`String()`（`%v`/`%+v`）打印条目数、统计和命中率，没有查询记录时命中率显示 `0.0%`。

缓存可以并发使用，读写都会加锁。goroutine 与定时清理由调用方负责，组件自身不持有连接、文件等资源，不需要 `Close`。

---

改写自 [go-pkgz/expirable-cache](https://github.com/go-pkgz/expirable-cache) v3（MIT）。
