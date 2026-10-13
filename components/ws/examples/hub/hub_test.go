package main

import (
	"sync"
	"testing"
)

func newTestClient(userID string) *Client {
	return &Client{UserID: userID, Send: make(chan *Message, 1), Done: make(chan struct{})}
}

func TestHubPush(t *testing.T) {
	h := NewHub()

	// 一个用户两个设备 + 另一个用户
	c1 := newTestClient("u")
	c2 := newTestClient("u")
	other := newTestClient("v")
	h.Register(c1)
	h.Register(c2)
	h.Register(other)

	// 推送到达该用户所有设备，不影响别人
	h.Push("u", &Message{Msg: "hi"})
	for _, c := range []*Client{c1, c2} {
		if got := <-c.Send; got.Msg != "hi" {
			t.Fatalf("got %+v", got)
		}
	}
	select {
	case <-other.Send:
		t.Fatal("user v must not receive")
	default:
	}

	// 已断开（Done 关闭）的连接被跳过
	close(c1.Done)
	h.Push("u", &Message{Msg: "bye"})
	if got := <-c2.Send; got.Msg != "bye" {
		t.Fatalf("got %+v", got)
	}

	// 慢客户端（缓冲满）不阻塞推送方：能 Push 返回即通过
	slow := newTestClient("slow")
	slow.Send <- &Message{} // 占满缓冲
	h.Register(slow)
	h.Push("slow", &Message{Msg: "dropped"})
}

func TestHubPushTagAndBroadcast(t *testing.T) {
	h := NewHub()
	sub := newTestClient("u1")
	sub.Tags.Store("case-123", true)
	nosub := newTestClient("u2")
	h.Register(sub)
	h.Register(nosub)

	// 只送达订阅者
	h.PushTag("case-123", &Message{Msg: "progress"})
	if got := <-sub.Send; got.Msg != "progress" {
		t.Fatalf("got %+v", got)
	}
	select {
	case <-nosub.Send:
		t.Fatal("未订阅 tag 不应收到")
	default:
	}

	// 全体送达
	h.Broadcast(&Message{Msg: "all"})
	if got := <-sub.Send; got.Msg != "all" {
		t.Fatalf("got %+v", got)
	}
	if got := <-nosub.Send; got.Msg != "all" {
		t.Fatalf("got %+v", got)
	}
}

func TestHubUnregister(t *testing.T) {
	h := NewHub()
	c1 := newTestClient("u")
	c2 := newTestClient("u")
	h.Register(c1)
	h.Register(c2)

	// 真实流程里 readLoop defer 会 Unregister 每个连接
	h.Unregister(c1)
	h.Unregister(c2) // u 的最后一个连接，键应删除
	if _, ok := h.clients["u"]; ok {
		t.Fatal("key u should be deleted after last unregister")
	}
}

func TestHubCloseAll(t *testing.T) {
	h := NewHub()
	c1 := newTestClient("u1")
	c2 := newTestClient("u2")
	closedConns := make(chan struct{}, 2)
	c1.closeConn = func() { closedConns <- struct{}{} }
	c2.closeConn = func() { closedConns <- struct{}{} }
	h.Register(c1)
	h.Register(c2)

	h.CloseAll()
	<-c1.Done // Done 与底层 conn 都被关闭
	<-c2.Done
	<-closedConns
	<-closedConns

	c1.Close() // 幂等：重复关闭不 panic
	c1.Close()
}

func TestHubUnregisterReleasesClient(t *testing.T) {
	h := NewHub()
	keep := newTestClient("u")
	removed := newTestClient("u")
	h.Register(keep)
	h.Register(removed)
	removed.Close()
	h.Unregister(removed)

	live := h.clients["u"]
	if len(live) != 1 || live[0] != keep {
		t.Fatal("unregister must preserve the remaining device")
	}
	for _, stale := range live[len(live):cap(live)] {
		if stale != nil {
			t.Fatal("disconnected client remains reachable through the backing array")
		}
	}
}

func TestHubPushConcurrentMembership(t *testing.T) {
	h := NewHub()
	c1 := newTestClient("u")
	c2 := newTestClient("u")
	c1.Close()
	c2.Close()
	h.Register(c1)
	h.Register(c2)

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		<-start
		for range 1000 {
			h.Push("u", &Message{})
		}
	})
	wg.Go(func() {
		<-start
		for range 1000 {
			h.Unregister(c1)
			h.Register(c1)
		}
	})
	close(start)
	wg.Wait()
}
