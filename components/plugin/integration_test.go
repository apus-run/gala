package plugin_test

// 集成测试：只用导出 API，按 lifecycle.go 文档里的宿主用法把
// Registry、Install、Lifecycle、Container 串成一个最小应用。
// 使用独立宿主 integrationApp（Life + Services），不依赖示例测试的类型。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/apus-run/gala/components/plugin"
)

type integrationApp struct {
	Life     plugin.Lifecycle
	Services plugin.Container
}

type recorder struct{ events []string }

var errInstallBoom = errors.New("install boom")

func (r *recorder) record(e string) { r.events = append(r.events, e) }

// logPlugin 提供 recorder 服务并挂满钩子，由 Select 排在消费者之前。
type logPlugin struct{ rec *recorder }

func (p *logPlugin) Name() string { return "log" }
func (p *logPlugin) Install(a *integrationApp) error {
	if err := plugin.Provide(&a.Services, p.rec); err != nil {
		return err
	}
	a.Life.BeforeStart(func(context.Context) error { p.rec.record("before-start"); return nil })
	a.Life.BeforeStop(func(context.Context) error { p.rec.record("before-stop"); return nil })
	a.Life.AfterStop(func(context.Context) error { p.rec.record("after-stop"); return nil })
	return nil
}

// greetPlugin 消费 log 提供的服务，由 Select 排在提供方之后。
type greetPlugin struct {
	got *recorder
	ok  bool
}

func (p *greetPlugin) Name() string { return "greet" }
func (p *greetPlugin) Install(a *integrationApp) error {
	p.got, p.ok = plugin.Inject[*recorder](&a.Services)
	if !p.ok {
		return errors.New("greet: no recorder provided")
	}
	p.got.record("greet-installed")
	return nil
}

func TestIntegrationInstallStartStopSharesService(t *testing.T) {
	reg := new(plugin.Registry[*integrationApp])
	rec := &recorder{}
	greet := &greetPlugin{}
	for _, p := range []plugin.Plugin[*integrationApp]{greet, &logPlugin{rec: rec}} {
		if err := reg.TryRegister(p); err != nil {
			t.Fatalf("register %s: %v", p.Name(), err)
		}
	}

	ps, err := reg.Select("log", "greet")
	if err != nil {
		t.Fatal(err)
	}
	a, ctx := &integrationApp{}, context.Background()
	if err := plugin.Install(a, ps...); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !greet.ok || greet.got != rec {
		t.Fatal("consumer did not receive the provider's recorder")
	}
	if err := a.Life.RunBeforeStart(ctx); err != nil {
		t.Fatalf("before start: %v", err)
	}
	if err := a.Life.RunBeforeStop(ctx); err != nil {
		t.Fatalf("before stop: %v", err)
	}
	if err := a.Life.RunAfterStop(ctx); err != nil {
		t.Fatalf("after stop: %v", err)
	}

	if want := "greet-installed,before-start,before-stop,after-stop"; strings.Join(rec.events, ",") != want {
		t.Fatalf("events = %v, want %v", rec.events, want)
	}
}

// failPlugin 安装失败，但已挂上的 AfterStop 清理钩子仍要执行（见 Lifecycle 文档）。
type failPlugin struct{ cleaned *bool }

func (p *failPlugin) Name() string { return "zfail" }
func (p *failPlugin) Install(a *integrationApp) error {
	a.Life.AfterStop(func(context.Context) error { *p.cleaned = true; return nil })
	return errInstallBoom
}

func TestIntegrationAfterStopRunsAfterInstallFailure(t *testing.T) {
	reg := new(plugin.Registry[*integrationApp])
	cleaned := false
	if err := reg.TryRegister(&failPlugin{cleaned: &cleaned}); err != nil {
		t.Fatalf("register: %v", err)
	}

	a := &integrationApp{}
	if err := plugin.Install(a, reg.All()...); !errors.Is(err, errInstallBoom) {
		t.Fatalf("install err = %v, want errInstallBoom", err)
	}
	if err := a.Life.RunAfterStop(context.Background()); err != nil {
		t.Fatalf("after stop: %v", err)
	}
	if !cleaned {
		t.Fatal("cleanup hook did not run after install failure")
	}
}
