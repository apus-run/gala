// Upstream: github.com/casbin/redis-adapter v3.6.0 (fb849061fe604426e73e60fd029fec37fe2d2bc2).
// Client stack is github.com/redis/go-redis/v9 and github.com/casbin/casbin/v3.
module github.com/apus-run/gala/components/authz/adapter/redis

go 1.27

replace github.com/apus-run/gala/components/authz/adapter => ../

require (
	github.com/alicebob/miniredis/v2 v2.40.0
	github.com/casbin/casbin/v3 v3.10.0
	github.com/redis/go-redis/v9 v9.23.0
)

require (
	github.com/bmatcuk/doublestar/v4 v4.6.1 // indirect
	github.com/casbin/govaluate v1.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.uber.org/atomic v1.12.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)
