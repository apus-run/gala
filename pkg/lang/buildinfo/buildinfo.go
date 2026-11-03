// Package buildinfo exposes build-time facts (release/test/race flags and the
// build flavor) derived from build tags.
// 包 buildinfo 暴露由构建标签决定的构建期信息（release/test/race 标志与构建类型）。
package buildinfo

const (
	// ReleaseBuild indicates whether this build is a release build | 是否为 release 构建.
	ReleaseBuild bool = releaseBuild

	// TestBuild indicates whether this build is a test build | 是否为 test 构建.
	TestBuild bool = testBuild

	// BuildFlavor indicates the build flavor: "release" for release builds,
	// "development" for development builds.
	// BuildFlavor 表示构建类型：release 构建为 "release"，其余为 "development"。
	BuildFlavor string = buildFlavor

	// RaceEnabled indicates whether the build was created with the race
	// detector enabled. This usually only applies to tests, and will be false
	// for actual binary builds.
	// RaceEnabled 表示构建时是否开启了 race 检测器。通常只对测试生效，
	// 实际二进制构建中为 false。
	RaceEnabled = raceEnabled
)
