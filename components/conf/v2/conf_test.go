package conf_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	conf "github.com/apus-run/gala/components/conf/v2"
)

// fakeSource returns a fixed map, or an error, and counts Load calls.
// fakeSource 返回固定 map 或错误，并统计 Load 次数。
type fakeSource struct {
	mu    sync.Mutex
	data  map[string]any
	err   error
	calls int
}

func (f *fakeSource) Load(context.Context) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]any{}
	for k, v := range f.data {
		out[k] = v
	}
	return out, nil
}

type appConfig struct {
	Server struct {
		Port int `mapstructure:"port"`
	} `mapstructure:"server"`
}

func (c *appConfig) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return errors.New("server.port: must be in 1..65535")
	}
	return nil
}

func portFile(port any) map[string]any {
	return map[string]any{"server": map[string]any{"port": port}}
}

func portEnv(port string) map[string]any {
	return map[string]any{"server": map[string]any{"port": port}}
}

// C01: a higher-priority env layer keeps winning when the file changes.
// C01：文件变化时，靠后的环境变量层仍然胜出。
func TestC01EnvWinsAfterFileReload(t *testing.T) {
	file := &fakeSource{data: portFile(8080)}
	env := &fakeSource{data: portEnv("9000")}

	cfg, err := conf.Open[appConfig](context.Background(), file, env)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := cfg.Current().Server.Port; got != 9000 {
		t.Fatalf("initial port = %d, want 9000", got)
	}

	file.data = portFile(8081)
	if err := cfg.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := cfg.Current().Server.Port; got != 9000 {
		t.Fatalf("after file change port = %d, want 9000 (env still wins)", got)
	}
}

// C02: removing the override restores the file value; a failed reload keeps the
// previous published value.
// C02：移除覆盖后恢复文件值；重载失败时保留旧的已发布值。
func TestC02RemoveOverrideAndKeepLastGood(t *testing.T) {
	file := &fakeSource{data: portFile(8081)}
	env := &fakeSource{data: portEnv("9000")}

	cfg, err := conf.Open[appConfig](context.Background(), file, env)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	env.data = map[string]any{}
	if err := cfg.Reload(context.Background()); err != nil {
		t.Fatalf("reload without env: %v", err)
	}
	if got := cfg.Current().Server.Port; got != 8081 {
		t.Fatalf("port after env removed = %d, want 8081", got)
	}

	file.data = portFile(0)
	if err := cfg.Reload(context.Background()); err == nil {
		t.Fatal("reload with invalid port: want error")
	}
	if got := cfg.Current().Server.Port; got != 8081 {
		t.Fatalf("port after failed validate = %d, want 8081 (kept)", got)
	}
}

// C03: a bind error on one field leaves every field unchanged, and the same
// instance recovers once the input is fixed.
// C03：单个字段绑定失败时所有字段都保持不变；修正输入后同一实例可恢复。
func TestC03BindErrorIsAtomic(t *testing.T) {
	type cfg struct {
		Name string `mapstructure:"name"`
		Port int    `mapstructure:"port"`
	}
	file := &fakeSource{data: map[string]any{"name": "old", "port": 8080}}
	c, err := conf.Open[cfg](context.Background(), file)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	file.data = map[string]any{"name": "new", "port": "abc"}
	if err := c.Reload(context.Background()); err == nil {
		t.Fatal("reload with bad port: want error")
	}
	if got := c.Current().Name; got != "old" {
		t.Fatalf("name = %q after failed batch, want %q", got, "old")
	}

	file.data = map[string]any{"name": "new", "port": 9090}
	if err := c.Reload(context.Background()); err != nil {
		t.Fatalf("reload after fix: %v", err)
	}
	if got := c.Current(); got.Name != "new" || got.Port != 9090 {
		t.Fatalf("after fix = %+v, want name=new port=9090", *got)
	}
}

// C04: a first-load failure yields no instance and no panic.
// C04：首次加载失败时不返回实例，也不 panic。
func TestC04FirstLoadFailureReturnsNil(t *testing.T) {
	file := &fakeSource{err: errors.New("disk on fire /secret/path/cfg.yaml")}
	c, err := conf.Open[appConfig](context.Background(), file)
	if err == nil {
		t.Fatal("open: want error")
	}
	if c != nil {
		t.Fatal("open: want nil config on failure")
	}
}

// C04 (reload): a reload failure keeps the old pointer and does not leak the
// source's raw error text.
// C04（重载）：重载失败保留旧指针，且不泄露来源的原始错误文本。
func TestC04ReloadFailureKeepsOldAndHidesText(t *testing.T) {
	file := &fakeSource{data: portFile(8080)}
	c, err := conf.Open[appConfig](context.Background(), file)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	file.err = errors.New("leak: /secret/path/cfg.yaml token=TOPSECRET")

	err = c.Reload(context.Background())
	if err == nil {
		t.Fatal("reload: want error")
	}
	if got := c.Current().Server.Port; got != 8080 {
		t.Fatalf("port = %d, want 8080 kept", got)
	}
	if msg := err.Error(); containsAny(msg, "TOPSECRET", "/secret/path") {
		t.Fatalf("error leaks source text: %q", msg)
	}
}

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

	c, err := conf.Open[appConfig](context.Background(), file)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Drain the Open signal, then block further loads.
	// 取走 Open 的信号，然后阻塞之后的加载。
	<-started
	file.setGate(make(chan struct{}))

	firstDone := make(chan error, 1)
	go func() { firstDone <- c.Reload(context.Background()) }()
	<-started // the first reload is now blocked inside Load | 第一个 Reload 已阻塞在 Load 中

	ctx, cancel := context.WithCancel(context.Background())
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
	for k, v := range b.data {
		out[k] = v
	}
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

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
