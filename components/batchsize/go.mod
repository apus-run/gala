module github.com/apus-run/gala/components/batchsize

go 1.27

require (
	github.com/apus-run/gala/components/ringbuffer v0.17.3
	github.com/stretchr/testify v1.12.1
)

require go.yaml.in/yaml/v3 v3.0.5 // indirect

replace github.com/apus-run/gala/components/batchsize => ../batchsize

replace github.com/apus-run/gala/components/ringbuffer => ../ringbuffer
