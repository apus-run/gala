# fileutils

标准库 `os` / `path/filepath` 之上的文件系统工具集。

```go
import "github.com/apus-run/gala/pkg/fileutils"
```

零第三方依赖、零全局状态、零后台 goroutine。

## 设计原则

1. **只保留比标准库多做了事的函数。** 多做了事 = 自动建父目录、标准库没有的判断、算法（扫描/拷贝/glob/体积格式化）、环境探测。
   纯别名（`Open`、`Stat`、`Chmod`、`Rename`、`Join`、`Abs` …）一律不提供，直接用 `os` / `filepath`。
2. **错误原样返回。** `*os.PathError` 已带操作名和路径，不再二次包装，`errors.Is(err, fs.ErrNotExist)` 开箱可用。
3. **不静默吞错。** 「出错返回空值」的写法会掩盖问题：能报错的返回 error，做不到的函数不提供。
4. **选项用结构体，不用机制。** `CopyOption`、`ScanOptions` 是普通值；没有 TTL、watcher、环境变量配置、`init()`。

## 职责边界

一个包、九个源文件，每个文件只回答一个问题，彼此之间只有调用、没有共享状态：

| 文件 | 职责 | 对外 API |
|---|---|---|
| `path.go` | 查询路径本身 | `Exists` `IsDir` `IsFile` `IsEmpty` `IsReadable` `IsWritable` `RealPath` `Name` `ExtName` |
| `io.go` | 读写内容 | `Create` `WriteFile` `AppendFile` `Lines` |
| `replace.go` | 读-改-写（建立在 `io` + `scan` 之上） | `ReplaceFile` `ReplaceDir` |
| `scan.go` | 列目录 | `Scan` `ScanOptions` `SortFiles` |
| `match.go` | 路径 glob（支持 `**`） | `MatchGlob` |
| `copy.go` | 拷贝文件/目录树 | `Copy` `CopyOption` |
| `size.go` | 体积的格式化与解析 | `FormatSize` `ParseSize` |
| `cache.go` | 带失效检测的读缓存 | `Cache` |
| `search.go` | 在候选目录中定位文件 | `Search` `SelfPath` |

没有拆成多个包：它们都是几十行的无状态函数，共享同一个概念（路径），拆包只会增加 import 与循环依赖的风险。

## 用法

```go
// 写文件：自动建父目录；string 与 []byte 都行
_ = fileutils.WriteFile("/tmp/app/conf/a.yaml", "name: demo")
_ = fileutils.AppendFile("/tmp/app/app.log", []byte("started\n"))

// 流式读行；break 会关闭文件
for line, err := range fileutils.Lines("/var/log/app.log") {
	if err != nil {
		return err
	}
	handle(line)
}

// 递归找 .go 与 .md，只要文件，再用 ** 精筛路径
files, err := fileutils.Scan("./src", fileutils.ScanOptions{
	Patterns:  []string{"*.go", "*.md"},
	Recursive: true,
	FilesOnly: true,
	Filter: func(p string, _ fs.DirEntry) bool {
		ok, _ := fileutils.MatchGlob("**/internal/**", filepath.ToSlash(p))
		return !ok
	},
})

// 批量替换
err = fileutils.ReplaceDir("./docs", func(_, s string) string {
	return strings.ReplaceAll(s, "v1", "v2")
}, fileutils.ScanOptions{Patterns: []string{"*.md"}, Recursive: true})

// 拷贝（目录树、保留权限；Sync 落盘）
err = fileutils.Copy("./tpl", "./out", fileutils.CopyOption{Sync: true})

// 带失效检测的读缓存
var cache fileutils.Cache
b, err := cache.ReadFile("./templates/index.html")

// 体积
fileutils.FormatSize(1536)           // "1.50K"
n, _ := fileutils.ParseSize("64 MB") // 67108864
```

## 有意不做

- 原子写（temp + rename）。需要时按需单独加，不预先设计。

## 已知限制与取舍

这些是明知的、写在这里供使用者和后续维护者判断，不是遗漏。

**API 形态**

- `Copy`、`Scan`、`ReplaceDir` 的选项是可变参数（`opt ...T`），为的是零配置调用最简洁；传入多个时**只有第一个生效**。
  若要杜绝这种歧义，可改为必填结构体，或在不引入跨 module 依赖的前提下自行引入函数式选项。
- `Scan` 一次性收集并排序全部结果：超大目录树会占内存，也不能提前终止。
  需要流式遍历时直接用 `fs.WalkDir`。
- `Scan` 遇到第一个读取错误（如无权限的子目录）即中止并返回错误，没有「跳过继续」的开关。

**行为约定**

- `Copy` 默认保留源的权限位；显式给 `Mode` 时，新建目录至少保留 owner 的 x 位——没有它目录无法进入，产物不可用。
- `Copy` 与 `Scan` 都不跟随符号链接（根路径本身是链接除外）；目录内的链接与特殊文件在拷贝时被跳过。
- `ReplaceFile` 对不存在的文件返回 `fs.ErrNotExist`，不会把空文件写出来；内容未变化时不重写，mtime 不动。

