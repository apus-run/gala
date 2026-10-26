module github.com/apus-run/gala/components/mq

go 1.27

require (
	github.com/apus-run/gala/pkg/lang v0.13.0
	go.uber.org/mock v0.6.0
)

require (
	github.com/panjf2000/ants/v2 v2.11.3 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/sync v0.19.0 // indirect
)

replace github.com/apus-run/gala/components/mq => ../mq
