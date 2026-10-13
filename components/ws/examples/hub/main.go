// 演示：gala ws 组件之上搭一个带 Hub 的通知服务。
//
// 跑起来后测试：
//
//	终端1: go run ./examples/hub
//	终端2: wscat -c "ws://localhost:8080/ws?user_id=alice"
//	       < {"event":"connected","timestamp":...}          ← 连接确认
//	       > {"event":"subscribe","tag":"case-123"}
//	       > {"event":"unsubscribe","tag":"case-123"}
//	       > {"event":"ping"}                                ← 应用层心跳，回 pong
//	终端3: curl "http://localhost:8080/push?user_id=alice&msg=hello"
//	       curl "http://localhost:8080/push?tag=case-123&msg=new-progress"
//	       curl "http://localhost:8080/push?msg=to-all"
//
// Ctrl+C 优雅停机：停止接收新连接并关闭所有在线连接。
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/apus-run/gala/components/ws"
)

// Request 客户端上行封装（简化版：真实协议一般是 event + data 嵌套结构）
type Request struct {
	Event string `json:"event"`
	Tag   string `json:"tag"`
}

// 事件分发表。真实业务里处理器较重时应放进带并发上限的 goroutine 池执行，
// 避免单个事件把读循环占死
var handlers = map[string]func(c *Client, req *Request){
	"subscribe": func(c *Client, req *Request) {
		c.Tags.Store(req.Tag, true)
	},
	"unsubscribe": func(c *Client, req *Request) {
		c.Tags.Delete(req.Tag)
	},
	"ping": func(c *Client, _ *Request) { // 应用层心跳，回 pong
		select {
		case c.Send <- &Message{Event: "pong", Timestamp: time.Now().Unix()}:
		default:
		}
	},
}

func main() {
	hub := NewHub()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		// 演示用 user_id query 参数标识用户；生产环境此处应完成鉴权
		// （建议从 cookie/header 取凭证，别学 token 放 URL 的写法，会泄漏进访问日志）。
		// 允许匿名连接、登录后再绑定用户的场景：先以空 userID 注册，
		// 收到 login 事件时 Unregister + 换 userID 重新 Register 即可。
		userID := r.URL.Query().Get("user_id")

		readLoop := func(ctx context.Context, conn *ws.Conn) {
			client := &Client{
				UserID:    userID,
				Send:      make(chan *Message, 256),
				Done:      make(chan struct{}),
				closeConn: func() { _ = conn.Close() },
			}
			// 发布到 Hub 前先入队，避免并发推送填满缓冲、阻塞写协程启动。
			client.Send <- &Message{Event: "connected", Timestamp: time.Now().Unix()}
			hub.Register(client)
			defer func() {
				hub.Unregister(client)
				client.Close()
			}()
			go client.writeLoop(ctx, conn)

			// 独占读：解析 + 事件分发
			for {
				_, raw, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var req Request
				if json.Unmarshal(raw, &req) != nil || req.Event == "" {
					continue // 坏消息容错，不断连
				}
				if fn, ok := handlers[req.Event]; ok {
					fn(client, &req)
				}
			}
		}

		// 客户端需在窗口内发 ping（gala ws 客户端 ws.WithPing 即可）。
		// 纯浏览器 JS 发不了 ping 控制帧：该场景去掉此选项，改用应用层心跳
		server := ws.NewServer(w, r, readLoop, ws.WithNoClientPingTimeout(time.Minute))
		if err := server.Run(context.Background()); err != nil {
			log.Println("ws server error:", err)
		}
	})

	// HTTP 触发推送，方便联调：tag > user_id > 全体
	http.HandleFunc("/push", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		msg := &Message{Event: "notice", Msg: q.Get("msg"), Timestamp: time.Now().Unix()}
		switch {
		case q.Get("tag") != "":
			hub.PushTag(q.Get("tag"), msg)
		case q.Get("user_id") != "":
			hub.Push(q.Get("user_id"), msg)
		default:
			hub.Broadcast(msg)
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Addr: ":8080"}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	log.Println("listening on :8080")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	log.Println("shutting down...")
	_ = srv.Shutdown(context.Background()) // 停止接收新连接
	hub.CloseAll()                         // 关闭所有在线连接，readLoop 各自完成清理
	log.Println("bye")
}
