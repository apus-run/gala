package env_test

import (
	"context"
	"testing"

	"github.com/apus-run/gala/components/conf/v2/env"
)

// Duplicate paths, parent/child conflicts, and empty segments are rejected
// before any value is read, so the result never depends on map order.
// 重复路径、父子冲突与空段在读取任何值之前即被拒绝，因此结果不依赖 map 顺序。
func TestLoadRejectsInvalidMappings(t *testing.T) {
	cases := map[string]map[string]string{
		"duplicate path":   {"A": "server.port", "B": "server.port"},
		"parent conflicts": {"A": "server", "B": "server.port"},
		"child conflicts":  {"A": "server.port", "B": "server"},
		"empty segment":    {"A": "server..port"},
		"empty path":       {"A": ""},
		"leading dot":      {"A": ".server"},
	}
	for name, bindings := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := env.New(bindings).Load(context.Background()); err == nil {
				t.Fatal("want mapping error, got nil")
			}
		})
	}
}

// Set values populate nested paths; unset and empty values are absent.
// 已设置的值写入嵌套路径；未设置与空串都视为缺席。
func TestLoadBuildsNestedAndSkipsAbsent(t *testing.T) {
	t.Setenv("T_ENV_PORT", "9000")
	t.Setenv("T_ENV_EMPTY", "")
	src := env.New(map[string]string{
		"T_ENV_PORT":  "server.port",
		"T_ENV_EMPTY": "name",
		"T_ENV_UNSET": "other",
	})
	out, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	server, ok := out["server"].(map[string]any)
	if !ok || server["port"] != "9000" {
		t.Fatalf("server.port not set: %#v", out)
	}
	if _, has := out["name"]; has {
		t.Fatal("empty env value should be absent")
	}
	if _, has := out["other"]; has {
		t.Fatal("unset env value should be absent")
	}
}
