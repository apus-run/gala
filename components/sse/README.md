# sse

只依赖标准库的 Server-Sent Events（`text/event-stream`）：服务端 `Writer`，客户端 `Reader`，加一个读取 `Last-Event-ID` 的函数。

## 定位与非目标

- 不依赖 gala 的 `server`，不绑定 `ServeMux`、Gin、Chi、Echo：服务端只需要一个 `http.ResponseWriter`，客户端只需要一个 `io.Reader`。
- 不启动任何 goroutine，没有 hub、broker、订阅者管理，也不自动重连。心跳、广播、断线重连都由调用方用几行代码自己写。
- 只做 SSE 协议本身：事件分帧、解析、断点 ID。

## 安装

要求 **Go 1.27 或更高版本**（与仓库其余组件一致；代码本身只用到 Go 1.20 的 `http.ResponseController`）。

```sh
go get github.com/apus-run/gala/components/sse
```

## 接口签名

```go
type Event struct {
    ID    string        // 为空则不写 id 行
    Type  string        // 为空即默认的 "message"
    Data  []byte        // 任何换行（LF、CRLF、CR）都拆成独立的 data 行
    Retry time.Duration // 毫秒精度，零值不发送；只有 Retry 的 Event 只发 retry 行
}

// 服务端
func NewWriter(w http.ResponseWriter) (*Writer, error)
func (w *Writer) Send(e Event) error
func (w *Writer) SendJSON(v any) error     // = Send(Event{Data: json.Marshal(v)})
func (w *Writer) Comment(text string) error // 注释行，用作心跳
func LastEventID(r *http.Request) string

type Writer struct {
    WriteTimeout time.Duration // 每次写入的上限，零值不限制；首次写入前设置
}

// 客户端
func NewReader(r io.Reader) *Reader
func (r *Reader) Next() (Event, error) // 流正常结束返回 io.EOF
func (r *Reader) LastEventID() string
func (r *Reader) Retry() time.Duration

var ErrFlushUnsupported error // 包装 http.ErrNotSupported
var ErrInvalidField error     // ID 或 Type 含换行（ID 还包括 NUL）
```

## 服务端

```go
func events(w http.ResponseWriter, r *http.Request) {
    sw, err := sse.NewWriter(w)
    if err != nil { // 例如被不支持 Flush 的中间件包装
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    sw.WriteTimeout = 10 * time.Second // 防止客户端不再读取时把 handler 阻塞到永远

    resume := sse.LastEventID(r) // 重连客户端带来的断点，首次连接为空

    heartbeat := time.NewTicker(15 * time.Second)
    defer heartbeat.Stop()
    for {
        select {
        case <-r.Context().Done(): // 客户端离开
            return
        case <-heartbeat.C:
            if err := sw.Comment("ping"); err != nil {
                return
            }
        case msg := <-source(resume):
            if err := sw.Send(sse.Event{ID: msg.ID, Type: "update", Data: msg.JSON}); err != nil {
                return
            }
        }
    }
}
```

`NewWriter` 做四件事：设置 `Content-Type: text/event-stream`、`Cache-Control: no-cache`、`X-Accel-Buffering: no`；清除本次响应的写超时；`Flush` 响应头让客户端立刻看到流已建立。

只要框架暴露的是能 `Flush` 的 `http.ResponseWriter`（直接实现，或通过 `Unwrap` 暴露底层 writer），就能把它传给 `NewWriter`。带缓冲或压缩的包装层如果两者都不满足，`NewWriter` 返回 `ErrFlushUnsupported`，此时没有写出任何内容，仍可返回普通错误响应。

## 客户端

```go
req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
req.Header.Set("Accept", "text/event-stream")
if last != "" {
    req.Header.Set("Last-Event-ID", last) // 上次保存的 reader.LastEventID()
}
resp, err := http.DefaultClient.Do(req)
if err != nil { /* ... */ }
defer resp.Body.Close()

r := sse.NewReader(resp.Body)
for {
    e, err := r.Next()
    if errors.Is(err, io.EOF) { break } // 服务端正常结束
    if err != nil { /* 读取错误：自行决定是否按 r.Retry() 重连 */ break }
    handle(e)
}
last = r.LastEventID()
```

取消读取：关闭 `resp.Body`，或让 `ctx` 到期，`Next` 会返回底层读取错误。

JSON 负载用 `SendJSON`（与 axum 的 `json_data`、sse-starlette 的自动序列化同一形态：核心 `Data` 保持 `[]byte`，糖方法固定用 `encoding/json/v2`，不引入可插拔序列化器；接收端自己 `json.Unmarshal(e.Data, ...)`）。

## 注意事项

- **写超时**：`http.Server.WriteTimeout` 会切断长连接。`NewWriter` 为本次响应清除它；若想保留保护，设置 `Writer.WriteTimeout` 限制单次写入。`server/http` 默认的 30 秒写超时因此不会影响通过 `NewWriter` 建立的流，包里有对照测试覆盖这一点。
- **Writer 不能比 handler 活得长**：与 `http.ResponseWriter` 的规则一致。`Writer` 本身并发安全，心跳 goroutine 可以和事件循环共用，但 handler 返回前要保证它们已经停止。
- **写入错误是粘性的**：客户端断开后，之后每次 `Send` 和 `Comment` 都直接返回同一个错误。
- **非法字段**：`ID` 或 `Type` 含换行会破坏分帧，`Send` 返回 `ErrInvalidField`，不写出任何内容，流仍可继续使用。
- **数据换行**：`Data` 中的 CR、CRLF 都会读回成 LF；其余内容（包括首尾空白、末尾换行）原样往返。
- **`Reader` 的断点语义**：`LastEventID()` 在带有效 id 的块以空行结束时更新（包括纯 id 块，空 id 会清空断点），所以一个只收到一半就断开的事件不会被跳过（至少一次）。`Event.ID` 是该事件自己的 id 行，没有则为空。
- **其他细节**：单行上限 1 MiB，超过返回 `bufio.ErrTooLong`；流结束时尚未完整的事件（缺结尾空行）按规范丢弃；行尾支持 LF、CRLF、CR；开头的 BOM 会被忽略。
- **反向代理**：`X-Accel-Buffering: no` 对 nginx 有效；其他代理如需关闭缓冲，请自行配置。长时间无数据时用 `Comment` 定期发心跳。
