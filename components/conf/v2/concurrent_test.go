package conf_test

import (
	"context"
	"maps"
	"sync"
	"testing"

	conf "github.com/apus-run/gala/components/conf/v2"
)

// C06 (subset): Reload calls on one instance never read sources concurrently,
// and the cancelled waiter does not read the source.
// C06（子集）：同一实例的 Reload 从不并发读取来源；等待中被取消的调用不读取来源。
func TestC06SerializedReloadAndCancelledWaiter(t *testing.T) {
	// The gate starts open so Open can finish; it is closed before the test
	// starts the blocking reloads.
	// gate 初始打开，使 Open 能完成；测试启动阻塞的 Reload 前再关闭它。
	gate := make(chan struct{})
	close(gate)
	started := make(chan struct{}, 8)
	file := &blockingSource{data: portFile(8080), gate: gate, started: started}

	c, err := conf.Open(t.Context(), appConfig{}, file)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Drain the Open signal, then block further loads.
	// 取走 Open 的信号，然后阻塞之后的加载。
	<-started
	file.setGate(make(chan struct{}))

	firstDone := make(chan error, 1)
	go func() { firstDone <- c.Reload(t.Context()) }()
	<-started // the first reload is now blocked inside Load | 第一个 Reload 已阻塞在 Load 中

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := file.count()
	if err := c.Reload(ctx); err == nil {
		t.Fatal("cancelled reload: want error")
	}
	if after := file.count(); after != before {
		t.Fatalf("cancelled waiter read source: calls %d -> %d", before, after)
	}

	file.releaseGate()
	if err := <-firstDone; err != nil {
		t.Fatalf("first reload: %v", err)
	}
}

// blockingSource blocks in Load until gate is closed, and records how many
// times Load ran.
// blockingSource 在 gate 关闭前阻塞于 Load，并统计 Load 次数。
type blockingSource struct {
	mu      sync.Mutex
	data    map[string]any
	gate    chan struct{}
	started chan struct{}
	calls   int
}

func (b *blockingSource) Load(context.Context) (map[string]any, error) {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-b.gate
	out := map[string]any{}
	maps.Copy(out, b.data)
	return out, nil
}

func (b *blockingSource) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

func (b *blockingSource) setGate(g chan struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gate = g
}

func (b *blockingSource) releaseGate() {
	b.mu.Lock()
	g := b.gate
	b.mu.Unlock()
	close(g)
}

// pair holds two fields that must always be published together.
// pair 的两个字段必须一起发布。
type pair struct {
	A int `conf:"a"`
	B int `conf:"b"`
}

// C03: a reader holding one Current pointer always sees A and B from the same
// published version, while another goroutine reloads repeatedly. Run under -race.
// C03：持有同一 Current 指针的读者，始终看到同一已发布版本的 A 与 B；
// 另一协程反复重载。在 -race 下运行。
func TestC03ReadersSeeOneVersion(t *testing.T) {
	src := &mutableSource{}
	src.set(1)
	c, err := conf.Open(t.Context(), pair{}, src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	const iterations = 200
	var wg sync.WaitGroup
	errs := make(chan string, 1)

	// Writer: each version sets A == B == n, so a torn read shows A != B.
	// 写者：每个版本令 A == B == n，撕裂的读取会显示 A != B。
	wg.Go(func() {
		for n := 2; n < iterations; n++ {
			src.set(n)
			if err := c.Reload(t.Context()); err != nil {
				select {
				case errs <- "reload: " + err.Error():
				default:
				}
				return
			}
		}
	})

	// Readers: each takes one pointer and checks the pair is consistent.
	// 读者：每次取一个指针并检查两个字段一致。
	for range 4 {
		wg.Go(func() {
			for range iterations {
				p := c.Current()
				if p.A != p.B {
					select {
					case errs <- "torn read":
					default:
					}
					return
				}
			}
		})
	}

	wg.Wait()
	select {
	case msg := <-errs:
		t.Fatal(msg)
	default:
	}
}

// mutableSource returns a pair whose two fields are both the current value.
// mutableSource 返回两个字段都等于当前值的 pair。
type mutableSource struct {
	mu sync.Mutex
	n  int
}

func (m *mutableSource) set(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n = n
}

func (m *mutableSource) Load(context.Context) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]any{"a": m.n, "b": m.n}, nil
}

func TestOldNestedSnapshotDuringReload(t *testing.T) {
	type config struct {
		Entries map[string][]int `conf:"entries"`
	}
	entries := map[string][]int{"entry": {1, 2}}
	source := &fakeSource{data: map[string]any{"entries": entries}}
	c, err := conf.Open(t.Context(), config{}, source)
	if err != nil {
		t.Fatal(err)
	}
	old := c.Current()
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Go(func() {
		<-start
		for range 1000 {
			if old.Entries["entry"][0] != 1 || old.Entries["entry"][1] != 2 {
				t.Error("old snapshot changed")
				return
			}
		}
	})
	close(start)
	entries["entry"][0] = 3
	entries["entry"] = []int{4, 5}
	if err := c.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if c.Current().Entries["entry"][0] != 4 || old.Entries["entry"][0] != 1 {
		t.Fatal("snapshots alias")
	}
}
