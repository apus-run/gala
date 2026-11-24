// Package sse demonstrates streaming Server-Sent Events from a gin handler
// through ginx.Context.EventStream: the handler writes events, never touches
// the response writer directly, and returns ErrNoResponse so the wrapper does
// not append a JSON body after the stream.
// Package sse 演示在 gin handler 里通过 ginx.Context.EventStream 推送 SSE：
// handler 只写事件，不直接碰响应写入器，并返回 ErrNoResponse，
// 避免包装器在流后面再追加 JSON 响应体。
//
// Parameters come from the typed accessors (Query/Param/Cookie → value.AnyValue),
// which is the same path a real endpoint would use. A reconnecting client that
// sends Last-Event-ID resumes after that event.
// 参数取自类型化访问器（Query/Param/Cookie → value.AnyValue），与真实端点一致。
// 带 Last-Event-ID 重连的客户端从该事件之后继续。
package sse

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/apus-run/gala/components/ginx"
	sselib "github.com/apus-run/gala/components/sse"
)

// New builds the demo engine | New 构建示例引擎.
func New() *gin.Engine {
	r := gin.New()
	r.GET("/events/:room", ginx.W(stream))
	// A bare /events has no :room segment, so it gets its own route with the default.
	// 裸 /events 没有 :room 段，单独挂一条用默认值。
	r.GET("/events", ginx.W(stream))
	return r
}

// stream sends ?count events (default 3) with ?delay milliseconds between them
// (default 0), starting after Last-Event-ID when the client reconnects, then
// returns ErrNoResponse: the stream already is the body.
// stream 发送 ?count 个事件（默认 3），间隔 ?delay 毫秒（默认 0），客户端重连时
// 从 Last-Event-ID 之后继续，最后返回 ErrNoResponse——流本身就是响应体。
func stream(ctx *ginx.Context) (ginx.Result, error) {
	// Path parameter first, then the room cookie, then the default.
	// 先取路径参数，再取 room cookie，最后用默认值。
	room := ctx.Param("room").StringOrDefault("")
	if room == "" {
		room = ctx.Cookie("room").StringOrDefault("default")
	}

	// Query and cookie values are strings, so AsInt parses them; IntOrDefault is
	// the strict accessor and would silently fall back for every string input.
	// Query 与 cookie 的值都是字符串，用 AsInt 解析；IntOrDefault 是严格断言版本，
	// 对任何字符串输入都会静默回落到默认值。
	count := 3
	if v, err := ctx.Query("count").AsInt(); err == nil {
		count = v
	}
	delay := 0
	if v, err := ctx.Query("delay").AsInt(); err == nil {
		delay = v
	}
	wait := time.Duration(delay) * time.Millisecond

	// Resume after the last delivered event; the client sends this header on
	// reconnect. 从最后送达的事件之后继续；客户端重连时会带上这个请求头。
	start := 0
	if last := sselib.LastEventID(ctx.Request); last != "" {
		if n, err := strconv.Atoi(last); err == nil {
			start = n + 1
		}
	}

	sw, err := ctx.EventStream()
	if err != nil {
		// Nothing has been written yet, so the wrapper can still render an error.
		// 此时还没写出任何内容，包装器仍能正常输出错误响应。
		return ginx.Result{Code: http.StatusInternalServerError, Msg: err.Error()}, err
	}
	// A comment line is a valid frame that clients ignore; here it proves the
	// stream is open before the first event and exercises Comment().
	// 注释行是客户端会忽略的合法帧；这里用它先探活，并顺带覆盖 Comment()。
	if err := sw.Comment("stream open"); err != nil {
		return ginx.Result{}, ginx.ErrNoResponse
	}
	for i := start; i < start+count; i++ {
		event := sselib.Event{
			ID:   strconv.Itoa(i),
			Type: "tick",
			Data: []byte(room + "-" + strconv.Itoa(i)),
		}
		if err := sw.Send(event); err != nil {
			// The client is gone; there is nothing left to say to it.
			// 客户端已断开，已经没有可回复的对象。
			return ginx.Result{}, ginx.ErrNoResponse
		}
		if wait > 0 {
			time.Sleep(wait)
		}
	}
	return ginx.Result{}, ginx.ErrNoResponse
}
