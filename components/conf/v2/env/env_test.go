package env_test

import (
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
			if _, err := env.New(bindings).Load(t.Context()); err == nil {
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
	out, err := src.Load(t.Context())
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

// Absorbed from koanf's EnvironFunc: an injected lookup supplies values, so
// the same absent-value contract holds without touching the process
// environment and the test can run in parallel.
// 吸收自 koanf 的 EnvironFunc：由注入的 lookup 提供值，缺席语义不变，
// 不碰进程环境，测试可并行。
func TestNewWithLookupInjectsValues(t *testing.T) {
	t.Parallel()
	values := map[string]string{"T_INJ_PORT": "9000", "T_INJ_EMPTY": ""}
	src := env.NewWithLookup(map[string]string{
		"T_INJ_PORT":  "server.port",
		"T_INJ_EMPTY": "name",
		"T_INJ_UNSET": "other",
	}, func(name string) (string, bool) {
		v, ok := values[name]
		return v, ok
	})
	out, err := src.Load(t.Context())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	server, ok := out["server"].(map[string]any)
	if !ok || server["port"] != "9000" {
		t.Fatalf("server.port not set: %#v", out)
	}
	if _, has := out["name"]; has {
		t.Fatal("empty injected value should be absent")
	}
	if _, has := out["other"]; has {
		t.Fatal("unset injected value should be absent")
	}
}

// A nil lookup falls back to the process environment.
// lookup 为 nil 时回退进程环境。
func TestNewWithLookupNilReadsProcessEnv(t *testing.T) {
	t.Setenv("T_INJ_PROC", "fallback")
	src := env.NewWithLookup(map[string]string{"T_INJ_PROC": "k"}, nil)
	out, err := src.Load(t.Context())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if out["k"] != "fallback" {
		t.Fatalf("k = %#v, want fallback", out["k"])
	}
}
