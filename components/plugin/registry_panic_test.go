package plugin

import (
	"testing"
	"time"
)

type panicName struct{}

func (panicName) Name() string        { panic("boom") }
func (panicName) Register(server any) {}

// Name() panic 之后全局锁必须是释放状态，否则后面的 Register/All/Get 全部卡死。
func TestRegisterNamePanicDoesNotLeakLock(t *testing.T) {
	func() {
		defer func() { _ = recover() }()
		Register(panicName{})
	}()
	done := make(chan struct{})
	go func() { _ = All(); close(done) }()
	select {
	case <-done:
	case <-timeoutC():
		t.Fatal("registry lock was leaked by a panicking Name()")
	}
}

func timeoutC() <-chan time.Time { return time.After(time.Second) }
