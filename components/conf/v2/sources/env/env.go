// Package env provides a conf.Source that maps explicitly named environment
// variables onto configuration paths. Nothing is inferred from variable names.
//
// Package env 提供把显式命名的环境变量映射到配置路径的 conf.Source。
// 不从变量名推断字段。
package env

import (
	"context"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/apus-run/gala/components/conf/v2"
)

// Source maps variable name to a dotted field path, e.g.
// {"SERVER_PORT": "server.port"}.
//
// Source 把变量名映射到点分字段路径，例如 {"SERVER_PORT": "server.port"}。
type Source struct {
	bindings map[string]string
	lookup   func(string) (string, bool)
}

// New returns a Source for the given bindings, reading from the process
// environment.
//
// New 返回按给定映射读取进程环境的来源。
func New(bindings map[string]string) *Source {
	return NewWithLookup(bindings, os.LookupEnv)
}

// NewWithLookup is New with a custom value lookup matching os.LookupEnv's
// signature. A nil lookup falls back to the process environment. Absorbed
// from koanf v2.4.1's env Opt.EnvironFunc: callers can supply values without
// touching the process environment, so tests need no t.Setenv (which forbids
// parallel tests) and fakes stay local.
//
// NewWithLookup 与 New 相同，但用自定义取值函数（签名同 os.LookupEnv）。
// lookup 为 nil 时回退进程环境。吸收自 koanf v2.4.1 env 包的
// Opt.EnvironFunc：调用方可以不碰进程环境地提供值，测试无需 t.Setenv
// （它会禁止并行），伪造值也保持局部。
func NewWithLookup(bindings map[string]string, lookup func(name string) (string, bool)) *Source {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	return &Source{bindings: maps.Clone(bindings), lookup: lookup}
}

// Load builds a nested map from the bindings. An unset variable and a
// variable set to "" are both absent, so they never override a lower layer.
// Duplicate target paths and parent/child path conflicts are rejected before
// any value is read, so the result does not depend on map iteration order.
//
// Load 从映射构造嵌套 map。未设置与设为空串都视为缺席，不覆盖下层。
// 重复路径与父子路径冲突在读取值之前即报错，结果不依赖 map 遍历顺序。
func (s *Source) Load(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := checkPaths(s.bindings); err != nil {
		return nil, err
	}

	// Iterate in sorted order so errors are deterministic.
	// 按排序遍历，使错误可重现。
	out := map[string]any{}
	for _, name := range slices.Sorted(maps.Keys(s.bindings)) {
		val, ok := s.lookup(name)
		if !ok || val == "" {
			continue
		}
		setPath(out, s.bindings[name], val)
	}
	return out, nil
}

// checkPaths rejects empty segments, duplicate paths, and a path that is a
// prefix of another (a leaf cannot also be a parent).
// checkPaths 拒绝空段、重复路径，以及互为前缀的路径（叶子不能同时是父节点）。
func checkPaths(bindings map[string]string) error {
	seen := map[string]string{}
	paths := make([]string, 0, len(bindings))
	for _, name := range slices.Sorted(maps.Keys(bindings)) {
		path := bindings[name]
		if path == "" {
			return conf.SourceError(conf.StageConfig, name, "", nil)
		}
		for seg := range strings.SplitSeq(path, ".") {
			if seg == "" {
				return conf.SourceError(conf.StageConfig, name, path, nil)
			}
		}
		if _, dup := seen[path]; dup {
			return conf.SourceError(conf.StageConfig, name, path, nil)
		}
		seen[path] = name
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for i := range paths {
		for j := range paths {
			if i != j && strings.HasPrefix(paths[j], paths[i]+".") {
				return conf.SourceError(conf.StageConfig, seen[paths[j]], paths[j], nil)
			}
		}
	}
	return nil
}

// setPath writes val at the dotted path inside out, creating intermediate
// maps. checkPaths has already ruled out conflicts.
// setPath 在 out 的点分路径处写入 val，并创建中间 map。冲突已由 checkPaths 排除。
func setPath(out map[string]any, path, val string) {
	segs := strings.Split(path, ".")
	cur := out
	for _, seg := range segs[:len(segs)-1] {
		next, ok := cur[seg].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[seg] = next
		}
		cur = next
	}
	cur[segs[len(segs)-1]] = val
}
