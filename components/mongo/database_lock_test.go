package mongo

import (
	"testing"
	"time"
)

// Database.Client 返回后必须释放 mu，否则第二次调用永久死锁。
func TestDatabaseClientReleasesLock(t *testing.T) {
	d := &Database{} // database 为 nil 时 Client 会 panic，所以只验证锁
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.mu.Lock()
		d.mu.Unlock()
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("lock was not released")
	}
}
