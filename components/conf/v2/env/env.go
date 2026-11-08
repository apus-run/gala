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
	"sort"
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
	return &Source{bindings: maps.Clone(bindings), lookup: os.LookupEnv}
}

// SafeName is the empty string: env errors name the variable, which Load
// reports directly.
// SafeName 返回空串：ENV 错误直接在 Load 中给出变量名。
func (s *Source) SafeName() string { return "" }

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
		return nil, &conf.MappingError{Err: err}
	}

	// Iterate in sorted order so errors are deterministic.
	// 按排序遍历，使错误可重现。
	names := make([]string, 0, len(s.bindings))
	for name := range s.bindings {
		names = append(names, name)
	}
	sort.Strings(names)

	out := map[string]any{}
	for _, name := range names {
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
	names := make([]string, 0, len(bindings))
	for name := range bindings {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
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
	sort.Strings(paths)
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
