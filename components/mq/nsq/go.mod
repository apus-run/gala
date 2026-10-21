module github.com/apus-run/gala/components/mq/nsq

go 1.27

require (
	github.com/apus-run/gala/components/mq v0.0.0
	github.com/apus-run/gala/pkg/lang v0.8.1
	github.com/nsqio/go-nsq v1.1.0
)

require (
	github.com/golang/snappy v0.0.1 // indirect
	github.com/panjf2000/ants/v2 v2.11.3 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/sync v0.19.0 // indirect
)

replace github.com/apus-run/gala/components/mq => ../
