package plugin_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/apus-run/gala/components/plugin"
)

type tracer interface{ trace() }

type noopTracer struct{}

func (noopTracer) trace() {}

func TestContainerProvideInjectRoundTrip(t *testing.T) {
	var c plugin.Container // 零值可用
	if err := plugin.Provide[tracer](&c, noopTracer{}); err != nil {
		t.Fatalf("provide: %v", err)
	}
	got, ok := plugin.Inject[tracer](&c)
	if !ok || got != tracer(noopTracer{}) {
		t.Fatalf("Inject = %v, %v, want the provided tracer", got, ok)
	}
	if _, ok := plugin.Inject[int](&c); ok {
		t.Fatal("Inject[int] found, want miss for unregistered type")
	}
}

func TestContainerDuplicateProvideRejected(t *testing.T) {
	var c plugin.Container
	if err := plugin.Provide[tracer](&c, noopTracer{}); err != nil {
		t.Fatalf("first provide: %v", err)
	}
	err := plugin.Provide[tracer](&c, noopTracer{})
	if err == nil || !strings.Contains(err.Error(), "tracer") {
		t.Fatalf("err = %v, want duplicate error naming the type", err)
	}
}

// 第二次 Provide 失败时，第一次登记的值必须原样保留。
func TestContainerDuplicateProvideKeepsFirstValue(t *testing.T) {
	var c plugin.Container
	first := &noopTracer{}
	if err := plugin.Provide[tracer](&c, first); err != nil {
		t.Fatalf("first provide: %v", err)
	}
	if err := plugin.Provide[tracer](&c, noopTracer{}); err == nil {
		t.Fatal("second provide: want error")
	}
	if got, ok := plugin.Inject[tracer](&c); !ok || got != tracer(first) {
		t.Fatalf("Inject = %v, %v, want the first value kept", got, ok)
	}
}

func TestContainerProvideNilRejected(t *testing.T) {
	var c plugin.Container
	if err := plugin.Provide[tracer](&c, nil); err == nil {
		t.Fatal("provide nil: want error")
	}
	if _, ok := plugin.Inject[tracer](&c); ok {
		t.Fatal("nil value must not be stored")
	}
}

func TestContainerConcreteKeyDoesNotSatisfyInterfaceLookup(t *testing.T) {
	var c plugin.Container
	// 键是 T 的静态类型：这里键是 noopTracer，不是 tracer（见 Container 文档）。
	if err := plugin.Provide(&c, noopTracer{}); err != nil {
		t.Fatalf("provide: %v", err)
	}
	if _, ok := plugin.Inject[tracer](&c); ok {
		t.Fatal("Inject[tracer] found a value stored under its concrete type")
	}
}

// 并发 Provide 不同类型 + 并发 Inject，全部成功且值一一对应。
func TestContainerConcurrentProvideInject(t *testing.T) {
	var c plugin.Container
	var wg sync.WaitGroup
	const n = 40
	for i := range n {
		wg.Add(2)
		go func() { defer wg.Done(); _ = plugin.Provide(&c, fmt.Sprintf("svc-%02d", i)) }()
		go func() {
			defer wg.Done()
			if _, ok := plugin.Inject[int](&c); ok {
				t.Error("Inject[int] must not resolve under a string key")
			}
		}()
	}
	wg.Wait()
	for range n {
		got, ok := plugin.Inject[string](&c)
		if !ok || got == "" {
			t.Fatalf("Inject[string] after concurrent provides = %q, %v", got, ok)
		}
	}
}
