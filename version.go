package gala

import (
	"encoding/json"
	"fmt"
	"runtime"
)

// Name and Version info of this framework, used for statistics and debug
const (
	Name    = "Gala"
	Version = "v0.15.1"
)

// Build metadata of the binary, overridden at build time:
//
//	go build -ldflags "\
//	  -X github.com/apus-run/gala.gitCommit=$(git rev-parse --short HEAD) \
//	  -X github.com/apus-run/gala.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
var (
	gitCommit = "unknown"
	buildDate = "unknown"
)

// Info holds version metadata of the framework and the running binary.
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

// String returns a single-line summary of all build metadata.
func (i *Info) String() string {
	return fmt.Sprintf("%s %s (commit %s, built %s, %s, %s)",
		i.Name, i.Version, i.GitCommit, i.BuildDate, i.GoVersion, i.Platform)
}

// ToJSON returns the info as a JSON object, e.g. for a version endpoint.
// Info holds strings only, so marshaling cannot fail.
func (i *Info) ToJSON() string {
	data, _ := json.Marshal(i)
	return string(data)
}
