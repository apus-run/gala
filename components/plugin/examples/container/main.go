// Command container demonstrates part three of the plugin package on its
// own: Container — no Plugin, no Registry, no Lifecycle. Two layers that
// never import each other share a service by type.
//
// container 单独演示 plugin 的零件三：Container——不用 Plugin、Registry，
// 也不用 Lifecycle。两个互不 import 的层按类型共享服务。
//
// # Run | 运行
//
//	go run ./examples/container
package main

import (
	"fmt"

	"github.com/apus-run/gala/components/plugin"
)

// RequestSink is the shared service: the transport layer writes request
// lines, the metrics layer writes counters — both depend only on this
// interface, never on each other.
// RequestSink 是共享服务：传输层写请求行，指标层写计数——两者只依赖这个
// 接口，互不依赖。
type RequestSink interface {
	Write(line string)
}

type stdoutSink struct{ lines []string }

func (s *stdoutSink) Write(line string) { s.lines = append(s.lines, line) }

func (s *stdoutSink) dump() {
	for _, l := range s.lines {
		fmt.Println("sink:", l)
	}
}

// transportLayer only consumes: it takes the sink from the container.
// transportLayer 只消费：从容器取出 sink。
func transportLayer(c *plugin.Container) error {
	sink, ok := plugin.Inject[RequestSink](c)
	if !ok {
		return fmt.Errorf("transport: no RequestSink provided")
	}
	sink.Write("GET /users")
	sink.Write("POST /orders")
	return nil
}

// metricsLayer consumes the same instance, no wiring on our side.
// metricsLayer 消费同一个实例，无需人工接线。
func metricsLayer(c *plugin.Container) error {
	sink, ok := plugin.Inject[RequestSink](c)
	if !ok {
		return fmt.Errorf("metrics: no RequestSink provided")
	}
	sink.Write("counter requests=2")
	return nil
}

func main() {
	var services plugin.Container // zero value, ready to use | 零值可用

	// Provide registers under the STATIC type T: spell the interface out so
	// consumers can Inject[RequestSink]. Providing the concrete
	// *stdoutSink instead would make Inject[RequestSink] miss — the classic
	// trap from the Container docs.
	// Provide 以 T 的静态类型为键：显式写出接口，消费方才能
	// Inject[RequestSink]。若按具体类型 *stdoutSink 登记，接口就取不到——
	// 这是 Container 文档里点名的坑。
	sink := &stdoutSink{}
	if err := plugin.Provide[RequestSink](&services, sink); err != nil {
		fmt.Println("provide:", err)
		return
	}

	// A second provide of the same type is rejected: one service per type.
	// 同类型重复登记被拒绝：一个类型一个服务。
	if err := plugin.Provide[RequestSink](&services, sink); err != nil {
		fmt.Println("duplicate:", err)
	}

	for _, layer := range []func(*plugin.Container) error{transportLayer, metricsLayer} {
		if err := layer(&services); err != nil {
			fmt.Println(err)
			return
		}
	}
	sink.dump()
}
