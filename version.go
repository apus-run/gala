package gala

import (
	"encoding/json"
	"fmt"
	"runtime"
)

// Name and Version info of this framework, used for statistics and debug.
// 框架名称与版本信息，用于统计与调试.
const (
	Name    = "Gala"
	Version = "v0.15.1"
)

// Build metadata of the binary, overridden at build time:
// 二进制构建元数据，构建时通过 -ldflags 覆盖：
//
//	go build -ldflags "\
//	  -X github.com/apus-run/gala.gitCommit=$(git rev-parse --short HEAD) \
//	  -X github.com/apus-run/gala.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
var (
	gitCommit = "unknown"
	buildDate = "unknown"
)

// Info holds version metadata of the framework and the running binary | Info 保存框架与运行二进制的版本元数据.
type Info struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	GitCommit string `json:"gitCommit"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
	Compiler  string `json:"compiler"`
	Platform  string `json:"platform"`
}

// Get returns version metadata. Name and Version are the framework's, the rest
// describe the binary that is running; marshal it directly for a version endpoint.
// Get 返回版本元数据。Name 与 Version 取框架值，其余描述当前运行的二进制；
// 可直接序列化给版本端点用.
func Get() *Info {
	return &Info{
		Name:      Name,
		Version:   Version,
		GitCommit: gitCommit,
		BuildDate: buildDate,
		GoVersion: runtime.Version(),
		Compiler:  runtime.Compiler,
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// String returns a single-line summary of all build metadata | String 返回所有构建元数据的单行摘要.
func (i *Info) String() string {
	return fmt.Sprintf("%s %s (commit %s, built %s, %s, %s)",
		i.Name, i.Version, i.GitCommit, i.BuildDate, i.GoVersion, i.Platform)
}

// ToJSON returns the info as a JSON object, e.g. for a version endpoint.
// Info holds strings only, so marshaling cannot fail.
// ToJSON 返回 JSON 形式的信息，可用于版本端点。Info 只含字符串，序列化不会失败.
func (i *Info) ToJSON() string {
	data, _ := json.Marshal(i)
	return string(data)
}
