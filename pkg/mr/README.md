# MR：并发处理与结果归并

`mr` 是进程内的泛型 MapReduce 工具，适合处理一批相互独立的输入，并集中归并结果。它负责限制 Mapper 并发数、传递中间结果、取消调度与等待退出，无第三方运行时依赖。

代码改编自 `onexstack/onexmesh/pkg/core/mr`，保留原作者版权声明及 [MIT 授权](LICENSE)。Gala 版本修正了阻塞读写的取消、取消错误类型、通道关闭权和退出等待行为。

## 工作方式

```text
Generate 产生输入 T
        ↓
Map 并发处理：T → 零个、一个或多个 U
        ↓
Reduce 消费结果流 U，输出一个 V
```

Generate 和 Reduce 各执行一次。Mapper 同时运行的数量默认不超过 16，可通过 `WithWorkers` 调整。Reduce 可以边接收边累计，不需要先收集所有中间结果。

## 使用场景

| 场景 | 输入 | Map 的工作 | Reduce 的结果 |
| --- | --- | --- | --- |
| 批量健康检查 | 服务地址 | 请求每个端点，生成检查结果 | 健康、异常、超时列表或统计 |
| 批量资料补全 | 用户或资源 ID | 查询外部服务、转换 DTO | 完整 DTO 列表或按 ID 索引的 map |
| 文件解析与校验 | 文件路径 | 解析文件、计算摘要、检查格式 | 错误清单、分类结果或计数 |
| 分片数据查询 | 分片编号或独立查询条件 | 查询各个分片 | 合并、去重、排序后的结果 |
| 后台数据处理 | 记录或处理任务 | 清洗、过滤、提取字段 | 分组统计、总和或处理报告 |

这些场景的共同点是：单项处理可以独立执行，最终需要消费并归并结果。并发数应结合下游连接池、限流配额和单项资源消耗设置。

### 如何选择工具

| 需求 | 建议 |
| --- | --- |
| 一批同类输入，独立处理后汇总 | `MapReduce` |
| 已有输入 channel，需要并发处理后汇总 | `MapReduceChan` |
| 批量处理，无结果且无需返回处理错误 | `ForEach` |
| 少量独立函数，只需要等待完成或取得首个错误 | `Finish`，或现有 `taskgroup` / `errgroup` |
| 需要显式共享取消 Context，控制一组任务 | `errgroup.WithContext`，回调使用其派生 Context |
| 同一个事件交给多个订阅者 | `components/eventbus` |
| 下游已有批量 API 或 SQL 批量查询 | 优先批量调用，避免 N+1 查询 |
| 很简单的计算或很少的输入 | 普通循环通常更直接；是否并发应通过实际测量决定 |

它没有持久化、跨进程调度、自动重试或断点恢复能力。需要这些能力时，应使用应用层任务管理或消息基础设施。Reduce 的处理速度也会限制流水线吞吐；把所有结果放进一个 slice 或 map，仍会消耗与结果规模相应的内存。

## 快速开始

下面是完整的平方和示例：

```go
package main

import (
    "context"
    "fmt"

    "github.com/apus-run/gala/pkg/mr"
)

func main() {
    ctx := context.Background()
    total, err := mr.MapReduce(
        func(source chan<- int) {
            for i := 1; i <= 100; i++ {
                source <- i
            }
        },
        func(item int, writer mr.Writer[int], _ func(error)) {
            writer.Write(item * item)
        },
        func(pipe <-chan int, writer mr.Writer[int], _ func(error)) {
            sum := 0
            for value := range pipe {
                sum += value
            }
            writer.Write(sum)
        },
        mr.WithWorkers(8),
        mr.WithContext(ctx),
    )
    if err != nil {
        panic(err)
    }
    fmt.Println(total) // 338350
}
```

泛型参数通常可以从回调推导。上述输入、中间结果和最终结果都是 `int`，实际业务中可以分别使用不同类型。

## 实例：批量健康检查

例如要检查 100 个服务，可以按下面的方式组织处理：

```text
Generate：依次输出服务 URL
Map：最多 8 个并发请求，每项输出 {URL, 状态码, 错误}
Reduce：归并检查结果，统计健康、异常与超时数量
```

Mapper 应把调用方的 Context 传给 HTTP 请求，并配置客户端超时。健康标准按业务协议定义，例如请求没有错误且状态码为 2xx。

健康检查通常需要保留单个端点的失败，再继续检查其他端点。因此请求错误应放进结果 DTO，交给 Reduce 收集，而不是直接调用 `cancel(err)` 中止整批。并发结果没有输入顺序保证；需要按原 URL 列表展示时，在输入和输出中携带索引，由 Reduce 按索引归并。

### 单项失败如何处理

| 策略 | 适合场景 | Mapper 的行为 |
| --- | --- | --- |
| 中止整批 | 必须完整成功的数据转换、严格校验 | `cancel(err)` 后返回 |
| 收集错误并继续 | 健康检查、批量诊断、逐项校验报告 | 将错误放进结果 DTO，再 `writer.Write(result)` |
| 过滤无效项 | 搜索过滤、清洗、只收集匹配记录 | 不写结果，直接返回 |

