module github.com/apus-run/gala/plugins/registry/redis

go 1.27

replace github.com/apus-run/gala => ../../..

replace github.com/apus-run/gala/components/rdb => ../../../components/rdb

require (
	github.com/apus-run/gala v0.17.0
	github.com/apus-run/gala/components/rdb v0.17.0
	github.com/redis/go-redis/v9 v9.19.0
)

require (
	github.com/alicebob/miniredis/v2 v2.35.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
)
