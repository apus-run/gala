package mongo

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/apus-run/gala/components/log/zlog"
)

// Interceptor wraps a processFn to observe or modify command execution
// | Interceptor 包装 processFn，用于观察或修改命令执行。
type Interceptor func(oldProcessFn processFn) (newProcessFn processFn)

// InterceptorChain composes interceptors so the first one runs outermost
// | InterceptorChain 组合拦截器，第一个在最外层执行。
func InterceptorChain(interceptors ...Interceptor) func(oldProcess processFn) processFn {
	build := func(interceptor Interceptor, oldProcess processFn) processFn {
		return interceptor(oldProcess)
	}

	return func(oldProcess processFn) processFn {
		chain := oldProcess
		for _, interceptor := range slices.Backward(interceptors) {
			chain = build(interceptor, chain)
		}
		return chain
	}
}

// Builder builds ready-made interceptors with a logger
// | Builder 基于logger 构建现成的拦截器。
type Builder struct {
	log zlog.Logger
}

// NewBuilder creates a Builder over log | NewBuilder 基于 log 创建 Builder。
func NewBuilder(log zlog.Logger) *Builder {
	return &Builder{
		log,
	}
}

// BuildDebugInterceptor returns an interceptor logging every command with its
// cost and result | BuildDebugInterceptor 返回记录每条命令耗时与结果的拦截器。
func (b *Builder) BuildDebugInterceptor() func(processFn) processFn {
	return func(oldProcess processFn) processFn {
		return func(cmd *cmd) error {
			beg := time.Now()
			err := oldProcess(cmd)
			cost := time.Since(beg)
			var fields = make([]zlog.Field, 0)
			if err != nil {
				fields = append(
					fields,
					zlog.String("type", "mongo.response"),
					zlog.Duration("cost", cost),
					zlog.String("data", fmt.Sprintf("%s %v", cmd.name, mustJsonMarshal(cmd.req))),
					zlog.String("error", fmt.Sprintf("%v", err.Error())),
				)
			} else {
				fields = append(
					fields,
					zlog.String("type", "mongo.response"),
					zlog.Duration("cost", cost),
					zlog.String("data", fmt.Sprintf("%s %v", cmd.name, mustJsonMarshal(cmd.req))),
					zlog.String("res", fmt.Sprintf("%v", cmd.res)),
					zlog.String("dbName", cmd.dbName),
					zlog.String("collName", cmd.collName),
					zlog.String("cmdName", cmd.name),
				)
			}

			b.log.Info("mongodb", fields...)
			return err
		}
	}
}

func mustJsonMarshal(val any) string {
	res, _ := json.Marshal(val)
	return string(res)
}
