module github.com/apus-run/gala/components/cache

go 1.25

replace github.com/apus-run/gala/components/cache => ../cache

require (
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/stretchr/testify v1.10.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/google/uuid v1.6.0
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/redis/go-redis/v9 v9.12.1
	go.uber.org/mock v0.5.2
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
