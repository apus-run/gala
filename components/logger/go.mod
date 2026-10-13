module github.com/apus-run/gala/components/logger

go 1.27

replace github.com/apus-run/gala/components/logger => ../logger

require (
	github.com/k0kubun/pp/v3 v3.5.3
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
)

require (
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)
