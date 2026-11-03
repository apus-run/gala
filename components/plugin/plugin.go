// Package plugin 提供三个互相独立的小零件，按需取用，只依赖标准库：
//
//   - Plugin / Registry / Install：登记插件，按应用方给定的顺序安装。
//   - Lifecycle：四个生命周期钩子，钩子里的 panic、阻塞不会拖垮宿主。
//   - Container：按类型在插件之间共享服务。
//
// 不依赖任何 server 或 Web 框架，宿主类型 H 完全由使用方决定。
package plugin

import (
	"errors"
	"fmt"
	"sync"
)

// Plugin 是装配到宿主 H 上的扩展单元。Name 必须全局唯一且不变。
//
// 安装顺序不由插件声明（插件不该知道自己在整个应用里排第几），来源只有两种：
// 一是登记顺序（见 Registry.All，init() 自注册时由包的导入关系决定），
// 二是应用方在 Select 里显式给出。
type Plugin[H any] interface {
	Name() string
	Install(H) error
}

// Registry 是宿主 H 的插件目录，按登记顺序保存，并发安全，零值可用。
//
// Go 的包级变量不能带类型参数，所以由宿主包自己持有一份，例如：
//
//	var Plugins plugin.Registry[*App]
type Registry[H any] struct {
	mu sync.RWMutex
	es []entry[H] // 登记顺序就是切片顺序，是唯一的数据来源
}

// entry 缓存登记时的名字，这样查找时不必在持锁期间调用用户代码 Name()。
type entry[H any] struct {
	name string
	p    Plugin[H]
}

// find 要在持锁时调用。插件通常只有几十个，线性查找足够，不必再维护一份索引。
func (r *Registry[H]) find(name string) (Plugin[H], bool) {
	for _, e := range r.es {
		if e.name == name {
			return e.p, true
		}
	}
	return nil, false
}

// TryRegister 登记插件。p 为 nil 或重名时返回错误。
func (r *Registry[H]) TryRegister(p Plugin[H]) error {
	if p == nil {
		return errors.New("plugin: register nil plugin")
	}
	name := p.Name() // 用户代码，放在锁外
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.find(name); dup {
		return fmt.Errorf("plugin: %q already registered", name)
	}
	r.es = append(r.es, entry[H]{name: name, p: p})
	return nil
}

// Register 与 TryRegister 相同，但出错时 panic。适合在 init() 里调用
// （与 database/sql.Register 一致：那里没法返回 error，这类错误应当在启动时暴露）。
func (r *Registry[H]) Register(p Plugin[H]) {
	if err := r.TryRegister(p); err != nil {
		panic(err)
	}
}

// Get 按名字查找。
func (r *Registry[H]) Get(name string) (Plugin[H], bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.find(name)
}

// All 返回所有插件，按登记顺序。
//
// 插件在 init() 里自注册时，登记顺序就是 Go 的包初始化顺序：被依赖的包一定先于
// 依赖它的包，所以"A 依赖 B"只要让 A 所在的包导入 B 所在的包（只用来声明依赖时写
// import _ "…/b"），B 就一定先登记，不需要任何额外的声明机制。
// Go 每次从所有依赖均已初始化的包中，选择导入路径最小的包初始化。
// 集中写空白导入时，列表的书写顺序不影响结果；互不依赖的两个包也可能因为
// 各自的其他依赖，出现与导入路径排序不同的初始化顺序。
// 要完全由应用方决定顺序时用 Select。
func (r *Registry[H]) All() []Plugin[H] {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ps := make([]Plugin[H], len(r.es))
	for i, e := range r.es {
		ps[i] = e.p
	}
	return ps
}

// Select 按 names 的顺序返回插件，这个顺序就是之后 Install 的顺序，由应用方决定：
//
//	ps, err := Plugins.Select("logging", "tracing", "metrics", "db")
//
// 有名字未注册或重复时返回错误（列出全部问题），不返回部分结果。
func (r *Registry[H]) Select(names ...string) ([]Plugin[H], error) {
	out := make([]Plugin[H], 0, len(names))
	seen := make(map[string]bool, len(names))
	var errs []error

	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, n := range names {
		p, ok := r.find(n)
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("plugin: %q not registered", n))
		case seen[n]:
			errs = append(errs, fmt.Errorf("plugin: %q selected twice", n))
		default:
			seen[n] = true
			out = append(out, p)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}

// Install 按给定顺序安装 ps（通常是 Select 的结果），遇到第一个失败就停止，
// 错误里带上插件名。
//
// 资源清理不在这里处理：让插件在 Install 里登记 Lifecycle 的停止钩子，
// 宿主照常执行 AfterStop 即可，安装中途失败时也一样（见 Lifecycle）。
func Install[H any](h H, ps ...Plugin[H]) error {
	for _, p := range ps {
		if err := p.Install(h); err != nil {
			return fmt.Errorf("plugin %q: install: %w", p.Name(), err)
		}
	}
	return nil
}
