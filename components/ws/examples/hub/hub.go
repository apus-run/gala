// Hub 是业务层代码：按 userID 管理在线连接（支持一个用户多设备），
// 演示如何在 gala ws 组件之上实现 IM 语义（注册/注销/多维度推送/事件分发/优雅停机），
// 刻意不放进 ws 包，保持组件库只做传输层。
package main

import (
	"context"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/apus-run/gala/components/ws"
)

// Message 服务端下行封装（真实业务的协议形态各不相同，按需改字段）
type Message struct {
	Event     string `json:"event"`
	Data      any    `json:"data,omitempty"`
	Code      int    `json:"code"`
	Msg       string `json:"msg,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// Client 一个用户连接。Send 交给 writeLoop 独占写 conn
// （gorilla/websocket 不允许并发写，业务侧不能直接拿 conn 写）。
type Client struct {
	UserID string
	Send   chan *Message
	Done   chan struct{} // 连接关闭信号，触发 writeLoop 退出
	Tags   sync.Map      // 该连接订阅的 tag（如订阅某案件/工单的进展），PushTag 用

	closeOnce sync.Once
	closeConn func() // 由 readLoop 在 Register 前设置：关底层 conn，解除 ReadMessage 阻塞
}

// Close 幂等关闭：关 Done（停 writeLoop）并关底层 conn（解除读阻塞）。
// readLoop 的 defer 和 Hub.CloseAll 都会调它，靠 sync.Once 保证只执行一次。
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.Done)
		if c.closeConn != nil {
			c.closeConn()
		}
	})
}

// Hub userID → 连接列表
type Hub struct {
	mu      sync.RWMutex
	clients map[string][]*Client
}

func NewHub() *Hub {
	return &Hub{clients: make(map[string][]*Client)}
}

func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c.UserID] = append(h.clients[c.UserID], c)
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	list := h.clients[c.UserID]
	for i, item := range list {
		if item == c {
			h.clients[c.UserID] = slices.Delete(list, i, i+1)
			break
		}
	}
	if len(h.clients[c.UserID]) == 0 {
		delete(h.clients, c.UserID)
	}
}

// Push 非阻塞推送给该用户的所有在线设备。
// 慢客户端（缓冲满）直接丢弃并记日志：不能让一个慢连接拖垮推送方和其他设备。
func (h *Hub) Push(userID string, msg *Message) {
	h.mu.RLock()
	// Unregister 会原地移动元素，解锁后的遍历必须使用独立快照。
	list := slices.Clone(h.clients[userID])
	h.mu.RUnlock()

	for _, c := range list {
		select {
		case c.Send <- msg:
		case <-c.Done: // 连接已关闭，跳过
		default:
			log.Printf("[hub] push dropped: userID=%s send buffer full", userID)
		}
	}
}

// PushTag 推送给订阅了 tag 的所有连接（如：某案件有新进展）。
func (h *Hub) PushTag(tag string, msg *Message) {
	h.mu.RLock()
	all := make([]*Client, 0, len(h.clients))
	for _, list := range h.clients {
		all = append(all, list...)
	}
	h.mu.RUnlock()

	for _, c := range all {
		if _, ok := c.Tags.Load(tag); !ok {
			continue
		}
		select {
		case c.Send <- msg:
		case <-c.Done:
		default:
			log.Printf("[hub] push dropped: tag=%s send buffer full", tag)
		}
	}
}

// Broadcast 推送给全体在线连接。
func (h *Hub) Broadcast(msg *Message) {
	h.mu.RLock()
	all := make([]*Client, 0, len(h.clients))
	for _, list := range h.clients {
		all = append(all, list...)
	}
	h.mu.RUnlock()

	for _, c := range all {
		select {
		case c.Send <- msg:
		case <-c.Done:
		default:
			log.Printf("[hub] broadcast dropped: send buffer full")
		}
	}
}

// CloseAll 服务下线时关闭所有连接（优雅停机）：
// 客户端感知断开后按自身重连策略切到其他实例。
// closeConn 的写入发生在 Register 之前、读取发生在从 map 取出之后，
// 两端都被 Hub 锁间隔，无数据竞争。
func (h *Hub) CloseAll() {
	h.mu.RLock()
	all := make([]*Client, 0, len(h.clients))
	for _, list := range h.clients {
		all = append(all, list...)
	}
	h.mu.RUnlock()

	for _, c := range all {
		c.Close()
	}
}

// writeLoop 单 goroutine 独占写。带写超时，防止慢客户端把写方永久卡死
// （写失败即连接已坏，Close 解除 readLoop 的阻塞，由 readLoop 的 defer 统一清理）。
func (c *Client) writeLoop(ctx context.Context, conn *ws.Conn) {
	for {
		select {
		case msg := <-c.Send:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(msg); err != nil {
				_ = conn.Close()
				return
			}
		case <-c.Done:
			return
		case <-ctx.Done():
			return
		}
	}
}
