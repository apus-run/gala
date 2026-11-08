package conf_test

import (
	"context"
	"sync"
	"testing"

	conf "github.com/apus-run/gala/components/conf/v2"
)

// pair holds two fields that must always be published together.
// pair 的两个字段必须一起发布。
type pair struct {
	A int `mapstructure:"a"`
	B int `mapstructure:"b"`
}

// C03: a reader holding one Current pointer always sees A and B from the same
// published version, while another goroutine reloads repeatedly. Run under -race.
// C03：持有同一 Current 指针的读者，始终看到同一已发布版本的 A 与 B；
// 另一协程反复重载。在 -race 下运行。
func TestC03ReadersSeeOneVersion(t *testing.T) {
	src := &mutableSource{}
	src.set(1)
	c, err := conf.Open[pair](context.Background(), src)
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
			if err := c.Reload(context.Background()); err != nil {
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