中止整批时，错误最好包含能识别输入的业务信息。下面的 `Input`、`Output` 和 `transform` 由业务定义：

```go
func(item Input, writer mr.Writer[Output], cancel func(error)) {
    result, err := transform(ctx, item)
    if err != nil {
        cancel(fmt.Errorf("transform input %s: %w", item.ID, err))
        return
    }
    writer.Write(result)
}
```

并发执行时，多个错误的“首个”取决于竞争顺序。已经发生的写库、发送或其他副作用不会回滚，重试与幂等由业务决定。

## API

| API | 说明 |
| --- | --- |
| `MapReduce[T, U, V]` | 使用 Generate 产生输入，并归并为一个结果 |
| `MapReduceChan[T, U, V]` | 消费调用方提供的 channel，归并为一个结果 |
| `ForEach[T]` | 无输出的并发处理；没有 error 返回值，取消错误被忽略，panic 仍传播 |
| `Finish(fns ...func() error)` | 并发执行函数，取得首个错误，返回前等待已启动函数结束 |
| `FinishVoid(fns ...func())` | 并发执行无返回值函数并等待结束，panic 仍传播 |
| `WithWorkers(n)` | 设置 Mapper 并发上限，默认 16，小于 1 时按 1 处理 |
| `WithContext(ctx)` | 用 Context 控制调度和通道写入；nil 使用 Background |

`Finish`、`FinishVoid` 没有 Option 参数，并发上限设为函数数量。它们适合有限、少量任务；大量任务需要可配置并发限制时，使用 `MapReduce`、`ForEach` 或其他有界任务组。

## 顺序与并发安全

- Mapper 输出顺序不保证。需要恢复输入顺序时，携带索引，由 Reduce 按索引收集或排序。
- Mapper 可以写零个、一个或多个中间结果。Reduce 必须写一个最终结果；没有结果返回 `ErrReduceNoOutput`，写多个结果会 panic。
- Mapper 不应直接写共享 map 或 slice；将结果交给 Writer，让 Reduce 在一个回调内归并，或者自行使用同步机制。
- channel 传输不会深拷贝数据。传入 pointer、map、slice 时，应明确所有权，避免发送后继续修改，或多个 Mapper 修改同一对象。
- Writer 只能在对应回调执行期间使用，不要把它交给脱离回调生命周期的 goroutine。

## 取消、关闭与退出

`WithContext` 控制内部调度与读写，回调仍应把调用方 Context 传给 HTTP、RPC 等操作，并设置必要的超时。

- `cancel(err)` 停止后续调度、解除阻塞的内部通道写入；首个取消原因生效。`cancel(nil)` 返回 `ErrCancelWithNil`。
- 父 Context 取消时保留 `context.Cause`，包括 `Canceled`、`DeadlineExceeded` 和自定义原因。
- 内部取消不会取消调用方的 Context，也不会强制终止正在运行的回调。返回前会等待已启动回调结束；回调若永久阻塞，调用也无法结束。
- `MapReduce`、`ForEach` 管理 Generate 创建的输入通道。生成器必须有限并最终返回，不能自行关闭 `source`。取消后框架排空剩余输入，以释放阻塞在发送上的生成器；无法打断生成器中无关的阻塞操作。
- `MapReduceChan` 的输入通道由调用方管理。调用方负责关闭通道、停止生产者；框架在取消后不会排空或关闭该通道。与取消竞争的接收可能已经消费一个输入。
- 内部通道由生产端在写入结束后关闭。回调 panic 后先清理并等待退出，再在调用方 goroutine 重新抛出带堆栈信息的 panic。
- 调用返回错误时，不应依赖返回值取得部分结果。

### 使用调用方的 channel

`source` 应由生产端关闭。即使消费端错误提前返回，生产端也必须能通过 Context 停止：

```go
ctx, cancel := context.WithCancel(parent)
defer cancel()
source := make(chan Input)
producerDone := make(chan struct{})
go func() {
    defer close(producerDone)
    defer close(source)
    for _, item := range inputs {
        select {
        case source <- item:
        case <-ctx.Done():
            return
        }
    }
}()

result, err := mr.MapReduceChan(source, mapper, reducer, mr.WithContext(ctx))
cancel() // 消费端提前失败时，停止仍可能等待发送的生产端。
<-producerDone
```

上述 `parent`、`inputs`、`mapper` 和 `reducer` 由调用方提供。生产端如果包含其他阻塞操作，也需要为那些操作处理退出。

## 测试

`pkg/mr` 是独立 Go module，需要在本目录运行测试；仓库根目录的 `go test ./...` 不会覆盖它。

```sh
cd pkg/mr
go test -race ./...
go vet ./...
```

[example_test.go](example_test.go) 包含可执行的公开 API 示例，[cancellation_test.go](cancellation_test.go) 覆盖取消原因、阻塞发送、输入所有权、退出等待、panic、结果约束与并发上限。
