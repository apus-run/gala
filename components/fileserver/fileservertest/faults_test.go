package fileservertest

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// waitSignal 等待信号并带超时，让阻塞相关用例在回归时快速失败而不是挂死。
func waitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s was not signalled", what)
	}
}

func TestBlockingReaderBlocksUntilReleased(t *testing.T) {
	b := NewBlockingReader(bytes.NewReader([]byte("payload")))
	defer b.Release() // 任何失败路径都要放行读取协程，避免泄漏

	select {
	case <-b.Started():
		t.Fatal("Started fired before any Read")
	default:
	}

	type result struct {
		n   int
		err error
		got string
	}
	done := make(chan result, 1)
	go func() {
		buf := make([]byte, len("payload"))
		n, err := b.Read(buf)
		done <- result{n, err, string(buf[:n])}
	}()

	waitSignal(t, b.Started(), "Started")

	select {
	case r := <-done:
		t.Fatalf("Read returned before Release: %+v", r)
	case <-time.After(50 * time.Millisecond):
	}

	b.Release()

	select {
	case r := <-done:
		if r.err != nil || r.got != "payload" {
			t.Fatalf("Read result = %+v, want payload with no error", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not return after Release")
	}
}

// TestBlockingReaderReadsTransparentlyAfterRelease 检查解除阻塞之后，每次读取
// 都直接透传底层 Reader，第二次读取不会再等信号。
func TestBlockingReaderReadsTransparentlyAfterRelease(t *testing.T) {
	b := NewBlockingReader(bytes.NewReader([]byte("abcdef")))
	defer b.Release()
	b.Release() // 允许在读取之前就解除阻塞

	buf := make([]byte, 3)
	for i, want := range []string{"abc", "def"} {
		n, err := b.Read(buf)
		if err != nil || string(buf[:n]) != want {
			t.Fatalf("read %d: n=%d err=%v got=%q, want %q", i, n, err, buf[:n], want)
		}
	}
	if _, err := b.Read(buf); !errors.Is(err, io.EOF) {
		t.Fatalf("Read after exhaustion = %v, want io.EOF", err)
	}
	waitSignal(t, b.Started(), "Started")
}

func TestBlockingReaderReleaseIsIdempotent(t *testing.T) {
	b := NewBlockingReader(bytes.NewReader([]byte("x")))
	b.Release()
	b.Release()
	b.Release()

	buf := make([]byte, 1)
	if n, err := b.Read(buf); err != nil || string(buf[:n]) != "x" {
		t.Fatalf("Read = %d, %v, want x with no error", n, err)
	}
}

// TestTrackedReaderDetectsClose 让契约套件里“Upload 不关闭输入”的断言有意义：
// 那条断言只检查 Closed() 是否为假，若 Close 本身不生效，它会无条件通过。
func TestTrackedReaderDetectsClose(t *testing.T) {
	tr := &TrackedReader{Reader: bytes.NewReader([]byte("data"))}
	if tr.Closed() {
		t.Fatal("a fresh TrackedReader already reports closed")
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(tr, buf); err != nil || string(buf) != "data" {
		t.Fatalf("Read = %q, %v, want data", buf, err)
	}
	if tr.Closed() {
		t.Fatal("reading the input marked it closed")
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !tr.Closed() {
		t.Fatal("Close was not recorded")
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestTrackedReaderConcurrentCloseAndInspect 检查记录位在并发读写下没有数据竞争。
func TestTrackedReaderConcurrentCloseAndInspect(t *testing.T) {
	tr := &TrackedReader{Reader: bytes.NewReader([]byte("x"))}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_ = tr.Closed()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = tr.Close()
	}()
	wg.Wait()
	if !tr.Closed() {
		t.Fatal("the close was lost under concurrent access")
	}
}

// TestBlockingReaderReleaseConcurrentWithRead 检查 Release 与 Read 并发时
// （race 模式下）没有数据竞争，且读取总能完成。
func TestBlockingReaderReleaseConcurrentWithRead(t *testing.T) {
	for range 50 {
		b := NewBlockingReader(bytes.NewReader([]byte("x")))
		done := make(chan struct{})
		go func() {
			defer close(done)
			buf := make([]byte, 1)
			if _, err := b.Read(buf); err != nil && !errors.Is(err, io.EOF) {
				t.Errorf("Read: %v", err)
			}
		}()
		waitSignal(t, b.Started(), "Started")
		b.Release()
		waitSignal(t, done, "the reader goroutine to finish")
	}
}
