module github.com/apus-run/gala/components/authn

go 1.25

replace github.com/apus-run/gala/components/authn => ./../authn

require (
	github.com/golang-jwt/jwt/v5 v5.3.0
	github.com/redis/go-redis/v9 v9.12.1
	golang.org/x/crypto v0.41.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
)
