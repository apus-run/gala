module github.com/apus-run/gala/components/mq/kafka

go 1.27

require (
	github.com/apus-run/gala/components/mq v0.14.0
	github.com/apus-run/gala/pkg/lang v0.14.0
	github.com/twmb/franz-go v1.22.2
	github.com/twmb/franz-go/pkg/kfake v0.0.0-20261013022759-7b6cac30a57a
	github.com/twmb/franz-go/pkg/kmsg v1.15.0
)

require (
	github.com/klauspost/compress v1.20.3 // indirect
	github.com/panjf2000/ants/v2 v2.11.3 // indirect
	github.com/pierrec/lz4/v4 v4.1.33 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/sync v0.19.0 // indirect
)

replace github.com/apus-run/gala/components/mq => ../
