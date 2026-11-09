package env_test

import (
	"errors"
	"testing"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/env"
)

type stageCfg struct {
	Name string `mapstructure:"name"`
}

// An invalid env mapping is reported as stage "config", not "read" or "bind".
// 无效的 env 映射报告为 "config" 阶段，而非 "read" 或 "bind"。
func TestInvalidMappingIsConfigStage(t *testing.T) {
	_, err := conf.Open[stageCfg](t.Context(), conf.WithSources(env.New(map[string]string{
		"A": "name",
		"B": "name",
	})))
	if err == nil {
		t.Fatal("duplicate mapping: want error")
	}
	ce, ok := errors.AsType[*conf.Error](err)
	if !ok {
		t.Fatalf("want *conf.Error, got %T: %v", err, err)
	}
	if ce.Stage != conf.StageConfig {
		t.Fatalf("stage = %q, want %q", ce.Stage, conf.StageConfig)
	}
}
