module github.com/apus-run/gala/components/authn

go 1.27

require (
	github.com/alicebob/miniredis/v2 v2.35.0
	github.com/apus-run/gala/components/token v0.14.0
	github.com/apus-run/gala/pkg/jwtx v0.14.0
	github.com/apus-run/gala/pkg/option v0.14.0
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/redis/go-redis/v9 v9.19.0
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// 本地开发用：jwtx 发新 tag 后删掉这行，把上面的 require 提到该版本
replace github.com/apus-run/gala/pkg/jwtx => ../../pkg/jwtx

// 本地开发用：components/token 与 pkg/jwtx 发新 tag 后删掉这两行，把 require 提到对应版本
replace github.com/apus-run/gala/components/token => ../token
