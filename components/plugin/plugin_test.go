package plugin_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/apus-run/gala/components/plugin"
)

type host struct{ events []string }

// fake 安装时记录自己的名字；installErr 非空时安装失败。
type fake struct {
	name       string
	installErr error
}

func (f *fake) Name() string { return f.name }
func (f *fake) Install(h *host) error {
	if f.installErr != nil {
		return f.installErr
	}
	h.events = append(h.events, f.name)
	return nil
}

func names(ps []plugin.Plugin[*host]) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name()
	}
	return out
}

func registry(ns ...string) *plugin.Registry[*host] {
	r := new(plugin.Registry[*host])
	for _, n := range ns {
		r.Register(&fake{name: n})
	}
	return r
}

func TestTryRegisterReturnsErrors(t *testing.T) {
	r := registry("a")
	if err := r.TryRegister(&fake{name: "a"}); err == nil {
		t.Fatal("duplicate: want error")
	}
	if err := r.TryRegister(nil); err == nil {
		t.Fatal("nil: want error")
	}
	if len(r.All()) != 1 {
		t.Fatal("failed registration must not change the registry")
	}
}

func TestRegisterPanicsOnError(t *testing.T) {
	r := registry("a")
	for name, fn := range map[string]func(){
		"nil":       func() { r.Register(nil) },
		"duplicate": func() { r.Register(&fake{name: "a"}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: want panic", name)
				}
			}()
			fn()
		}()
	}
}

func TestGet(t *testing.T) {
	r := registry("a")
	if p, ok := r.Get("a"); !ok || p.Name() != "a" {
		t.Fatalf("Get(a) = %v, %v", p, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatal("Get(missing) should report !ok")
	}
}

// All 是登记顺序，不是名字顺序。
func TestAllIsRegistrationOrder(t *testing.T) {
	r := registry("c", "a", "b")
	if got, want := names(r.All()), []string{"c", "a", "b"}; !slices.Equal(got, want) {
		t.Fatalf("All() = %v, want %v", got, want)
	}
}

// Select 的顺序就是调用方给定的顺序，不是名字顺序。
func TestSelectKeepsRequestedOrder(t *testing.T) {
	r := registry("a", "b", "c")
	got, err := r.Select("c", "a")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"c", "a"}; !slices.Equal(names(got), want) {
		t.Fatalf("Select = %v, want %v", names(got), want)
	}
}

func TestSelectRejectsMissingAndDuplicates(t *testing.T) {
	r := registry("a")

	got, err := r.Select("a", "x", "y", "a")
	if got != nil {
		t.Fatalf("partial result: %v", names(got))
	}
	for _, want := range []string{`"x" not registered`, `"y" not registered`, `"a" selected twice`} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want it to contain %s", err, want)
		}
	}
}

func TestInstallFollowsGivenOrder(t *testing.T) {
	r := registry("db", "tracing", "logging")
	ps, err := r.Select("logging", "tracing", "db")
	if err != nil {
		t.Fatal(err)
	}

	h := &host{}
	if err := plugin.Install(h, ps...); err != nil {
		t.Fatal(err)
	}
	if want := []string{"logging", "tracing", "db"}; !slices.Equal(h.events, want) {
		t.Fatalf("install order = %v, want %v", h.events, want)
	}
}

func TestInstallStopsAtFirstFailureAndNamesPlugin(t *testing.T) {
	boom := errors.New("boom")
	h := &host{}

	err := plugin.Install(h,
		&fake{name: "a"},
		&fake{name: "bad", installErr: boom},
		&fake{name: "never"},
	)

	if !errors.Is(err, boom) || !strings.Contains(err.Error(), `"bad"`) {
		t.Fatalf("err = %v, want wrapped boom naming the plugin", err)
	}
	if want := []string{"a"}; !slices.Equal(h.events, want) { // 失败之后的不再安装
		t.Fatalf("installed %v, want %v", h.events, want)
	}
}

func TestConcurrentRegisterAndRead(t *testing.T) {
	r := new(plugin.Registry[*host])
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(3)
		go func() { defer wg.Done(); r.Register(&fake{name: fmt.Sprintf("p%02d", i)}) }()
		go func() { defer wg.Done(); _ = r.All() }()
		go func() { defer wg.Done(); _, _ = r.Select("p00") }()
	}
	wg.Wait()
	if n := len(r.All()); n != 50 {
		t.Fatalf("len(All) = %d, want 50", n)
	}
}

// Name() 里反过来访问注册表不能死锁（Name 必须在锁外调用）。
func TestNameCalledOutsideLock(t *testing.T) {
	r := registry("a")
	r.Register(&reentrant{r: r})
}

type reentrant struct{ r *plugin.Registry[*host] }

func (p *reentrant) Name() string {
	p.r.Get("a")
	return "reentrant"
}
func (p *reentrant) Install(*host) error { return nil }
